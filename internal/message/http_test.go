package message

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apperrors "github.com/ck-chat/ck-chat/internal/errors"
	"github.com/ck-chat/ck-chat/internal/ratelimit"
	"github.com/ck-chat/ck-chat/internal/sequence"
)

func TestHTTPHandlerSendAndSync(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	router := gin.New()
	NewHTTPHandler(svc).Register(router)

	body := []byte(`{"conversation_id":10,"sender_id":20,"sender_device_id":"ios","client_msg_id":"c1","type":1,"payload":"aGVsbG8="}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	syncReq := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/10/messages?from_seq=1&limit=10", nil)
	syncRec := httptest.NewRecorder()
	router.ServeHTTP(syncRec, syncReq)
	if syncRec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", syncRec.Code, syncRec.Body.String())
	}
	var decoded struct {
		Data struct {
			Messages []messageJSON `json:"messages"`
		} `json:"data"`
	}
	if err := json.NewDecoder(syncRec.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data.Messages) != 1 || decoded.Data.Messages[0].Seq != 1 {
		t.Fatalf("unexpected response: %+v", decoded)
	}
}

func TestHTTPHandlerRateLimitsMessageSend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	handler := NewHTTPHandler(svc).WithRateLimiter(ratelimit.New(1, time.Second))
	handler.now = func() time.Time { return time.Unix(100, 0) }
	router := gin.New()
	handler.Register(router)

	body := []byte(`{"conversation_id":10,"sender_id":20,"sender_device_id":"ios","client_msg_id":"c1","type":1}`)
	first := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(body))
	first.RemoteAddr = "192.0.2.1:1234"
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusAccepted {
		t.Fatalf("unexpected first status: %d body=%s", firstRec.Code, firstRec.Body.String())
	}

	secondBody := []byte(`{"conversation_id":10,"sender_id":20,"sender_device_id":"ios","client_msg_id":"c2","type":1}`)
	second := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(secondBody))
	second.RemoteAddr = "192.0.2.1:1234"
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("unexpected second status: %d body=%s", secondRec.Code, secondRec.Body.String())
	}
	if secondRec.Header().Get("Retry-After") != "1" {
		t.Fatalf("expected Retry-After header, got %q", secondRec.Header().Get("Retry-After"))
	}
	var decoded struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(secondRec.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Error.Code != string(apperrors.MsgRateLimited) {
		t.Fatalf("unexpected error code: %+v", decoded)
	}
}

func TestHTTPHandlerRecall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	router := gin.New()
	NewHTTPHandler(svc).Register(router)

	// Send a message
	sendBody := []byte(`{"conversation_id":10,"sender_id":20,"sender_device_id":"ios","client_msg_id":"recall-c1","type":1}`)
	sendReq := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(sendBody))
	sendRec := httptest.NewRecorder()
	router.ServeHTTP(sendRec, sendReq)
	if sendRec.Code != http.StatusAccepted {
		t.Fatalf("send failed: %d body=%s", sendRec.Code, sendRec.Body.String())
	}

	var sendResp struct {
		Data struct {
			Message struct {
				MsgID uint64 `json:"msg_id"`
			} `json:"message"`
		} `json:"data"`
	}
	if err := json.NewDecoder(sendRec.Body).Decode(&sendResp); err != nil {
		t.Fatal(err)
	}
	msgID := sendResp.Data.Message.MsgID
	if msgID == 0 {
		t.Fatal("expected non-zero msg_id in send response")
	}

	// Recall the message
	recallBody := []byte(`{"sender_id":20}`)
	recallReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/messages/%d/recall", msgID), bytes.NewReader(recallBody))
	recallRec := httptest.NewRecorder()
	router.ServeHTTP(recallRec, recallReq)
	if recallRec.Code != http.StatusOK {
		t.Fatalf("recall failed: %d body=%s", recallRec.Code, recallRec.Body.String())
	}

	var recallResp struct {
		Data struct {
			Message messageJSON `json:"message"`
		} `json:"data"`
	}
	if err := json.NewDecoder(recallRec.Body).Decode(&recallResp); err != nil {
		t.Fatal(err)
	}
	if recallResp.Data.Message.Status != MessageStatusRecalled {
		t.Fatalf("expected recalled status, got %v", recallResp.Data.Message.Status)
	}
	if recallResp.Data.Message.RecalledAtMs == 0 {
		t.Fatal("expected non-zero recalled_at_ms")
	}
}

func TestHTTPHandlerRecallRejectsNonSender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	router := gin.New()
	NewHTTPHandler(svc).Register(router)

	// Send as sender 20
	sendBody := []byte(`{"conversation_id":10,"sender_id":20,"sender_device_id":"ios","client_msg_id":"recall-c2","type":1}`)
	sendReq := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(sendBody))
	sendRec := httptest.NewRecorder()
	router.ServeHTTP(sendRec, sendReq)
	if sendRec.Code != http.StatusAccepted {
		t.Fatalf("send failed: %d body=%s", sendRec.Code, sendRec.Body.String())
	}

	var sendResp struct {
		Data struct {
			Message struct {
				MsgID uint64 `json:"msg_id"`
			} `json:"message"`
		} `json:"data"`
	}
	if err := json.NewDecoder(sendRec.Body).Decode(&sendResp); err != nil {
		t.Fatal(err)
	}
	msgID := sendResp.Data.Message.MsgID

	// Try to recall as sender 21
	recallBody := []byte(`{"sender_id":21}`)
	recallReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/messages/%d/recall", msgID), bytes.NewReader(recallBody))
	recallRec := httptest.NewRecorder()
	router.ServeHTTP(recallRec, recallReq)
	if recallRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", recallRec.Code, recallRec.Body.String())
	}

	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(recallRec.Body).Decode(&errResp); err != nil {
		t.Fatal(err)
	}
	if errResp.Error.Code != string(apperrors.MsgRecallNotAllowed) {
		t.Fatalf("expected MSG_RECALL_NOT_ALLOWED, got %q", errResp.Error.Code)
	}
}

func TestHTTPHandlerRecallNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	router := gin.New()
	NewHTTPHandler(svc).Register(router)

	recallBody := []byte(`{"sender_id":20}`)
	recallReq := httptest.NewRequest(http.MethodPost, "/api/v1/messages/999999/recall", bytes.NewReader(recallBody))
	recallRec := httptest.NewRecorder()
	router.ServeHTTP(recallRec, recallReq)
	if recallRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", recallRec.Code, recallRec.Body.String())
	}

	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(recallRec.Body).Decode(&errResp); err != nil {
		t.Fatal(err)
	}
	if errResp.Error.Code != string(apperrors.MsgNotFound) {
		t.Fatalf("expected MSG_NOT_FOUND, got %q", errResp.Error.Code)
	}
}

func TestHTTPHandlerDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	router := gin.New()
	NewHTTPHandler(svc).Register(router)

	sendBody := []byte(`{"conversation_id":10,"sender_id":20,"sender_device_id":"ios","client_msg_id":"delete-c1","type":1}`)
	sendReq := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(sendBody))
	sendRec := httptest.NewRecorder()
	router.ServeHTTP(sendRec, sendReq)
	if sendRec.Code != http.StatusAccepted {
		t.Fatalf("send failed: %d body=%s", sendRec.Code, sendRec.Body.String())
	}
	var sendResp struct {
		Data struct {
			Message struct {
				MsgID uint64 `json:"msg_id"`
			} `json:"message"`
		} `json:"data"`
	}
	if err := json.NewDecoder(sendRec.Body).Decode(&sendResp); err != nil {
		t.Fatal(err)
	}
	msgID := sendResp.Data.Message.MsgID

	deleteBody := []byte(`{"sender_id":20}`)
	deleteReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/messages/%d", msgID), bytes.NewReader(deleteBody))
	deleteRec := httptest.NewRecorder()
	router.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete failed: %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	var deleteResp struct {
		Data struct {
			Message messageJSON `json:"message"`
		} `json:"data"`
	}
	if err := json.NewDecoder(deleteRec.Body).Decode(&deleteResp); err != nil {
		t.Fatal(err)
	}
	if deleteResp.Data.Message.Status != MessageStatusDeleted {
		t.Fatalf("expected deleted status, got %v", deleteResp.Data.Message.Status)
	}
	if deleteResp.Data.Message.RecalledAtMs == 0 {
		t.Fatal("expected non-zero deleted timestamp")
	}
}

func TestHTTPHandlerDeleteRejectsNonSender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	router := gin.New()
	NewHTTPHandler(svc).Register(router)

	sendBody := []byte(`{"conversation_id":10,"sender_id":20,"sender_device_id":"ios","client_msg_id":"delete-c2","type":1}`)
	sendReq := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(sendBody))
	sendRec := httptest.NewRecorder()
	router.ServeHTTP(sendRec, sendReq)
	if sendRec.Code != http.StatusAccepted {
		t.Fatalf("send failed: %d body=%s", sendRec.Code, sendRec.Body.String())
	}
	var sendResp2 struct {
		Data struct {
			Message struct {
				MsgID uint64 `json:"msg_id"`
			} `json:"message"`
		} `json:"data"`
	}
	if err := json.NewDecoder(sendRec.Body).Decode(&sendResp2); err != nil {
		t.Fatal(err)
	}
	msgID := sendResp2.Data.Message.MsgID

	deleteBody := []byte(`{"sender_id":21}`)
	deleteReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/messages/%d", msgID), bytes.NewReader(deleteBody))
	deleteRec := httptest.NewRecorder()
	router.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	var errResp2 struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(deleteRec.Body).Decode(&errResp2); err != nil {
		t.Fatal(err)
	}
	if errResp2.Error.Code != string(apperrors.MsgDeleteNotAllowed) {
		t.Fatalf("expected MSG_DELETE_NOT_ALLOWED, got %q", errResp2.Error.Code)
	}
}

func TestHTTPHandlerDeleteNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	router := gin.New()
	NewHTTPHandler(svc).Register(router)

	deleteBody := []byte(`{"sender_id":20}`)
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/messages/999999", bytes.NewReader(deleteBody))
	deleteRec := httptest.NewRecorder()
	router.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	var errResp3 struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	
	func TestHTTPHandlerEdit(t *testing.T) {
		gin.SetMode(gin.TestMode)
		svc := NewService(sequence.NewAllocator(), NewMemoryStore())
		router := gin.New()
		NewHTTPHandler(svc).Register(router)
	
		sendBody := []byte(`{"conversation_id":10,"sender_id":20,"sender_device_id":"ios","client_msg_id":"edit-c1","type":1,"payload":"b3JpZ2luYWw="}`)
		sendReq := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(sendBody))
		sendRec := httptest.NewRecorder()
		router.ServeHTTP(sendRec, sendReq)
		if sendRec.Code != http.StatusAccepted {
			t.Fatalf("send failed: %d body=%s", sendRec.Code, sendRec.Body.String())
		}
		var sendResp struct {
			Data struct {
				Message struct {
					MsgID uint64 `json:"msg_id"`
				} `json:"message"`
			} `json:"data"`
		}
		if err := json.NewDecoder(sendRec.Body).Decode(&sendResp); err != nil {
			t.Fatal(err)
		}
		msgID := sendResp.Data.Message.MsgID
	
		editBody := []byte(`{"sender_id":20,"payload":"dXBkYXRlZA=="}`)
		editReq := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/messages/%d", msgID), bytes.NewReader(editBody))
		editRec := httptest.NewRecorder()
		router.ServeHTTP(editRec, editReq)
		if editRec.Code != http.StatusOK {
			t.Fatalf("edit failed: %d body=%s", editRec.Code, editRec.Body.String())
		}
		var editResp struct {
			Data struct {
				Message messageJSON `json:"message"`
			} `json:"data"`
		}
		if err := json.NewDecoder(editRec.Body).Decode(&editResp); err != nil {
			t.Fatal(err)
		}
		if editResp.Data.Message.Status != MessageStatusEdited {
			t.Fatalf("expected edited status, got %v", editResp.Data.Message.Status)
		}
		if editResp.Data.Message.EditedAtMs == 0 {
			t.Fatal("expected non-zero edited_at_ms")
		}
		if editResp.Data.Message.Payload != "dXBkYXRlZA==" {
			t.Fatalf("expected updated payload, got %q", editResp.Data.Message.Payload)
		}
	}
	
	func TestHTTPHandlerEditRejectsNonSender(t *testing.T) {
		gin.SetMode(gin.TestMode)
		svc := NewService(sequence.NewAllocator(), NewMemoryStore())
		router := gin.New()
		NewHTTPHandler(svc).Register(router)
	
		sendBody := []byte(`{"conversation_id":10,"sender_id":20,"sender_device_id":"ios","client_msg_id":"edit-c2","type":1,"payload":"b3JpZ2luYWw="}`)
		sendReq := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(sendBody))
		sendRec := httptest.NewRecorder()
		router.ServeHTTP(sendRec, sendReq)
		if sendRec.Code != http.StatusAccepted {
			t.Fatalf("send failed: %d body=%s", sendRec.Code, sendRec.Body.String())
		}
		var sendResp struct {
			Data struct {
				Message struct {
					MsgID uint64 `json:"msg_id"`
				} `json:"message"`
			} `json:"data"`
		}
		if err := json.NewDecoder(sendRec.Body).Decode(&sendResp); err != nil {
			t.Fatal(err)
		}
		msgID := sendResp.Data.Message.MsgID
	
		editBody := []byte(`{"sender_id":21,"payload":"aGFja2Vk"}`)
		editReq := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/messages/%d", msgID), bytes.NewReader(editBody))
		editRec := httptest.NewRecorder()
		router.ServeHTTP(editRec, editReq)
		if editRec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%s", editRec.Code, editRec.Body.String())
		}
		var errResp struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.NewDecoder(editRec.Body).Decode(&errResp); err != nil {
			t.Fatal(err)
		}
		if errResp.Error.Code != string(apperrors.MsgEditNotAllowed) {
			t.Fatalf("expected MSG_EDIT_NOT_ALLOWED, got %q", errResp.Error.Code)
		}
	}
	
	func TestHTTPHandlerEditNotFound(t *testing.T) {
		gin.SetMode(gin.TestMode)
		svc := NewService(sequence.NewAllocator(), NewMemoryStore())
		router := gin.New()
		NewHTTPHandler(svc).Register(router)
	
		editBody := []byte(`{"sender_id":20,"payload":"dXBkYXRlZA=="}`)
		editReq := httptest.NewRequest(http.MethodPatch, "/api/v1/messages/999999", bytes.NewReader(editBody))
		editRec := httptest.NewRecorder()
		router.ServeHTTP(editRec, editReq)
		if editRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", editRec.Code, editRec.Body.String())
		}
		var errResp struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.NewDecoder(editRec.Body).Decode(&errResp); err != nil {
			t.Fatal(err)
		}
		if errResp.Error.Code != string(apperrors.MsgNotFound) {
			t.Fatalf("expected MSG_NOT_FOUND, got %q", errResp.Error.Code)
		}
	}
	if err := json.NewDecoder(deleteRec.Body).Decode(&errResp3); err != nil {
		t.Fatal(err)
	}
	if errResp3.Error.Code != string(apperrors.MsgNotFound) {
		t.Fatalf("expected MSG_NOT_FOUND, got %q", errResp3.Error.Code)
	}
}
