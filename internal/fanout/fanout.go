package fanout

import (
	"context"

	"github.com/ck-chat/ck-chat/internal/delivery"
	"github.com/ck-chat/ck-chat/internal/inbox"
	"github.com/ck-chat/ck-chat/internal/message"
)

type ConversationMembers interface {
	Members(conversationID uint64) []uint64
}

type StaticMembers map[uint64][]uint64

func (s StaticMembers) Members(conversationID uint64) []uint64 {
	return append([]uint64(nil), s[conversationID]...)
}

type Service struct {
	members  ConversationMembers
	inbox    *inbox.Store
	delivery *delivery.Service
}

func NewService(members ConversationMembers, inboxStore *inbox.Store, deliveryService *delivery.Service) *Service {
	return &Service{members: members, inbox: inboxStore, delivery: deliveryService}
}

func (s *Service) Fanout(ctx context.Context, msg message.Message) []delivery.Result {
	targets := s.members.Members(msg.ConversationID)
	results := make([]delivery.Result, 0, len(targets))
	for _, userID := range targets {
		if userID == msg.SenderID {
			continue
		}
		s.inbox.Add(inbox.Entry{UserID: userID, ConversationID: msg.ConversationID, Seq: msg.Seq, MsgID: msg.ID, SenderID: msg.SenderID, CreatedAtMs: msg.CreatedAtMs})
		results = append(results, s.delivery.Deliver(ctx, delivery.Task{TargetUserID: userID, Message: msg}))
	}
	return results
}
