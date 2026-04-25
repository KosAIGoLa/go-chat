package message

import (
	"context"
	"testing"

	"github.com/ck-chat/ck-chat/internal/sequence"
)

func TestServiceSendIsIdempotent(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	req := SendRequest{ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: "c1", Type: 1, Payload: []byte("hello")}
	first, err := svc.Send(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Send(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Message.ID != second.Message.ID || first.Message.Seq != second.Message.Seq {
		t.Fatalf("expected idempotent response, first=%+v second=%+v", first, second)
	}
}

func TestServiceSync(t *testing.T) {
	svc := NewService(sequence.NewAllocator(), NewMemoryStore())
	for _, id := range []string{"a", "b", "c"} {
		_, err := svc.Send(context.Background(), SendRequest{ConversationID: 10, SenderID: 20, SenderDeviceID: "ios", ClientMsgID: id, Type: 1})
		if err != nil {
			t.Fatal(err)
		}
	}
	messages, err := svc.Sync(context.Background(), 10, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Seq != 2 || messages[1].Seq != 3 {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}
