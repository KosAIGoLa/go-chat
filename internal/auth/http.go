package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	apperrors "github.com/ck-chat/ck-chat/internal/errors"
	"github.com/ck-chat/ck-chat/internal/httpapi"
)

type HTTPHandler struct {
	tokens *TokenService
	ttl    time.Duration
}

func NewHTTPHandler(tokens *TokenService, ttl time.Duration) *HTTPHandler {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &HTTPHandler{tokens: tokens, ttl: ttl}
}

func (h *HTTPHandler) Register(router gin.IRouter) {
	router.POST("/api/v1/auth/login", h.login)
}

type loginRequest struct {
	TenantID uint64   `json:"tenant_id"`
	UserID   uint64   `json:"user_id"`
	DeviceID string   `json:"device_id"`
	Scopes   []string `json:"scopes"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (h *HTTPHandler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.GinError(c, http.StatusBadRequest, err)
		return
	}
	if req.UserID == 0 || req.DeviceID == "" {
		httpapi.GinError(c, http.StatusBadRequest, apperrors.AppError{Code: apperrors.SysBadRequest, Message: "user_id and device_id are required", Retryable: false})
		return
	}
	if req.TenantID == 0 {
		req.TenantID = 1
	}
	if len(req.Scopes) == 0 {
		req.Scopes = []string{"message:send", "message:sync"}
	}
	token, err := h.tokens.Issue(Principal{TenantID: req.TenantID, UserID: req.UserID, DeviceID: req.DeviceID, Scopes: req.Scopes}, h.ttl)
	if err != nil {
		httpapi.GinError(c, http.StatusInternalServerError, err)
		return
	}
	httpapi.GinJSON(c, http.StatusOK, loginResponse{AccessToken: token, TokenType: "Bearer", ExpiresIn: int64(h.ttl.Seconds())})
}
