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
	"github.com/ck-chat/ck-chat/internal/presence"
	"github.com/ck-chat/ck-chat/internal/receipt"
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

func TestWebSocketPushToDevice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	token, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 42, DeviceID: "ios"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWebSocketHandler().WithAuth(tokens)
	router := gin.New()
	handler.Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws?access_token="+token)
	defer conn.Close()

	ok := handler.PushToDevice(42, "ios", message.Message{ID: 99, ConversationID: 10, Seq: 7, SenderID: 1, SenderDeviceID: "pc", ClientMsgID: "c99", Type: 1, Payload: []byte("pushed"), CreatedAtMs: 123})
	if !ok {
		t.Fatal("expected push to connected device")
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "push_message" {
		t.Fatalf("unexpected push packet: %+v", pkt)
	}
	var pushed messagePayload
	if err := json.Unmarshal(pkt.Payload, &pushed); err != nil {
		t.Fatal(err)
	}
	if pushed.MsgID != 99 || pushed.Seq != 7 || pushed.Payload != base64.StdEncoding.EncodeToString([]byte("pushed")) {
		t.Fatalf("unexpected pushed message: %+v", pushed)
	}
	if handler.PushToDevice(42, "android", message.Message{ID: 100}) {
		t.Fatal("unexpected push to offline device")
	}
}

func TestWebSocketPushToUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	iosToken, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 42, DeviceID: "ios"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pcToken, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 42, DeviceID: "pc"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWebSocketHandler().WithAuth(tokens)
	router := gin.New()
	handler.Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	iosConn := dialWebSocket(t, server.URL, "/ws?access_token="+iosToken)
	defer iosConn.Close()
	pcConn := dialWebSocket(t, server.URL, "/ws?access_token="+pcToken)
	defer pcConn.Close()

	if delivered := handler.PushToUser(42, message.Message{ID: 101, ConversationID: 10, Seq: 8, Payload: []byte("fanout")}); delivered != 2 {
		t.Fatalf("expected 2 pushed devices, got %d", delivered)
	}
	for _, conn := range []*websocket.Conn{iosConn, pcConn} {
		var pkt Packet
		if err := conn.ReadJSON(&pkt); err != nil {
			t.Fatal(err)
		}
		if pkt.Command != "push_message" {
			t.Fatalf("unexpected push packet: %+v", pkt)
		}
	}
}

func TestWebSocketDeliveryAndReadAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	token, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 42, DeviceID: "ios"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := receipt.NewStore()
	handler := NewWebSocketHandler().WithAuth(tokens).WithReceiptStore(store)
	handler.now = func() time.Time { return time.UnixMilli(1234) }
	router := gin.New()
	handler.Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws?access_token="+token)
	defer conn.Close()

	deliveryPayload, err := json.Marshal(receiptAckPayload{ConversationID: 10, Seq: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "delivery_ack", TraceID: "delivered-1", Payload: deliveryPayload}); err != nil {
		t.Fatal(err)
	}
	var deliveryResp Packet
	if err := conn.ReadJSON(&deliveryResp); err != nil {
		t.Fatal(err)
	}
	if deliveryResp.Command != "delivery_ack_response" || deliveryResp.TraceID != "delivered-1" {
		t.Fatalf("unexpected delivery ack response: %+v", deliveryResp)
	}
	var delivered receiptAckResponsePayload
	if err := json.Unmarshal(deliveryResp.Payload, &delivered); err != nil {
		t.Fatal(err)
	}
	if delivered.ConversationID != 10 || delivered.DeliveredSeq != 7 || delivered.ReadSeq != 0 || delivered.UpdatedAtMs != 1234 {
		t.Fatalf("unexpected delivered receipt: %+v", delivered)
	}

	readPayload, err := json.Marshal(receiptAckPayload{ConversationID: 10, Seq: 9})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "read_ack", TraceID: "read-1", Payload: readPayload}); err != nil {
		t.Fatal(err)
	}
	var readResp Packet
	if err := conn.ReadJSON(&readResp); err != nil {
		t.Fatal(err)
	}
	if readResp.Command != "read_ack_response" || readResp.TraceID != "read-1" {
		t.Fatalf("unexpected read ack response: %+v", readResp)
	}
	var read receiptAckResponsePayload
	if err := json.Unmarshal(readResp.Payload, &read); err != nil {
		t.Fatal(err)
	}
	if read.DeliveredSeq != 9 || read.ReadSeq != 9 {
		t.Fatalf("unexpected read receipt: %+v", read)
	}
	stored, ok := store.Get(10, 42, "ios")
	if !ok || stored.DeliveredSeq != 9 || stored.ReadSeq != 9 {
		t.Fatalf("unexpected stored receipt: %+v ok=%v", stored, ok)
	}
}

func TestWebSocketBatchDeliveryAndReadAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	token, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 42, DeviceID: "ios"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := receipt.NewStore()
	handler := NewWebSocketHandler().WithAuth(tokens).WithReceiptStore(store)
	handler.now = func() time.Time { return time.UnixMilli(2345) }
	router := gin.New()
	handler.Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws?access_token="+token)
	defer conn.Close()

	deliveryPayload, err := json.Marshal(batchReceiptAckPayload{ConversationID: 10, Seqs: []uint64{7, 3, 9}})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "batch_delivery_ack", TraceID: "batch-delivered-1", Payload: deliveryPayload}); err != nil {
		t.Fatal(err)
	}
	var deliveryResp Packet
	if err := conn.ReadJSON(&deliveryResp); err != nil {
		t.Fatal(err)
	}
	if deliveryResp.Command != "batch_delivery_ack_response" || deliveryResp.TraceID != "batch-delivered-1" {
		t.Fatalf("unexpected batch delivery ack response: %+v", deliveryResp)
	}
	var delivered receiptAckResponsePayload
	if err := json.Unmarshal(deliveryResp.Payload, &delivered); err != nil {
		t.Fatal(err)
	}
	if delivered.ConversationID != 10 || delivered.DeliveredSeq != 9 || delivered.ReadSeq != 0 || delivered.UpdatedAtMs != 2345 {
		t.Fatalf("unexpected batch delivered receipt: %+v", delivered)
	}

	readPayload, err := json.Marshal(batchReceiptAckPayload{ConversationID: 10, Seqs: []uint64{8, 11, 10}})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "batch_read_ack", TraceID: "batch-read-1", Payload: readPayload}); err != nil {
		t.Fatal(err)
	}
	var readResp Packet
	if err := conn.ReadJSON(&readResp); err != nil {
		t.Fatal(err)
	}
	if readResp.Command != "batch_read_ack_response" || readResp.TraceID != "batch-read-1" {
		t.Fatalf("unexpected batch read ack response: %+v", readResp)
	}
	var read receiptAckResponsePayload
	if err := json.Unmarshal(readResp.Payload, &read); err != nil {
		t.Fatal(err)
	}
	if read.DeliveredSeq != 11 || read.ReadSeq != 11 || read.UpdatedAtMs != 2345 {
		t.Fatalf("unexpected batch read receipt: %+v", read)
	}
	stored, ok := store.Get(10, 42, "ios")
	if !ok || stored.DeliveredSeq != 11 || stored.ReadSeq != 11 {
		t.Fatalf("unexpected stored batch receipt: %+v ok=%v", stored, ok)
	}
}

func TestWebSocketReceiptAckValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewWebSocketHandler().WithReceiptStore(receipt.NewStore()).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws")
	defer conn.Close()

	payload, err := json.Marshal(receiptAckPayload{Seq: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "delivery_ack", TraceID: "bad-receipt", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "error" || pkt.TraceID != "bad-receipt" {
		t.Fatalf("unexpected error packet: %+v", pkt)
	}
	var resp errorPayload
	if err := json.Unmarshal(pkt.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != string(apperrors.SysBadRequest) || resp.Retryable {
		t.Fatalf("unexpected receipt error response: %+v", resp)
	}
}

func TestWebSocketBatchReceiptAckValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewWebSocketHandler().WithReceiptStore(receipt.NewStore()).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws")
	defer conn.Close()

	payload, err := json.Marshal(batchReceiptAckPayload{Seqs: []uint64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "batch_delivery_ack", TraceID: "bad-batch-receipt", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "error" || pkt.TraceID != "bad-batch-receipt" {
		t.Fatalf("unexpected error packet: %+v", pkt)
	}
	var resp errorPayload
	if err := json.Unmarshal(pkt.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != string(apperrors.SysBadRequest) || resp.Retryable {
		t.Fatalf("unexpected batch receipt error response: %+v", resp)
	}
}

func TestWebSocketPresenceSubscribe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	presenceStore := presence.NewStore()
	presenceStore.Online(7, presence.Device{DeviceID: "ios", GatewayID: "gw"})
	presenceStore.Online(7, presence.Device{DeviceID: "pc", GatewayID: "gw"})
	router := gin.New()
	NewWebSocketHandler().WithPresenceStore(presenceStore).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws")
	defer conn.Close()

	payload, err := json.Marshal(presenceSubscribePayload{UserIDs: []uint64{7, 8}})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "presence_subscribe", TraceID: "presence-1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "presence_subscribe_response" || pkt.TraceID != "presence-1" {
		t.Fatalf("unexpected presence response packet: %+v", pkt)
	}
	var resp presenceSubscribeResponsePayload
	if err := json.Unmarshal(pkt.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Events) != 2 || resp.Events[0].UserID != 7 || resp.Events[0].Status != presence.StatusOnline || len(resp.Events[0].OnlineDeviceIDs) != 2 || resp.Events[1].Status != presence.StatusOffline {
		t.Fatalf("unexpected presence response: %+v", resp)
	}
}

func TestWebSocketPresenceSubscribeValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewWebSocketHandler().WithPresenceStore(presence.NewStore()).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws")
	defer conn.Close()

	payload, err := json.Marshal(presenceSubscribePayload{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "presence_subscribe", TraceID: "presence-bad-1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "error" || pkt.TraceID != "presence-bad-1" {
		t.Fatalf("unexpected presence error packet: %+v", pkt)
	}
	var resp errorPayload
	if err := json.Unmarshal(pkt.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != string(apperrors.SysBadRequest) || resp.Retryable {
		t.Fatalf("unexpected presence error response: %+v", resp)
	}
}

func TestWebSocketRecallMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	token, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 42, DeviceID: "ios"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	svc := message.NewService(sequence.NewAllocator(), message.NewMemoryStore())
	// Pre-send a message as user 42
	sendResp, err := svc.Send(context.Background(), message.SendRequest{
		ConversationID: 10, SenderID: 42, SenderDeviceID: "ios", ClientMsgID: "recall-ws-1", Type: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	msgID := sendResp.Message.ID

	router := gin.New()
	NewWebSocketHandler().WithAuth(tokens).WithMessageSender(svc).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws?access_token="+token)
	defer conn.Close()

	payload, err := json.Marshal(recallMessagePayload{MessageID: msgID})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "recall_message", TraceID: "recall-1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "recall_message_ack" || pkt.TraceID != "recall-1" {
		t.Fatalf("unexpected recall ack packet: %+v", pkt)
	}
	var ack recallMessageAckPayload
	if err := json.Unmarshal(pkt.Payload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.MessageID != msgID || ack.Status != message.MessageStatusRecalled || ack.RecalledAtMs == 0 {
		t.Fatalf("unexpected recall ack payload: %+v", ack)
	}
}

func TestWebSocketRecallMessageValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewWebSocketHandler().WithMessageSender(errorSender{}).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws")
	defer conn.Close()

	// Send recall with zero msg_id
	payload, err := json.Marshal(recallMessagePayload{MessageID: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "recall_message", TraceID: "recall-bad-1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "error" || pkt.TraceID != "recall-bad-1" {
		t.Fatalf("unexpected error packet: %+v", pkt)
	}
	var resp errorPayload
	if err := json.Unmarshal(pkt.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != string(apperrors.SysBadRequest) || resp.Retryable {
		t.Fatalf("unexpected recall error response: %+v", resp)
	}
}

func TestWebSocketDeleteMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	token, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 42, DeviceID: "ios"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	svc := message.NewService(sequence.NewAllocator(), message.NewMemoryStore())
	sendResp, err := svc.Send(context.Background(), message.SendRequest{
		ConversationID: 10, SenderID: 42, SenderDeviceID: "ios", ClientMsgID: "delete-ws-1", Type: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	msgID := sendResp.Message.ID

	router := gin.New()
	NewWebSocketHandler().WithAuth(tokens).WithMessageSender(svc).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws?access_token="+token)
	defer conn.Close()

	payload, err := json.Marshal(recallMessagePayload{MessageID: msgID})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "delete_message", TraceID: "delete-1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "delete_message_ack" || pkt.TraceID != "delete-1" {
		t.Fatalf("unexpected delete ack packet: %+v", pkt)
	}
	var ack recallMessageAckPayload
	if err := json.Unmarshal(pkt.Payload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.MessageID != msgID || ack.Status != message.MessageStatusDeleted || ack.RecalledAtMs == 0 {
		t.Fatalf("unexpected delete ack payload: %+v", ack)
	}
}

func TestWebSocketEditMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tokens := auth.NewTokenService("secret")
	token, err := tokens.Issue(auth.Principal{TenantID: 1, UserID: 42, DeviceID: "ios"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	svc := message.NewService(sequence.NewAllocator(), message.NewMemoryStore())
	sendResp, err := svc.Send(context.Background(), message.SendRequest{
		ConversationID: 10, SenderID: 42, SenderDeviceID: "ios", ClientMsgID: "edit-ws-1", Type: 1, Payload: []byte("original"),
	})
	if err != nil {
		t.Fatal(err)
	}
	msgID := sendResp.Message.ID

	router := gin.New()
	NewWebSocketHandler().WithAuth(tokens).WithMessageSender(svc).Register(router)
	server := httptest.NewServer(router)
	defer server.Close()
	conn := dialWebSocket(t, server.URL, "/ws?access_token="+token)
	defer conn.Close()

	payload, err := json.Marshal(editMessagePayload{MessageID: msgID, Payload: "dXBkYXRlZA=="})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Packet{Command: "edit_message", TraceID: "edit-1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var pkt Packet
	if err := conn.ReadJSON(&pkt); err != nil {
		t.Fatal(err)
	}
	if pkt.Command != "edit_message_ack" || pkt.TraceID != "edit-1" {
		t.Fatalf("unexpected edit ack packet: %+v", pkt)
	}
	var ack editMessageAckPayload
	if err := json.Unmarshal(pkt.Payload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.MessageID != msgID || ack.Status != message.MessageStatusEdited || ack.Payload != "dXBkYXRlZA==" || ack.EditedAtMs == 0 {
		t.Fatalf("unexpected edit ack payload: %+v", ack)
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

func (errorSender) Recall(context.Context, message.RecallRequest) (message.Message, error) {
	return message.Message{}, nil
}

func (errorSender) Delete(context.Context, message.DeleteRequest) (message.Message, error) {
	return message.Message{}, nil
}

func (errorSender) Edit(context.Context, message.EditRequest) (message.Message, error) {
	return message.Message{}, nil
}
