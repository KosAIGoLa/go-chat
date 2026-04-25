package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/ck-chat/ck-chat/internal/auth"
	apperrors "github.com/ck-chat/ck-chat/internal/errors"
	"github.com/ck-chat/ck-chat/internal/message"
	"github.com/ck-chat/ck-chat/internal/presence"
	"github.com/ck-chat/ck-chat/internal/receipt"
	"github.com/ck-chat/ck-chat/internal/route"
)

type Packet struct {
	Command string          `json:"command"`
	TraceID string          `json:"trace_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type MessageService interface {
	Send(context.Context, message.SendRequest) (message.SendResponse, error)
	Sync(ctx context.Context, conversationID, fromSeq uint64, limit int) ([]message.Message, error)
	Recall(ctx context.Context, req message.RecallRequest) (message.Message, error)
	Delete(ctx context.Context, req message.DeleteRequest) (message.Message, error)
	Edit(ctx context.Context, req message.EditRequest) (message.Message, error)
}

type ReceiptStore interface {
	MarkDelivered(conversationID, userID uint64, deviceID string, seq uint64, updatedAtMs int64) receipt.Receipt
	MarkRead(conversationID, userID uint64, deviceID string, seq uint64, updatedAtMs int64) receipt.Receipt
	MarkDeliveredBatch(conversationID, userID uint64, deviceID string, seqs []uint64, updatedAtMs int64) receipt.Receipt
	MarkReadBatch(conversationID, userID uint64, deviceID string, seqs []uint64, updatedAtMs int64) receipt.Receipt
}

type PresenceStore interface {
	Online(userID uint64, device presence.Device)
	Offline(userID uint64, deviceID string)
	Snapshot(userID uint64) presence.Snapshot
}

// presenceSubsKey returns the map key for a watched user's subscriber set.
func presenceSubsKey(watchedUserID uint64) string {
	return fmt.Sprintf("psub:%d", watchedUserID)
}

type WebSocketHandler struct {
	gatewayID string
	tokens    *auth.TokenService
	routes    *route.Registry
	messages  MessageService
	receipts  ReceiptStore
	presence  PresenceStore
	upgrader  websocket.Upgrader
	clients   sync.Map
	// presenceSubs maps watchedUserID (as presenceSubsKey) → sync.Map{clientID → *wsClient}
	presenceSubs sync.Map
	now          func() time.Time
}

func NewWebSocketHandler() *WebSocketHandler {
	return &WebSocketHandler{
		gatewayID: "gateway-local",
		now:       time.Now,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (h *WebSocketHandler) WithAuth(tokens *auth.TokenService) *WebSocketHandler {
	h.tokens = tokens
	return h
}

func (h *WebSocketHandler) WithRouteRegistry(routes *route.Registry) *WebSocketHandler {
	h.routes = routes
	return h
}

func (h *WebSocketHandler) WithMessageSender(sender MessageService) *WebSocketHandler {
	h.messages = sender
	return h
}

func (h *WebSocketHandler) WithReceiptStore(store ReceiptStore) *WebSocketHandler {
	h.receipts = store
	return h
}

func (h *WebSocketHandler) WithPresenceStore(store PresenceStore) *WebSocketHandler {
	h.presence = store
	return h
}

func (h *WebSocketHandler) WithGatewayID(gatewayID string) *WebSocketHandler {
	if gatewayID != "" {
		h.gatewayID = gatewayID
	}
	return h
}

func (h *WebSocketHandler) Register(router gin.IRouter) {
	router.GET("/ws", h.serve)
	router.GET("/api/v1/gateway/ws", h.serve)
}

func (h *WebSocketHandler) serve(c *gin.Context) {
	principal, err := h.authenticate(c.Request)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "AUTH_TOKEN_INVALID", "message": err.Error(), "retryable": false}})
		return
	}
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	client := newClient(conn, principal, h.messages, h.receipts, h.presence, h.now, h)
	// Wire sender-side multi-device sync: push the sent message to the sender's
	// other connected devices by ranging over h.clients, skipping this device.
	client.syncPusher = func(msg message.Message) {
		myID := client.id
		myDeviceID := client.principal.DeviceID
		senderUserID := msg.SenderID
		seen := map[string]struct{}{myID: {}}
		h.clients.Range(func(_, value any) bool {
			other, ok := value.(*wsClient)
			if !ok || other == nil {
				return true
			}
			if other.principal.UserID != senderUserID || other.principal.DeviceID == myDeviceID {
				return true
			}
			if _, dup := seen[other.id]; dup {
				return true
			}
			seen[other.id] = struct{}{}
			other.writeJSON("push_message", "", toMessagePayload(msg))
			return true
		})
	}
	h.clients.Store(client.id, client)
	h.clients.Store(clientKey(principal.UserID, principal.DeviceID), client)
	if h.routes != nil {
		h.registerRoute(principal, client.id)
	}
	if h.presence != nil {
		h.presence.Online(principal.UserID, presence.Device{DeviceID: principal.DeviceID, GatewayID: h.gatewayID, SeenAt: h.now()})
		h.notifyPresenceChange(principal.UserID)
	}
	defer func() {
		if h.routes != nil {
			h.routes.Unregister(principal.UserID, principal.DeviceID)
		}
		if h.presence != nil {
			h.presence.Offline(principal.UserID, principal.DeviceID)
			h.notifyPresenceChange(principal.UserID)
		}
		client.unsubscribeAll()
		h.clients.Delete(client.id)
		h.clients.Delete(clientKey(principal.UserID, principal.DeviceID))
		_ = conn.Close()
	}()
	client.loop(c.Request.Context(), func() {
		now := h.now()
		if h.routes != nil {
			h.routes.UpdateLastSeen(principal.UserID, principal.DeviceID, now.UnixMilli())
		}
		if h.presence != nil {
			h.presence.Online(principal.UserID, presence.Device{DeviceID: principal.DeviceID, GatewayID: h.gatewayID, SeenAt: now})
			h.notifyPresenceChange(principal.UserID)
		}
	})
}

func (h *WebSocketHandler) PushToDevice(userID uint64, deviceID string, msg message.Message) bool {
	value, ok := h.clients.Load(clientKey(userID, deviceID))
	if !ok {
		return false
	}
	client, ok := value.(*wsClient)
	if !ok {
		return false
	}
	client.writeJSON("push_message", "", toMessagePayload(msg))
	return true
}

func (h *WebSocketHandler) PushToUser(userID uint64, msg message.Message) int {
	delivered := 0
	seen := make(map[string]struct{})
	h.clients.Range(func(_, value any) bool {
		client, ok := value.(*wsClient)
		if !ok || client.principal.UserID != userID {
			return true
		}
		if _, ok := seen[client.id]; ok {
			return true
		}
		seen[client.id] = struct{}{}
		client.writeJSON("push_message", "", toMessagePayload(msg))
		delivered++
		return true
	})
	return delivered
}

func (h *WebSocketHandler) registerRoute(principal auth.Principal, connID string) {
	h.routes.Register(route.DeviceRoute{
		UserID:     principal.UserID,
		DeviceID:   principal.DeviceID,
		GatewayID:  h.gatewayID,
		ConnID:     connID,
		Protocol:   "websocket",
		LastSeenMs: h.now().UnixMilli(),
	})
}

func (h *WebSocketHandler) authenticate(r *http.Request) (auth.Principal, error) {
	if h.tokens == nil {
		return auth.Principal{TenantID: 1, UserID: 1, DeviceID: "anonymous"}, nil
	}
	token := r.URL.Query().Get("access_token")
	if token == "" {
		var err error
		token, err = auth.BearerToken(r.Header.Get("Authorization"))
		if err != nil {
			return auth.Principal{}, err
		}
	}
	principal, err := h.tokens.Verify(token)
	if err != nil {
		return auth.Principal{}, err
	}
	if principal.UserID == 0 || strings.TrimSpace(principal.DeviceID) == "" {
		return auth.Principal{}, fmt.Errorf("user_id and device_id are required")
	}
	return principal, nil
}

type wsClient struct {
	id        string
	conn      *websocket.Conn
	principal auth.Principal
	messages  MessageService
	receipts  ReceiptStore
	presence  PresenceStore
	handler   *WebSocketHandler
	now       func() time.Time
	// syncPusher is called after a successful send_message to push the message
	// to the sender's other connected devices (sender-side multi-device sync).
	// It is set by serve() as a closure over the handler's clients map.
	syncPusher func(msg message.Message)
	// subscribedTo tracks userIDs this client subscribed to for presence push,
	// so we can clean up subscriptions on disconnect.
	subscribedTo []uint64
}

func newClient(conn *websocket.Conn, principal auth.Principal, sender MessageService, receipts ReceiptStore, presenceStore PresenceStore, now func() time.Time, h *WebSocketHandler) *wsClient {
	return &wsClient{
		id:        fmt.Sprintf("%d-%d-%s", principal.UserID, time.Now().UnixNano(), principal.DeviceID),
		conn:      conn,
		principal: principal,
		messages:  sender,
		receipts:  receipts,
		presence:  presenceStore,
		handler:   h,
		now:       now,
	}
}

func (c *wsClient) loop(ctx context.Context, onSeen func()) {
	c.conn.SetReadLimit(64 * 1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(65 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		if onSeen != nil {
			onSeen()
		}
		return c.conn.SetReadDeadline(time.Now().Add(65 * time.Second))
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var pkt Packet
		if err := c.conn.ReadJSON(&pkt); err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, context.Canceled) {
				return
			}
			return
		}
		if onSeen != nil {
			onSeen()
		}
		switch pkt.Command {
		case "ping", "heartbeat":
			c.write(Packet{Command: "pong", TraceID: pkt.TraceID})
		case "send_message":
			c.handleSendMessage(ctx, pkt)
		case "sync_messages":
			c.handleSyncMessages(ctx, pkt)
		case "delivery_ack":
			c.handleReceiptAck(pkt, false)
		case "read_ack":
			c.handleReceiptAck(pkt, true)
		case "batch_delivery_ack":
			c.handleBatchReceiptAck(pkt, false)
		case "batch_read_ack":
			c.handleBatchReceiptAck(pkt, true)
		case "presence_subscribe":
			c.handlePresenceSubscribe(pkt)
		case "recall_message":
			c.handleRecallMessage(ctx, pkt)
		case "delete_message":
			c.handleDeleteMessage(ctx, pkt)
		case "edit_message":
			c.handleEditMessage(ctx, pkt)
		default:
			c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), "unsupported command", false)
		}
	}
}

type sendMessagePayload struct {
	ConversationID uint64 `json:"conversation_id"`
	ClientMsgID    string `json:"client_msg_id"`
	Type           int32  `json:"type"`
	Payload        string `json:"payload"`
}

type sendMessageAckPayload struct {
	MsgID          uint64           `json:"msg_id"`
	ConversationID uint64           `json:"conversation_id"`
	Seq            uint64           `json:"seq"`
	ClientMsgID    string           `json:"client_msg_id"`
	Stage          message.AckStage `json:"stage"`
	CreatedAtMs    int64            `json:"created_at_ms"`
}

type syncMessagesPayload struct {
	ConversationID uint64 `json:"conversation_id"`
	FromSeq        uint64 `json:"from_seq"`
	Limit          int    `json:"limit"`
}

type syncMessagesResponsePayload struct {
	Messages []messagePayload `json:"messages"`
	HasMore  bool             `json:"has_more"`
}

type messagePayload struct {
	MsgID          uint64 `json:"msg_id"`
	ConversationID uint64 `json:"conversation_id"`
	Seq            uint64 `json:"seq"`
	SenderID       uint64 `json:"sender_id"`
	SenderDeviceID string `json:"sender_device_id"`
	ClientMsgID    string `json:"client_msg_id"`
	Type           int32  `json:"type"`
	Payload        string `json:"payload"`
	CreatedAtMs    int64  `json:"created_at_ms"`
}

type recallMessagePayload struct {
	MessageID uint64 `json:"msg_id"`
}

type editMessagePayload struct {
	MessageID uint64 `json:"msg_id"`
	Payload   string `json:"payload"`
}

type recallMessageAckPayload struct {
	MessageID    uint64                `json:"msg_id"`
	Status       message.MessageStatus `json:"status"`
	RecalledAtMs int64                 `json:"recalled_at_ms"`
}

type editMessageAckPayload struct {
	MessageID  uint64                `json:"msg_id"`
	Status     message.MessageStatus `json:"status"`
	Payload    string                `json:"payload"`
	EditedAtMs int64                 `json:"edited_at_ms"`
}

type receiptAckPayload struct {
	ConversationID uint64 `json:"conversation_id"`
	Seq            uint64 `json:"seq"`
}

type receiptAckResponsePayload struct {
	ConversationID uint64 `json:"conversation_id"`
	DeliveredSeq   uint64 `json:"delivered_seq"`
	ReadSeq        uint64 `json:"read_seq"`
	UpdatedAtMs    int64  `json:"updated_at_ms"`
}

type presenceSubscribePayload struct {
	UserIDs []uint64 `json:"user_ids"`
}

type presenceSubscribeResponsePayload struct {
	Events []presenceEventPayload `json:"events"`
}

type presenceEventPayload struct {
	UserID          uint64          `json:"user_id"`
	Status          presence.Status `json:"status"`
	OnlineDeviceIDs []string        `json:"online_device_ids"`
}

type errorPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (c *wsClient) handleSendMessage(ctx context.Context, pkt Packet) {
	if c.messages == nil {
		c.writeError(pkt.TraceID, string(apperrors.SysUnavailable), "message sender is unavailable", true)
		return
	}
	var payload sendMessagePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), err.Error(), false)
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Payload)
	if payload.Payload != "" && err != nil {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), err.Error(), false)
		return
	}
	resp, err := c.messages.Send(ctx, message.SendRequest{
		ConversationID: payload.ConversationID,
		SenderID:       c.principal.UserID,
		SenderDeviceID: c.principal.DeviceID,
		ClientMsgID:    payload.ClientMsgID,
		Type:           payload.Type,
		Payload:        decoded,
	})
	if err != nil {
		code := string(apperrors.SysInternal)
		messageText := "internal error"
		retryable := true
		if appErr, ok := err.(apperrors.AppError); ok {
			code = string(appErr.Code)
			messageText = appErr.Message
			retryable = appErr.Retryable
		}
		c.writeError(pkt.TraceID, code, messageText, retryable)
		return
	}
	c.writeJSON("send_message_ack", pkt.TraceID, sendMessageAckPayload{
		MsgID:          resp.Message.ID,
		ConversationID: resp.Message.ConversationID,
		Seq:            resp.Message.Seq,
		ClientMsgID:    resp.Message.ClientMsgID,
		Stage:          resp.Stage,
		CreatedAtMs:    resp.Message.CreatedAtMs,
	})
	c.syncOwnOtherDevices(resp.Message)
}

func (c *wsClient) handleSyncMessages(ctx context.Context, pkt Packet) {
	if c.messages == nil {
		c.writeError(pkt.TraceID, string(apperrors.SysUnavailable), "message service is unavailable", true)
		return
	}
	var payload syncMessagesPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), err.Error(), false)
		return
	}
	if payload.ConversationID == 0 {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), "conversation_id is required", false)
		return
	}
	requestedLimit := payload.Limit
	messages, err := c.messages.Sync(ctx, payload.ConversationID, payload.FromSeq, payload.Limit)
	if err != nil {
		code := string(apperrors.SysInternal)
		messageText := "internal error"
		retryable := true
		if appErr, ok := err.(apperrors.AppError); ok {
			code = string(appErr.Code)
			messageText = appErr.Message
			retryable = appErr.Retryable
		}
		c.writeError(pkt.TraceID, code, messageText, retryable)
		return
	}
	out := make([]messagePayload, 0, len(messages))
	for _, msg := range messages {
		out = append(out, toMessagePayload(msg))
	}
	c.writeJSON("sync_messages_response", pkt.TraceID, syncMessagesResponsePayload{Messages: out, HasMore: requestedLimit > 0 && len(out) == requestedLimit})
}

func toMessagePayload(msg message.Message) messagePayload {
	return messagePayload{MsgID: msg.ID, ConversationID: msg.ConversationID, Seq: msg.Seq, SenderID: msg.SenderID, SenderDeviceID: msg.SenderDeviceID, ClientMsgID: msg.ClientMsgID, Type: msg.Type, Payload: base64.StdEncoding.EncodeToString(msg.Payload), CreatedAtMs: msg.CreatedAtMs}
}

func (c *wsClient) handleRecallMessage(ctx context.Context, pkt Packet) {
	if c.messages == nil {
		c.writeError(pkt.TraceID, string(apperrors.SysUnavailable), "message service is unavailable", true)
		return
	}
	var payload recallMessagePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), err.Error(), false)
		return
	}
	if payload.MessageID == 0 {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), "msg_id is required", false)
		return
	}
	msg, err := c.messages.Recall(ctx, message.RecallRequest{
		MessageID: payload.MessageID,
		SenderID:  c.principal.UserID,
	})
	if err != nil {
		code := string(apperrors.SysInternal)
		messageText := "internal error"
		retryable := true
		if appErr, ok := err.(apperrors.AppError); ok {
			code = string(appErr.Code)
			messageText = appErr.Message
			retryable = appErr.Retryable
		}
		c.writeError(pkt.TraceID, code, messageText, retryable)
		return
	}
	c.writeJSON("recall_message_ack", pkt.TraceID, recallMessageAckPayload{
		MessageID:    msg.ID,
		Status:       msg.Status,
		RecalledAtMs: msg.RecalledAtMs,
	})
}

func (c *wsClient) handleDeleteMessage(ctx context.Context, pkt Packet) {
	if c.messages == nil {
		c.writeError(pkt.TraceID, string(apperrors.SysUnavailable), "message service is unavailable", true)
		return
	}
	var payload recallMessagePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), err.Error(), false)
		return
	}
	if payload.MessageID == 0 {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), "msg_id is required", false)
		return
	}
	msg, err := c.messages.Delete(ctx, message.DeleteRequest{
		MessageID: payload.MessageID,
		SenderID:  c.principal.UserID,
	})
	if err != nil {
		code := string(apperrors.SysInternal)
		messageText := "internal error"
		retryable := true
		if appErr, ok := err.(apperrors.AppError); ok {
			code = string(appErr.Code)
			messageText = appErr.Message
			retryable = appErr.Retryable
		}
		c.writeError(pkt.TraceID, code, messageText, retryable)
		return
	}
	c.writeJSON("delete_message_ack", pkt.TraceID, recallMessageAckPayload{
		MessageID:    msg.ID,
		Status:       msg.Status,
		RecalledAtMs: msg.RecalledAtMs,
	})
}

func (c *wsClient) handleEditMessage(ctx context.Context, pkt Packet) {
	if c.messages == nil {
		c.writeError(pkt.TraceID, string(apperrors.SysUnavailable), "message service is unavailable", true)
		return
	}
	var payload editMessagePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), err.Error(), false)
		return
	}
	if payload.MessageID == 0 {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), "msg_id is required", false)
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Payload)
	if payload.Payload != "" && err != nil {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), err.Error(), false)
		return
	}
	msg, err := c.messages.Edit(ctx, message.EditRequest{
		MessageID: payload.MessageID,
		SenderID:  c.principal.UserID,
		Payload:   decoded,
	})
	if err != nil {
		code := string(apperrors.SysInternal)
		messageText := "internal error"
		retryable := true
		if appErr, ok := err.(apperrors.AppError); ok {
			code = string(appErr.Code)
			messageText = appErr.Message
			retryable = appErr.Retryable
		}
		c.writeError(pkt.TraceID, code, messageText, retryable)
		return
	}
	c.writeJSON("edit_message_ack", pkt.TraceID, editMessageAckPayload{
		MessageID:  msg.ID,
		Status:     msg.Status,
		Payload:    base64.StdEncoding.EncodeToString(msg.Payload),
		EditedAtMs: msg.EditedAtMs,
	})
}

func clientKey(userID uint64, deviceID string) string {
	return fmt.Sprintf("%d:%s", userID, deviceID)
}

func (c *wsClient) handlePresenceSubscribe(pkt Packet) {
	if c.presence == nil {
		c.writeError(pkt.TraceID, string(apperrors.SysUnavailable), "presence store is unavailable", true)
		return
	}
	var payload presenceSubscribePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), err.Error(), false)
		return
	}
	if len(payload.UserIDs) == 0 {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), "user_ids is required", false)
		return
	}
	events := make([]presenceEventPayload, 0, len(payload.UserIDs))
	for _, userID := range payload.UserIDs {
		if userID == 0 {
			continue
		}
		snapshot := c.presence.Snapshot(userID)
		event := presenceEventPayload{UserID: snapshot.UserID, Status: snapshot.Status}
		for _, device := range snapshot.Devices {
			event.OnlineDeviceIDs = append(event.OnlineDeviceIDs, device.DeviceID)
		}
		events = append(events, event)
		// Register this client as a subscriber for proactive presence push.
		if c.handler != nil {
			c.handler.addPresenceSubscriber(userID, c)
			c.subscribedTo = append(c.subscribedTo, userID)
		}
	}
	c.writeJSON("presence_subscribe_response", pkt.TraceID, presenceSubscribeResponsePayload{Events: events})
}

func (c *wsClient) handleReceiptAck(pkt Packet, read bool) {
	if c.receipts == nil {
		c.writeError(pkt.TraceID, string(apperrors.SysUnavailable), "receipt store is unavailable", true)
		return
	}
	var payload receiptAckPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), err.Error(), false)
		return
	}
	if payload.ConversationID == 0 || payload.Seq == 0 {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), "conversation_id and seq are required", false)
		return
	}
	updatedAtMs := c.now().UnixMilli()
	var r receipt.Receipt
	command := "delivery_ack_response"
	if read {
		r = c.receipts.MarkRead(payload.ConversationID, c.principal.UserID, c.principal.DeviceID, payload.Seq, updatedAtMs)
		command = "read_ack_response"
	} else {
		r = c.receipts.MarkDelivered(payload.ConversationID, c.principal.UserID, c.principal.DeviceID, payload.Seq, updatedAtMs)
	}
	c.writeJSON(command, pkt.TraceID, receiptAckResponsePayload{ConversationID: r.ConversationID, DeliveredSeq: r.DeliveredSeq, ReadSeq: r.ReadSeq, UpdatedAtMs: r.UpdatedAtMs})
}

type batchReceiptAckPayload struct {
	ConversationID uint64   `json:"conversation_id"`
	Seqs           []uint64 `json:"seqs"`
}

func (c *wsClient) handleBatchReceiptAck(pkt Packet, read bool) {
	if c.receipts == nil {
		c.writeError(pkt.TraceID, string(apperrors.SysUnavailable), "receipt store is unavailable", true)
		return
	}
	var payload batchReceiptAckPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), err.Error(), false)
		return
	}
	if payload.ConversationID == 0 || len(payload.Seqs) == 0 {
		c.writeError(pkt.TraceID, string(apperrors.SysBadRequest), "conversation_id and seqs are required", false)
		return
	}
	updatedAtMs := c.now().UnixMilli()
	var r receipt.Receipt
	command := "batch_delivery_ack_response"
	if read {
		r = c.receipts.MarkReadBatch(payload.ConversationID, c.principal.UserID, c.principal.DeviceID, payload.Seqs, updatedAtMs)
		command = "batch_read_ack_response"
	} else {
		r = c.receipts.MarkDeliveredBatch(payload.ConversationID, c.principal.UserID, c.principal.DeviceID, payload.Seqs, updatedAtMs)
	}
	c.writeJSON(command, pkt.TraceID, receiptAckResponsePayload{ConversationID: r.ConversationID, DeliveredSeq: r.DeliveredSeq, ReadSeq: r.ReadSeq, UpdatedAtMs: r.UpdatedAtMs})
}

func (c *wsClient) write(pkt Packet) {
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = c.conn.WriteJSON(pkt)
}

func (c *wsClient) writeJSON(command, traceID string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		c.writeError(traceID, string(apperrors.SysInternal), "marshal response failed", true)
		return
	}
	c.write(Packet{Command: command, TraceID: traceID, Payload: b})
}

func (c *wsClient) writeError(traceID, code, messageText string, retryable bool) {
	c.writeJSON("error", traceID, errorPayload{Code: code, Message: messageText, Retryable: retryable})
}

func (c *wsClient) syncOwnOtherDevices(msg message.Message) {
	if c.syncPusher != nil {
		c.syncPusher(msg)
	}
}

// unsubscribeAll removes this client from all presence subscription sets it joined.
// Called on disconnect.
func (c *wsClient) unsubscribeAll() {
	if c.handler == nil {
		return
	}
	for _, userID := range c.subscribedTo {
		c.handler.removePresenceSubscriber(userID, c.id)
	}
}

// addPresenceSubscriber registers client as a subscriber for the given watched userID.
func (h *WebSocketHandler) addPresenceSubscriber(watchedUserID uint64, client *wsClient) {
	key := presenceSubsKey(watchedUserID)
	actual, _ := h.presenceSubs.LoadOrStore(key, &sync.Map{})
	subs := actual.(*sync.Map)
	subs.Store(client.id, client)
}

// removePresenceSubscriber removes a subscriber by clientID for the given watched userID.
func (h *WebSocketHandler) removePresenceSubscriber(watchedUserID uint64, clientID string) {
	key := presenceSubsKey(watchedUserID)
	if val, ok := h.presenceSubs.Load(key); ok {
		subs := val.(*sync.Map)
		subs.Delete(clientID)
	}
}

// notifyPresenceChange pushes a presence_changed packet to all clients subscribed
// to the given userID. It reads the current snapshot from the presence store.
func (h *WebSocketHandler) notifyPresenceChange(userID uint64) {
	key := presenceSubsKey(userID)
	val, ok := h.presenceSubs.Load(key)
	if !ok {
		return
	}
	subs := val.(*sync.Map)

	// Build the event payload from the current presence snapshot.
	var event presenceEventPayload
	if h.presence != nil {
		snapshot := h.presence.Snapshot(userID)
		event = presenceEventPayload{UserID: snapshot.UserID, Status: snapshot.Status}
		for _, d := range snapshot.Devices {
			event.OnlineDeviceIDs = append(event.OnlineDeviceIDs, d.DeviceID)
		}
	} else {
		event = presenceEventPayload{UserID: userID, Status: 0}
	}

	seen := make(map[string]struct{})
	subs.Range(func(_, value any) bool {
		client, ok := value.(*wsClient)
		if !ok || client == nil {
			return true
		}
		if _, dup := seen[client.id]; dup {
			return true
		}
		seen[client.id] = struct{}{}
		client.writeJSON("presence_changed", "", event)
		return true
	})
}
