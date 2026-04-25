package message

import (
	"context"
	"testing"

	"github.com/ck-chat/ck-chat/internal/mq"
	"github.com/ck-chat/ck-chat/internal/sequence"
)

func TestServicePublishesUpstreamEvent(t *testing.T) {
	broker := mq.NewMemoryBroker()
	svc := NewService(sequence.NewAllocator(), NewMemoryStore()).WithPublisher(NewPublisher(broker))
	if _, err := svc.Send(context.Background(), SendRequest{ConversationID: 10, SenderID: 1, SenderDeviceID: "ios", ClientMsgID: "c1", Type: 1}); err != nil {
		t.Fatal(err)
	}
	messages := broker.Messages()
	if len(messages) != 1 || messages[0].Topic != mq.TopicUpstream || messages[0].Key != "10" {
		t.Fatalf("unexpected mq messages: %+v", messages)
	}
	event, err := UnmarshalEvent(messages[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	if event.ConversationID != 10 || event.Seq != 1 {
		t.Fatalf("unexpected event: %+v", event)
	}
}
