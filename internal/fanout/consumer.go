package fanout

import (
	"context"

	"github.com/ck-chat/ck-chat/internal/message"
	"github.com/ck-chat/ck-chat/internal/mq"
)

type Consumer struct{ service *Service }

func NewConsumer(service *Service) *Consumer { return &Consumer{service: service} }

func (c *Consumer) Register(broker *mq.MemoryBroker) {
	broker.Subscribe(mq.TopicUpstream, c.Handle)
}

func (c *Consumer) Handle(ctx context.Context, msg mq.Message) error {
	event, err := message.UnmarshalEvent(msg.Value)
	if err != nil {
		return err
	}
	c.service.Fanout(ctx, event.ToMessage())
	return nil
}
