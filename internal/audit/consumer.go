package audit

import (
	"context"
	"encoding/json"

	"github.com/ck-chat/ck-chat/internal/mq"
)

type Consumer struct{ store *Store }

func NewConsumer(store *Store) *Consumer { return &Consumer{store: store} }

func (c *Consumer) Register(broker *mq.MemoryBroker) {
	broker.Subscribe(mq.TopicAudit, c.Handle)
}

func (c *Consumer) Handle(_ context.Context, msg mq.Message) error {
	var event Event
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return err
	}
	c.store.Append(event)
	return nil
}

func MarshalEvent(event Event) ([]byte, error) { return json.Marshal(event) }
