package audit

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ck-chat/ck-chat/internal/auth"
	apperrors "github.com/ck-chat/ck-chat/internal/errors"
	"github.com/ck-chat/ck-chat/internal/httpapi"
)

// HTTPHandler exposes the audit-event admin API.
type HTTPHandler struct {
	store *Store
}

// NewHTTPHandler creates an HTTPHandler backed by the given Store.
func NewHTTPHandler(store *Store) *HTTPHandler {
	return &HTTPHandler{store: store}
}

// Register mounts the audit routes onto the router.
//
//	GET /api/v1/admin/audit/events — list audit events (optionally filtered by tenant_id)
func (h *HTTPHandler) Register(router gin.IRouter) {
	router.GET("/api/v1/admin/audit/events", h.listEvents)
}

// eventJSON is the JSON representation of an audit Event.
type eventJSON struct {
	ID           uint64 `json:"id"`
	TenantID     uint64 `json:"tenant_id"`
	ActorUserID  uint64 `json:"actor_user_id"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	CreatedAtMs  int64  `json:"created_at_ms"`
}

func toEventJSON(e Event) eventJSON {
	return eventJSON{
		ID:           e.ID,
		TenantID:     e.TenantID,
		ActorUserID:  e.ActorUserID,
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		CreatedAtMs:  e.CreatedAtMs,
	}
}

func (h *HTTPHandler) listEvents(c *gin.Context) {
	// Scope enforcement: if an authenticated principal is present and has a
	// non-empty scopes slice, it must include "admin".  When no principal is
	// present (or scopes are empty) we allow access as a local-dev fallback,
	// matching the pattern used by the conversation handler.
	if principal, ok := auth.PrincipalFromContext(c.Request.Context()); ok {
		if len(principal.Scopes) > 0 && !containsAdmin(principal.Scopes) {
			httpapi.GinError(c, http.StatusForbidden, apperrors.AppError{
				Code:      apperrors.AuthForbidden,
				Message:   "admin scope required",
				Retryable: false,
			})
			return
		}
	}

	// Optional tenant_id query param for filtering.
	var events []Event
	if raw := c.Query("tenant_id"); raw != "" {
		tenantID, err := strconv.ParseUint(raw, 10, 64)
		if err == nil && tenantID > 0 {
			events = h.store.ListByTenant(tenantID)
		} else {
			events = h.store.All()
		}
	} else {
		events = h.store.All()
	}

	out := make([]eventJSON, 0, len(events))
	for _, e := range events {
		out = append(out, toEventJSON(e))
	}
	httpapi.GinJSON(c, http.StatusOK, out)
}

// containsAdmin reports whether scopes contains the "admin" value.
func containsAdmin(scopes []string) bool {
	for _, s := range scopes {
		if s == "admin" {
			return true
		}
	}
	return false
}
