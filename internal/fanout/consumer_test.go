package fanout

import (
	"context"
	"testing"

	"github.com/ck-chat/ck-chat/internal/delivery"
	"github.com/ck-chat/ck-chat/internal/inbox"
	"github.com/ck-chat/ck-chat/internal/message"
	"github.com/ck-chat/ck-chat/internal/mq"
	"github.com/ck-chat/ck-chat/internal/route"
)

func TestConsumerHandlesUpstreamEvent(t *testing.T) {
	broker := mq.NewMemoryBroker()
	inboxStore := inbox.NewStore()
	deliverySvc := delivery.NewService(route.NewRegistry())
	fanoutSvc := NewService(StaticMembers{10: {1, 2}}, inboxStore, deliverySvc)
	NewConsumer(fanoutSvc).Register(broker)

	publisher := message.NewPublisher(broker)
	if err := publisher.Publish(context.Background(), message.Message{ID: 1, ConversationID: 10, Seq: 1, SenderID: 1, ClientMsgID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if got := inboxStore.List(2); len(got) != 1 || got[0].MsgID != 1 {
		t.Fatalf("expected inbox from fanout, got %+v", got)
	}
}
