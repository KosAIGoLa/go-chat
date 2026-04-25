package message

import (
	"context"
	"sort"
	"sync"
)

type MemoryStore struct {
	mu       sync.RWMutex
	messages map[uint64][]Message
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{messages: make(map[uint64][]Message)} }

func (s *MemoryStore) Save(_ context.Context, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[msg.ConversationID] = append(s.messages[msg.ConversationID], msg)
	sort.Slice(s.messages[msg.ConversationID], func(i, j int) bool {
		return s.messages[msg.ConversationID][i].Seq < s.messages[msg.ConversationID][j].Seq
	})
	return nil
}

func (s *MemoryStore) ListAfter(_ context.Context, conversationID, fromSeq uint64, limit int) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Message
	for _, msg := range s.messages[conversationID] {
		if msg.Seq >= fromSeq {
			out = append(out, msg)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
