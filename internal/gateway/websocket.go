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
}

type ReceiptStore interface {
	MarkDelivered(conversationID, userID uint64, deviceID string, seq uint64, updatedAtMs int64) receipt.Receipt
	MarkRead(conversationID, userID uint64, deviceID string, seq uint64, updatedAtMs int64) receipt.Receipt
}

type PresenceStore interface {
	Online(userID uint64, device presence.Device)
	Offline(userID uint64, deviceID string)
	Snapshot(userID uint64) presence.Snapshot
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
	now       func() time.Time
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
	client := newClient(conn, principal, h.messages, h.receipts, h.presence, h.now)
	h.clients.Store(client.id, client)
	h.clients.Store(clientKey(principal.UserID, principal.DeviceID), client)
	if h.routes != nil {
		h.registerRoute(principal, client.id)
	}
	if h.presence != nil {
		h.presence.Online(principal.UserID, presence.Device{DeviceID: principal.DeviceID, GatewayID: h.gatewayID, SeenAt: h.now()})
	}
	defer func() {
		if h.routes != nil {
			h.routes.Unregister(principal.UserID, principal.DeviceID)
		}
		if h.presence != nil {
			h.presence.Offline(principal.UserID, principal.DeviceID)
		}
		h.clients.Delete(client.id)
		h.clients.Delete(clientKey(principal.UserID, principal.DeviceID))
		_ = conn.Close()
	}()
	client.loop(c.Request.Context(), func() {
		if h.routes != nil {
			h.registerRoute(principal, client.id)
		}
		if h.presence != nil {
			h.presence.Online(principal.UserID, presence.Device{DeviceID: principal.DeviceID, GatewayID: h.gatewayID, SeenAt: h.now()})
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
	now       func() time.Time
}

func newClient(conn *websocket.Conn, principal auth.Principal, sender MessageService, receipts ReceiptStore, presenceStore PresenceStore, now func() time.Time) *wsClient {
	return &wsClient{id: fmt.Sprintf("%d-%d-%s", principal.UserID, time.Now().UnixNano(), principal.DeviceID), conn: conn, principal: principal, messages: sender, receipts: receipts, presence: presenceStore, now: now}
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
		case "presence_subscribe":
			c.handlePresenceSubscribe(pkt)
		case "recall_message":
			c.handleRecallMessage(ctx, pkt)
		case "delete_message":
			c.handleDeleteMessage(ctx, pkt)
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

type recallMessageAckPayload struct {
	MessageID    uint64                `json:"msg_id"`
	Status       message.MessageStatus `json:"status"`
	RecalledAtMs int64                 `json:"recalled_at_ms"`
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
