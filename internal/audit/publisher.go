package audit

import (
	"context"
	"strconv"

	"github.com/ck-chat/ck-chat/internal/mq"
)

type Publisher struct{ broker *mq.MemoryBroker }

func NewPublisher(broker *mq.MemoryBroker) *Publisher { return &Publisher{broker: broker} }

func (p *Publisher) Publish(ctx context.Context, event Event) error {
	payload, err := MarshalEvent(event)
	if err != nil {
		return err
	}
	return p.broker.Publish(ctx, mq.Message{Topic: mq.TopicAudit, Key: strconv.FormatUint(event.ID, 10), Value: payload})
}
