package message

import (
	"context"
	"fmt"
	"sync"

	apperrors "github.com/ck-chat/ck-chat/internal/errors"
	"github.com/ck-chat/ck-chat/internal/sequence"
	"github.com/ck-chat/ck-chat/pkg/idgen"
	"github.com/ck-chat/ck-chat/pkg/timeutil"
)

type SendRequest struct {
	ConversationID uint64
	SenderID       uint64
	SenderDeviceID string
	ClientMsgID    string
	Type           int32
	Payload        []byte
}

type SendResponse struct {
	Message Message
	Stage   AckStage
}

type AckStage uint8

const (
	AckStageQueued AckStage = 2
)

type Store interface {
	Save(context.Context, Message) error
	ListAfter(ctx context.Context, conversationID, fromSeq uint64, limit int) ([]Message, error)
}

type EventPublisher interface {
	Publish(context.Context, Message) error
}

type Service struct {
	seq       *sequence.Allocator
	store     Store
	publisher EventPublisher
	mu        sync.Mutex
	idem      map[string]Message
}

func NewService(seq *sequence.Allocator, store Store) *Service {
	return &Service{seq: seq, store: store, idem: make(map[string]Message)}
}

func (s *Service) WithPublisher(publisher EventPublisher) *Service {
	s.publisher = publisher
	return s
}

func (s *Service) Send(ctx context.Context, req SendRequest) (SendResponse, error) {
	if req.ConversationID == 0 || req.SenderID == 0 || req.ClientMsgID == "" {
		return SendResponse{}, apperrors.AppError{Code: apperrors.SysBadRequest, Message: "conversation_id, sender_id and client_msg_id are required", Retryable: false}
	}
	key := fmt.Sprintf("%d:%s:%s", req.SenderID, req.SenderDeviceID, req.ClientMsgID)

	s.mu.Lock()
	if existing, ok := s.idem[key]; ok {
		s.mu.Unlock()
		return SendResponse{Message: existing, Stage: AckStageQueued}, nil
	}
	s.mu.Unlock()

	msg := Message{
		ID:             idgen.New(),
		ConversationID: req.ConversationID,
		Seq:            s.seq.Next(req.ConversationID),
		SenderID:       req.SenderID,
		SenderDeviceID: req.SenderDeviceID,
		ClientMsgID:    req.ClientMsgID,
		Type:           req.Type,
		Payload:        append([]byte(nil), req.Payload...),
		CreatedAtMs:    timeutil.UnixMilli(),
	}
	if err := s.store.Save(ctx, msg); err != nil {
		return SendResponse{}, err
	}
	if s.publisher != nil {
		if err := s.publisher.Publish(ctx, msg); err != nil {
			return SendResponse{}, err
		}
	}

	s.mu.Lock()
	s.idem[key] = msg
	s.mu.Unlock()
	return SendResponse{Message: msg, Stage: AckStageQueued}, nil
}

func (s *Service) Sync(ctx context.Context, conversationID, fromSeq uint64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.store.ListAfter(ctx, conversationID, fromSeq, limit)
}
