package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestHTTPLoginIssuesToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := NewTokenService("secret")
	router := gin.New()
	NewHTTPHandler(tokens, time.Minute).Register(router)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"tenant_id":1,"user_id":2,"device_id":"ios"}`)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var decoded struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	principal, err := tokens.Verify(decoded.Data.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if principal.UserID != 2 || principal.DeviceID != "ios" {
		t.Fatalf("unexpected principal: %+v", principal)
	}
}
