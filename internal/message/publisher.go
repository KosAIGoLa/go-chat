package message

import (
	"context"
	"strconv"

	"github.com/ck-chat/ck-chat/internal/mq"
)

type Publisher struct{ broker *mq.MemoryBroker }

func NewPublisher(broker *mq.MemoryBroker) *Publisher { return &Publisher{broker: broker} }

func (p *Publisher) Publish(ctx context.Context, msg Message) error {
	payload, err := MarshalEvent(EventFromMessage(msg))
	if err != nil {
		return err
	}
	return p.broker.Publish(ctx, mq.Message{Topic: mq.TopicUpstream, Key: strconv.FormatUint(msg.ConversationID, 10), Value: payload})
}
