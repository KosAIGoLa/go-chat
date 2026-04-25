package conversation

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ck-chat/ck-chat/internal/auth"
	"github.com/ck-chat/ck-chat/internal/httpapi"
)

// HTTPHandler exposes the per-user conversation settings API.
type HTTPHandler struct {
	store *Store
}

// NewHTTPHandler creates an HTTPHandler backed by the given Store.
func NewHTTPHandler(store *Store) *HTTPHandler {
	return &HTTPHandler{store: store}
}

// Register mounts the conversation settings routes onto the router.
//
//	GET  /api/v1/conversations          — list the caller's conversation settings
//	PATCH /api/v1/conversations/:conversation_id — upsert mute/pin settings
func (h *HTTPHandler) Register(router gin.IRouter) {
	router.GET("/api/v1/conversations", h.list)
	router.PATCH("/api/v1/conversations/:conversation_id", h.update)
}

// settingsJSON is the JSON representation of UserConversationSettings.
type settingsJSON struct {
	UserID         uint64 `json:"user_id"`
	ConversationID uint64 `json:"conversation_id"`
	MuteUntilMs    int64  `json:"mute_until_ms"`
	IsPinned       bool   `json:"is_pinned"`
	UpdatedAtMs    int64  `json:"updated_at_ms"`
}

// updateRequestJSON is the body accepted by the PATCH handler.
type updateRequestJSON struct {
	// UserID is used only when no authenticated principal is available
	// (e.g. in tests without middleware).
	UserID      uint64 `json:"user_id"`
	MuteUntilMs *int64 `json:"mute_until_ms"`
	IsPinned    *bool  `json:"is_pinned"`
}

func toSettingsJSON(s UserConversationSettings) settingsJSON {
	return settingsJSON{
		UserID:         s.UserID,
		ConversationID: s.ConversationID,
		MuteUntilMs:    s.MuteUntilMs,
		IsPinned:       s.IsPinned,
		UpdatedAtMs:    s.UpdatedAtMs,
	}
}

func (h *HTTPHandler) list(c *gin.Context) {
	userID := resolveUserID(c, 0)
	if userID == 0 {
		httpapi.GinError(c, http.StatusBadRequest, errMissingUserID)
		return
	}
	items := h.store.List(userID)
	out := make([]settingsJSON, 0, len(items))
	for _, item := range items {
		out = append(out, toSettingsJSON(item))
	}
	httpapi.GinJSON(c, http.StatusOK, gin.H{"conversations": out})
}

func (h *HTTPHandler) update(c *gin.Context) {
	conversationID, err := strconv.ParseUint(c.Param("conversation_id"), 10, 64)
	if err != nil || conversationID == 0 {
		httpapi.GinError(c, http.StatusBadRequest, errInvalidConversationID)
		return
	}

	var body updateRequestJSON
	if err := c.ShouldBindJSON(&body); err != nil {
		httpapi.GinError(c, http.StatusBadRequest, err)
		return
	}

	userID := resolveUserID(c, body.UserID)
	if userID == 0 {
		httpapi.GinError(c, http.StatusBadRequest, errMissingUserID)
		return
	}

	// Merge into existing settings (create with zero values if not yet present).
	existing, _ := h.store.Get(userID, conversationID)
	existing.UserID = userID
	existing.ConversationID = conversationID
	if body.MuteUntilMs != nil {
		existing.MuteUntilMs = *body.MuteUntilMs
	}
	if body.IsPinned != nil {
		existing.IsPinned = *body.IsPinned
	}

	saved := h.store.Upsert(existing)
	httpapi.GinJSON(c, http.StatusOK, gin.H{"conversation": toSettingsJSON(saved)})
}

// resolveUserID returns the authenticated user ID from context, falling back
// to the supplied fallback (typically from the request body) for test scenarios
// without authentication middleware.
func resolveUserID(c *gin.Context, fallback uint64) uint64 {
	if principal, ok := auth.PrincipalFromContext(c.Request.Context()); ok {
		return principal.UserID
	}
	return fallback
}

// sentinel errors (implement error interface inline to avoid extra file)
type appErr string

func (e appErr) Error() string { return string(e) }

const (
	errMissingUserID         appErr = "user_id is required"
	errInvalidConversationID appErr = "conversation_id must be a positive integer"
)
