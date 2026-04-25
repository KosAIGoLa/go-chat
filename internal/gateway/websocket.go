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
}

type WebSocketHandler struct {
	gatewayID string
	tokens    *auth.TokenService
	routes    *route.Registry
	messages  MessageService
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
	client := newClient(conn, principal, h.messages)
	h.clients.Store(client.id, client)
	if h.routes != nil {
		h.registerRoute(principal, client.id)
	}
	defer func() {
		if h.routes != nil {
			h.routes.Unregister(principal.UserID, principal.DeviceID)
		}
		h.clients.Delete(client.id)
		_ = conn.Close()
	}()
	client.loop(c.Request.Context(), func() {
		if h.routes != nil {
			h.registerRoute(principal, client.id)
		}
	})
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
}

func newClient(conn *websocket.Conn, principal auth.Principal, sender MessageService) *wsClient {
	return &wsClient{id: fmt.Sprintf("%d-%d-%s", principal.UserID, time.Now().UnixNano(), principal.DeviceID), conn: conn, principal: principal, messages: sender}
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
