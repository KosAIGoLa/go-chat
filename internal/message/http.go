package message

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ck-chat/ck-chat/internal/auth"
	apperrors "github.com/ck-chat/ck-chat/internal/errors"
	"github.com/ck-chat/ck-chat/internal/httpapi"
	"github.com/ck-chat/ck-chat/internal/ratelimit"
)

type HTTPHandler struct {
	service *Service
	limiter *ratelimit.Limiter
	now     func() time.Time
}

func NewHTTPHandler(service *Service) *HTTPHandler {
	return &HTTPHandler{service: service, now: time.Now}
}

func (h *HTTPHandler) WithRateLimiter(limiter *ratelimit.Limiter) *HTTPHandler {
	h.limiter = limiter
	return h
}

func (h *HTTPHandler) Register(router gin.IRouter) {
	router.POST("/api/v1/messages", h.send)
	router.GET("/api/v1/conversations/:conversation_id/messages", h.sync)
	router.POST("/api/v1/messages/:msg_id/recall", h.recall)
}

type sendMessageJSON struct {
	ConversationID uint64 `json:"conversation_id"`
	SenderID       uint64 `json:"sender_id"`
	SenderDeviceID string `json:"sender_device_id"`
	ClientMsgID    string `json:"client_msg_id"`
	Type           int32  `json:"type"`
	Payload        string `json:"payload"`
}

type recallMessageJSON struct {
	SenderID uint64 `json:"sender_id"`
}

type messageJSON struct {
	ID             uint64        `json:"msg_id"`
	ConversationID uint64        `json:"conversation_id"`
	Seq            uint64        `json:"seq"`
	SenderID       uint64        `json:"sender_id"`
	SenderDeviceID string        `json:"sender_device_id"`
	ClientMsgID    string        `json:"client_msg_id"`
	Type           int32         `json:"type"`
	Payload        string        `json:"payload"`
	CreatedAtMs    int64         `json:"created_at_ms"`
	Status         MessageStatus `json:"status"`
	RecalledAtMs   int64         `json:"recalled_at_ms,omitempty"`
}

type sendMessageResponseJSON struct {
	Message messageJSON `json:"message"`
	Stage   AckStage    `json:"stage"`
}

type syncMessagesResponseJSON struct {
	Messages []messageJSON `json:"messages"`
	HasMore  bool          `json:"has_more"`
}

func (h *HTTPHandler) send(c *gin.Context) {
	var req sendMessageJSON
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.GinError(c, http.StatusBadRequest, err)
		return
	}
	payload, err := base64.StdEncoding.DecodeString(req.Payload)
	if req.Payload != "" && err != nil {
		httpapi.GinError(c, http.StatusBadRequest, err)
		return
	}
	principal, ok := auth.PrincipalFromContext(c.Request.Context())
	if ok {
		req.SenderID = principal.UserID
		req.SenderDeviceID = principal.DeviceID
	}
	if h.limiter != nil {
		decision := h.limiter.AllowN(sendLimitKey(req, principal, ok, c.Request), 1, h.now())
		if !decision.Allowed {
			if decision.RetryAfter > 0 {
				c.Header("Retry-After", strconv.FormatInt(int64(decision.RetryAfter.Seconds()+0.999999999), 10))
			}
			httpapi.GinError(c, http.StatusTooManyRequests, apperrors.AppError{Code: apperrors.MsgRateLimited, Message: "message send rate limited", Retryable: true})
			return
		}
	}
	resp, err := h.service.Send(c.Request.Context(), SendRequest{
		ConversationID: req.ConversationID,
		SenderID:       req.SenderID,
		SenderDeviceID: req.SenderDeviceID,
		ClientMsgID:    req.ClientMsgID,
		Type:           req.Type,
		Payload:        payload,
	})
	if err != nil {
		httpapi.GinError(c, httpStatusFor(err), err)
		return
	}
	httpapi.GinJSON(c, http.StatusAccepted, sendMessageResponseJSON{Message: toJSON(resp.Message), Stage: resp.Stage})
}

func (h *HTTPHandler) sync(c *gin.Context) {
	conversationID, err := strconv.ParseUint(c.Param("conversation_id"), 10, 64)
	if err != nil {
		httpapi.GinError(c, http.StatusBadRequest, err)
		return
	}
	fromSeq, _ := strconv.ParseUint(c.Query("from_seq"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	messages, err := h.service.Sync(c.Request.Context(), conversationID, fromSeq, limit)
	if err != nil {
		httpapi.GinError(c, httpStatusFor(err), err)
		return
	}
	out := make([]messageJSON, 0, len(messages))
	for _, msg := range messages {
		out = append(out, toJSON(msg))
	}
	httpapi.GinJSON(c, http.StatusOK, syncMessagesResponseJSON{Messages: out, HasMore: len(out) == limit && limit > 0})
}

func (h *HTTPHandler) recall(c *gin.Context) {
	msgID, err := strconv.ParseUint(c.Param("msg_id"), 10, 64)
	if err != nil {
		httpapi.GinError(c, http.StatusBadRequest, err)
		return
	}

	var senderID uint64
	principal, ok := auth.PrincipalFromContext(c.Request.Context())
	if ok {
		senderID = principal.UserID
	} else {
		var body recallMessageJSON
		if err := c.ShouldBindJSON(&body); err == nil {
			senderID = body.SenderID
		}
	}

	msg, err := h.service.Recall(c.Request.Context(), RecallRequest{
		MessageID: msgID,
		SenderID:  senderID,
	})
	if err != nil {
		httpapi.GinError(c, httpStatusFor(err), err)
		return
	}
	httpapi.GinJSON(c, http.StatusOK, gin.H{"message": toJSON(msg)})
}

func toJSON(msg Message) messageJSON {
	return messageJSON{
		ID:             msg.ID,
		ConversationID: msg.ConversationID,
		Seq:            msg.Seq,
		SenderID:       msg.SenderID,
		SenderDeviceID: msg.SenderDeviceID,
		ClientMsgID:    msg.ClientMsgID,
		Type:           msg.Type,
		Payload:        base64.StdEncoding.EncodeToString(msg.Payload),
		CreatedAtMs:    msg.CreatedAtMs,
		Status:         msg.Status,
		RecalledAtMs:   msg.RecalledAtMs,
	}
}

func sendLimitKey(req sendMessageJSON, principal auth.Principal, authenticated bool, r *http.Request) string {
	if authenticated {
		return fmt.Sprintf("tenant:%d:user:%d:device:%s:message:send", principal.TenantID, principal.UserID, principal.DeviceID)
	}
	if req.SenderID != 0 {
		return fmt.Sprintf("user:%d:device:%s:message:send", req.SenderID, req.SenderDeviceID)
	}
	return "ip:" + strings.TrimSpace(r.RemoteAddr) + ":message:send"
}

func httpStatusFor(err error) int {
	if appErr, ok := err.(apperrors.AppError); ok {
		switch appErr.Code {
		case apperrors.MsgRateLimited:
			return http.StatusTooManyRequests
		case apperrors.SysUnavailable, apperrors.MsgQueueFailed:
			return http.StatusServiceUnavailable
		case apperrors.MsgNotFound:
			return http.StatusNotFound
		case apperrors.MsgRecallNotAllowed:
			return http.StatusForbidden
		}
	}
	return http.StatusBadRequest
}
