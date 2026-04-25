package audit

import (
	"context"
	"testing"

	"github.com/ck-chat/ck-chat/internal/mq"
)

func TestConsumerStoresAuditEvent(t *testing.T) {
	broker := mq.NewMemoryBroker()
	store := NewStore()
	NewConsumer(store).Register(broker)
	payload, err := MarshalEvent(Event{ID: 1, TenantID: 10, ActorUserID: 20, Action: "message.send", ResourceType: "message", ResourceID: "99"})
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Publish(context.Background(), mq.Message{Topic: mq.TopicAudit, Key: "1", Value: payload}); err != nil {
		t.Fatal(err)
	}
	if got := store.ListByTenant(10); len(got) != 1 || got[0].Action != "message.send" {
		t.Fatalf("unexpected audit events: %+v", got)
	}
}
