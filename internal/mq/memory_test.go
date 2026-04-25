package mq

import (
	"context"
	"testing"
)

func TestMemoryBrokerPublishSubscribe(t *testing.T) {
	broker := NewMemoryBroker()
	called := false
	broker.Subscribe(TopicUpstream, func(ctx context.Context, msg Message) error {
		called = string(msg.Value) == "hello"
		return nil
	})
	if err := broker.Publish(context.Background(), Message{Topic: TopicUpstream, Key: "1", Value: []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler not called")
	}
	if len(broker.Messages()) != 1 {
		t.Fatalf("expected one stored message")
	}
}
