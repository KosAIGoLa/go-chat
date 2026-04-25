package audit

import (
	"context"
	"testing"

	"github.com/ck-chat/ck-chat/internal/mq"
)

func TestPublisherPublishesAuditTopic(t *testing.T) {
	broker := mq.NewMemoryBroker()
	publisher := NewPublisher(broker)
	if err := publisher.Publish(context.Background(), Event{ID: 7, TenantID: 1, Action: "admin.login"}); err != nil {
		t.Fatal(err)
	}
	messages := broker.Messages()
	if len(messages) != 1 || messages[0].Topic != mq.TopicAudit || messages[0].Key != "7" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}
