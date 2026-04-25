package message

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ck-chat/ck-chat/internal/auth"
	"github.com/ck-chat/ck-chat/internal/sequence"
)

func TestHTTPHandlerUsesPrincipalForSender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	router := gin.New()
	NewHTTPHandler(svc).Register(router)
	ctx := auth.ContextWithPrincipal(httptest.NewRequest(http.MethodPost, "/", nil).Context(), auth.Principal{UserID: 42, DeviceID: "pc"})
	body := []byte(`{"conversation_id":10,"sender_id":999,"sender_device_id":"spoofed","client_msg_id":"c1","type":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var decoded struct {
		Data sendMessageResponseJSON `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Data.Message.SenderID != 42 || decoded.Data.Message.SenderDeviceID != "pc" {
		t.Fatalf("expected principal sender, got %+v", decoded.Data.Message)
	}
}
