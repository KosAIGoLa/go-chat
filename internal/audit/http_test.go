package audit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ck-chat/ck-chat/internal/auth"
	"github.com/ck-chat/ck-chat/internal/httpapi"
)

func parseEvents(t *testing.T, body []byte) []eventJSON {
	t.Helper()
	var resp httpapi.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	b, _ := json.Marshal(resp.Data)
	var events []eventJSON
	_ = json.Unmarshal(b, &events)
	return events
}

func TestAuditHTTPListEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore()
	router := gin.New()
	NewHTTPHandler(store).Register(router)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit/events", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	events := parseEvents(t, rec.Body.Bytes())
	if len(events) != 0 {
		t.Fatalf("expected empty events, got %+v", events)
	}
}

func TestAuditHTTPListAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore()
	store.Append(Event{ID: 1, TenantID: 10, ActorUserID: 101, Action: "message.send", CreatedAtMs: 1000})
	store.Append(Event{ID: 2, TenantID: 20, ActorUserID: 202, Action: "message.delete", CreatedAtMs: 2000})
	router := gin.New()
	NewHTTPHandler(store).Register(router)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit/events", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	events := parseEvents(t, rec.Body.Bytes())
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %+v", events)
	}
}

func TestAuditHTTPFilterByTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore()
	store.Append(Event{ID: 1, TenantID: 10, ActorUserID: 101, Action: "message.send", CreatedAtMs: 1000})
	store.Append(Event{ID: 2, TenantID: 20, ActorUserID: 202, Action: "message.delete", CreatedAtMs: 2000})
	router := gin.New()
	NewHTTPHandler(store).Register(router)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit/events?tenant_id=10", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	events := parseEvents(t, rec.Body.Bytes())
	if len(events) != 1 || events[0].TenantID != 10 {
		t.Fatalf("expected one tenant 10 event, got %+v", events)
	}
}

func TestAuditHTTPAdminScopeRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	token, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 1, DeviceID: "ios", Scopes: []string{"user"}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	protected := router.Group("/")
	protected.Use(auth.Middleware(tokens))
	NewHTTPHandler(NewStore()).Register(protected)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
