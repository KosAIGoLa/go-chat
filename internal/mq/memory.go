package mq

import (
	"context"
	"sync"
)

type Message struct {
	Topic string
	Key   string
	Value []byte
}

type Handler func(context.Context, Message) error

type MemoryBroker struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
	messages []Message
}

func NewMemoryBroker() *MemoryBroker { return &MemoryBroker{handlers: make(map[string][]Handler)} }

func (b *MemoryBroker) Publish(ctx context.Context, msg Message) error {
	b.mu.Lock()
	b.messages = append(b.messages, msg)
	handlers := append([]Handler(nil), b.handlers[msg.Topic]...)
	b.mu.Unlock()
	for _, handler := range handlers {
		if err := handler(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

func (b *MemoryBroker) Subscribe(topic string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = append(b.handlers[topic], handler)
}

func (b *MemoryBroker) Messages() []Message {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Message, len(b.messages))
	copy(out, b.messages)
	return out
}
