package push

import (
	"github.com/ck-chat/ck-chat/internal/message"
)

type OfflinePusher struct {
	store *Store
}

func NewOfflinePusher(store *Store) *OfflinePusher {
	return &OfflinePusher{store: store}
}

func (p *OfflinePusher) PushToDevice(_ uint64, _ string, _ message.Message) bool {
	return false
}

func (p *OfflinePusher) PushOffline(userID uint64, msg message.Message) {
	if p == nil || p.store == nil {
		return
	}
	p.store.Add(Task{
		TargetUserID:   userID,
		ConversationID: msg.ConversationID,
		MsgID:          msg.ID,
		Seq:            msg.Seq,
		SenderID:       msg.SenderID,
		CreatedAtMs:    msg.CreatedAtMs,
	})
}
