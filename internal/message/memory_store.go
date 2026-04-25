package message

import (
	"context"
	"sort"
	"sync"

	apperrors "github.com/ck-chat/ck-chat/internal/errors"
)

type MemoryStore struct {
	mu       sync.RWMutex
	messages map[uint64][]*Message
	byID     map[uint64]*Message
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		messages: make(map[uint64][]*Message),
		byID:     make(map[uint64]*Message),
	}
}

func (s *MemoryStore) Save(_ context.Context, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m := &Message{
		ID:             msg.ID,
		ConversationID: msg.ConversationID,
		Seq:            msg.Seq,
		SenderID:       msg.SenderID,
		SenderDeviceID: msg.SenderDeviceID,
		ClientMsgID:    msg.ClientMsgID,
		Type:           msg.Type,
		Payload:        msg.Payload,
		CreatedAtMs:    msg.CreatedAtMs,
		Status:         msg.Status,
		RecalledAtMs:   msg.RecalledAtMs,
	}

	s.messages[msg.ConversationID] = append(s.messages[msg.ConversationID], m)
	sort.Slice(s.messages[msg.ConversationID], func(i, j int) bool {
		return s.messages[msg.ConversationID][i].Seq < s.messages[msg.ConversationID][j].Seq
	})
	s.byID[msg.ID] = m

	return nil
}

func (s *MemoryStore) Get(_ context.Context, msgID uint64) (Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	m, ok := s.byID[msgID]
	if !ok {
		return Message{}, apperrors.AppError{Code: apperrors.MsgNotFound, Message: "message not found"}
	}
	return *m, nil
}

func (s *MemoryStore) ListAfter(_ context.Context, conversationID, fromSeq uint64, limit int) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []Message
	for _, m := range s.messages[conversationID] {
		if m.Seq >= fromSeq {
			out = append(out, *m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *MemoryStore) Recall(_ context.Context, msgID uint64, recalledAtMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.byID[msgID]
	if !ok {
		return apperrors.AppError{Code: apperrors.MsgNotFound, Message: "message not found"}
	}
	m.Status = MessageStatusRecalled
	m.RecalledAtMs = recalledAtMs
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, msgID uint64, deletedAtMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.byID[msgID]
	if !ok {
		return apperrors.AppError{Code: apperrors.MsgNotFound, Message: "message not found"}
	}
	m.Status = MessageStatusDeleted
	m.RecalledAtMs = deletedAtMs
	return nil
}
