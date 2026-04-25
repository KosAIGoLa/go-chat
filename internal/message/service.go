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

type RecallRequest struct {
	MessageID uint64
	SenderID  uint64
}

type Store interface {
	Save(context.Context, Message) error
	Get(ctx context.Context, msgID uint64) (Message, error)
	ListAfter(ctx context.Context, conversationID, fromSeq uint64, limit int) ([]Message, error)
	Recall(ctx context.Context, msgID uint64, recalledAtMs int64) error
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

func (s *Service) Recall(ctx context.Context, req RecallRequest) (Message, error) {
	if req.MessageID == 0 || req.SenderID == 0 {
		return Message{}, apperrors.AppError{Code: apperrors.SysBadRequest, Message: "msg_id and sender_id are required", Retryable: false}
	}
	msg, err := s.store.Get(ctx, req.MessageID)
	if err != nil {
		return Message{}, err
	}
	if msg.SenderID != req.SenderID {
		return Message{}, apperrors.AppError{Code: apperrors.MsgRecallNotAllowed, Message: "message recall not allowed", Retryable: false}
	}
	if msg.Status == MessageStatusRecalled {
		return msg, nil
	}
	recalledAtMs := timeutil.UnixMilli()
	if err := s.store.Recall(ctx, req.MessageID, recalledAtMs); err != nil {
		return Message{}, err
	}
	msg.Status = MessageStatusRecalled
	msg.RecalledAtMs = recalledAtMs
	return msg, nil
}
