package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/ck-chat/ck-chat/internal/auth"
	apperrors "github.com/ck-chat/ck-chat/internal/errors"
	"github.com/ck-chat/ck-chat/internal/message"
	"github.com/ck-chat/ck-chat/internal/route"
	"github.com/ck-chat/ck-chat/internal/sequence"
)

func TestWebSocketHeartbeat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewWebSocketHandler().Register(router)
	server := httptest.NewServer(router)
	defer server.Close()

	url := "ws" + server.URL[len("http"):] + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial failed status=%d err=%v", resp.StatusCode, err)
		}
		t.Fatal(err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(Packet{Command: "ping", TraceID: "t1"}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "pong" || pkt.TraceID != "t1" {
		t.Fatalf("unexpected packet: %+v", pkt)
	}
	_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

func TestWebSocketRejectsMissingTokenWhenAuthEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	router := gin.New()
	NewWebSocketHandler().WithAuth(tokens).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()

	url := "ws" + server.URL[len("http"):] + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected missing token to be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized response, got resp=%v err=%v", resp, err)
	}
}

func TestWebSocketRegistersAndUnregistersRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	token, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 42, DeviceID: "ios"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	routes := route.NewRegistry()
	router := gin.New()
	NewWebSocketHandler().WithAuth(tokens).WithRouteRegistry(routes).WithGatewayID("gw-1").Register(router)
	server := httptest.NewServer(router)
	defer server.Close()

	url := "ws" + server.URL[len("http"):] + "/api/v1/gateway/ws?access_token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial failed status=%d err=%v", resp.StatusCode, err)
		}
		t.Fatal(err)
	}

	registered := routes.Get(42)
	if len(registered) != 1 || registered[0].DeviceID != "ios" || registered[0].GatewayID != "gw-1" || registered[0].Protocol != "websocket" {
		t.Fatalf("unexpected registered route: %+v", registered)
	}
	if err := conn.WriteJSON(Packet{Command: "heartbeat", TraceID: "hb1"}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "pong" || pkt.TraceID != "hb1" {
		t.Fatalf("unexpected heartbeat response: %+v", pkt)
	}
	_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = conn.Close()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(routes.Get(42)) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("route was not unregistered: %+v", routes.Get(42))
}

func TestWebSocketSendMessageAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	token, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 42, DeviceID: "ios"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	svc := message.NewService(sequence.NewAllocator(), message.NewMemoryStore())
	router := gin.New()
	NewWebSocketHandler().WithAuth(tokens).WithMessageSender(svc).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws?access_token="+token)
	defer conn.Close()

	payload, err := json.Marshal(sendMessagePayload{ConversationID: 10, ClientMsgID: "c1", Type: 1, Payload: base64.StdEncoding.EncodeToString([]byte("hello"))})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "send_message", TraceID: "send-1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var ack Packet
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatal(err)
	}
	if ack.Command != "send_message_ack" || ack.TraceID != "send-1" {
		t.Fatalf("unexpected ack packet: %+v", ack)
	}
	var ackPayload sendMessageAckPayload
	if err := json.Unmarshal(ack.Payload, &ackPayload); err != nil {
		t.Fatal(err)
	}
	if ackPayload.ConversationID != 10 || ackPayload.Seq != 1 || ackPayload.ClientMsgID != "c1" || ackPayload.MsgID == 0 {
		t.Fatalf("unexpected ack payload: %+v", ackPayload)
	}
}

func TestWebSocketSendMessageValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewWebSocketHandler().WithMessageSender(errorSender{}).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws")
	defer conn.Close()

	if err := conn.WriteJSON(Packet{Command: "send_message", TraceID: "bad-1", Payload: json.RawMessage(`{"conversation_id":10,"client_msg_id":"c1","type":1,"payload":"%%%"}`)}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "error" || pkt.TraceID != "bad-1" {
		t.Fatalf("unexpected error packet: %+v", pkt)
	}
	var payload errorPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != string(apperrors.SysBadRequest) || payload.Retryable {
		t.Fatalf("unexpected error payload: %+v", payload)
	}
}

func TestWebSocketSyncMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := message.NewService(sequence.NewAllocator(), message.NewMemoryStore())
	if _, err := svc.Send(context.Background(), message.SendRequest{ConversationID: 10, SenderID: 42, SenderDeviceID: "ios", ClientMsgID: "c1", Type: 1, Payload: []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	NewWebSocketHandler().WithMessageSender(svc).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws")
	defer conn.Close()

	payload, err := json.Marshal(syncMessagesPayload{ConversationID: 10, FromSeq: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "sync_messages", TraceID: "sync-1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "sync_messages_response" || pkt.TraceID != "sync-1" {
		t.Fatalf("unexpected sync packet: %+v", pkt)
	}
	var resp syncMessagesResponsePayload
	if err := json.Unmarshal(pkt.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Messages) != 1 || resp.Messages[0].Seq != 1 || resp.Messages[0].Payload != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Fatalf("unexpected sync response: %+v", resp)
	}
}

func TestWebSocketSyncMessagesValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewWebSocketHandler().WithMessageSender(errorSender{}).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws")
	defer conn.Close()

	payload, err := json.Marshal(syncMessagesPayload{FromSeq: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "sync_messages", TraceID: "sync-bad-1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "error" || pkt.TraceID != "sync-bad-1" {
		t.Fatalf("unexpected error packet: %+v", pkt)
	}
	var resp errorPayload
	if err := json.Unmarshal(pkt.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != string(apperrors.SysBadRequest) || resp.Retryable {
		t.Fatalf("unexpected error response: %+v", resp)
	}
}

func TestWebSocketRouteRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewWebSocketHandler().Register(router)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gateway/ws", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected websocket upgrade failure on registered route, got %d", rec.Code)
	}
}

func dialWebSocket(t *testing.T, serverURL, path string) *websocket.Conn {
	t.Helper()
	url := "ws" + serverURL[len("http"):] + path
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial failed status=%d err=%v", resp.StatusCode, err)
		}
		t.Fatal(err)
	}
	return conn
}

type errorSender struct{}

func (errorSender) Send(context.Context, message.SendRequest) (message.SendResponse, error) {
	return message.SendResponse{}, nil
}

func (errorSender) Sync(context.Context, uint64, uint64, int) ([]message.Message, error) {
	return nil, nil
}
