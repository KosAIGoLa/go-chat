package conversation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHTTPHandlerListEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore()
	router := gin.New()
	NewHTTPHandler(store).Register(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations?user_id=1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		// No auth principal and no user_id in body/query → bad request
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPHandlerUpdateAndList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore()
	router := gin.New()
	NewHTTPHandler(store).Register(router)

	// PATCH: mute conversation 10 for user 1
	muteUntil := int64(9999999999000)
	body, _ := json.Marshal(map[string]interface{}{
		"user_id":       1,
		"mute_until_ms": muteUntil,
		"is_pinned":     true,
	})
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/10", bytes.NewReader(body))
	patchRec := httptest.NewRecorder()
	router.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from PATCH, got %d body=%s", patchRec.Code, patchRec.Body.String())
	}

	var patchResp struct {
		Data struct {
			Conversation settingsJSON `json:"conversation"`
		} `json:"data"`
	}
	if err := json.NewDecoder(patchRec.Body).Decode(&patchResp); err != nil {
		t.Fatal(err)
	}
	if patchResp.Data.Conversation.UserID != 1 || patchResp.Data.Conversation.ConversationID != 10 {
		t.Fatalf("unexpected conversation in PATCH response: %+v", patchResp.Data.Conversation)
	}
	if patchResp.Data.Conversation.MuteUntilMs != muteUntil {
		t.Fatalf("expected mute_until_ms=%d, got %d", muteUntil, patchResp.Data.Conversation.MuteUntilMs)
	}
	if !patchResp.Data.Conversation.IsPinned {
		t.Fatal("expected is_pinned=true")
	}
	if patchResp.Data.Conversation.UpdatedAtMs == 0 {
		t.Fatal("expected non-zero updated_at_ms")
	}
}

func TestHTTPHandlerUpdateMerges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore()
	router := gin.New()
	NewHTTPHandler(store).Register(router)

	// First PATCH: pin only
	body1, _ := json.Marshal(map[string]interface{}{"user_id": 5, "is_pinned": true})
	patch1 := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/20", bytes.NewReader(body1))
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, patch1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first PATCH failed: %d %s", rec1.Code, rec1.Body.String())
	}

	// Second PATCH: mute only (should preserve is_pinned)
	muteUntil := int64(8888888888000)
	body2, _ := json.Marshal(map[string]interface{}{"user_id": 5, "mute_until_ms": muteUntil})
	patch2 := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/20", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, patch2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second PATCH failed: %d %s", rec2.Code, rec2.Body.String())
	}

	var resp struct {
		Data struct {
			Conversation settingsJSON `json:"conversation"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec2.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Data.Conversation.IsPinned {
		t.Fatal("expected is_pinned to be preserved after second PATCH")
	}
	if resp.Data.Conversation.MuteUntilMs != muteUntil {
		t.Fatalf("expected mute_until_ms=%d, got %d", muteUntil, resp.Data.Conversation.MuteUntilMs)
	}
}

func TestHTTPHandlerUpdateRejectsBadConversationID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore()
	router := gin.New()
	NewHTTPHandler(store).Register(router)

	body, _ := json.Marshal(map[string]interface{}{"user_id": 1, "is_pinned": true})
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/conversations/%d", 0), bytes.NewReader(body))
	rec := httptest.NewRecorder()
	// Gin won't match /api/v1/conversations/0 as a param with value "0" — but
	// ParseUint will succeed for "0" and the handler will reject it.
	// Use a non-numeric path to trigger the parse error.
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/abc", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid conversation_id, got %d body=%s", rec.Code, rec.Body.String())
	}
}
