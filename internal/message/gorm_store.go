package message

import (
	"context"
	"errors"

	apperrors "github.com/ck-chat/ck-chat/internal/errors"
	"gorm.io/gorm"
)

type GORMMessage struct {
	ID             uint64 `gorm:"primaryKey;column:id"`
	ConversationID uint64 `gorm:"index:idx_conversation_seq,priority:1;not null"`
	Seq            uint64 `gorm:"index:idx_conversation_seq,priority:2;not null"`
	SenderID       uint64 `gorm:"index;not null"`
	SenderDeviceID string `gorm:"size:128;not null"`
	ClientMsgID    string `gorm:"size:128;not null;index:idx_sender_client_msg,priority:3"`
	Type           int32  `gorm:"not null"`
	Payload        []byte `gorm:"type:blob"`
	CreatedAtMs    int64  `gorm:"index;not null"`
	Status         uint8  `gorm:"not null;default:0"`
	RecalledAtMs   int64  `gorm:"not null;default:0"`
}

func (GORMMessage) TableName() string { return "messages" }

type GORMStore struct {
	db *gorm.DB
}

func NewGORMStore(db *gorm.DB) *GORMStore { return &GORMStore{db: db} }

func (s *GORMStore) AutoMigrate(ctx context.Context) error {
	return s.db.WithContext(ctx).AutoMigrate(&GORMMessage{})
}

func (s *GORMStore) Save(ctx context.Context, msg Message) error {
	return s.db.WithContext(ctx).Create(&GORMMessage{
		ID:             msg.ID,
		ConversationID: msg.ConversationID,
		Seq:            msg.Seq,
		SenderID:       msg.SenderID,
		SenderDeviceID: msg.SenderDeviceID,
		ClientMsgID:    msg.ClientMsgID,
		Type:           msg.Type,
		Payload:        append([]byte(nil), msg.Payload...),
		CreatedAtMs:    msg.CreatedAtMs,
		Status:         uint8(msg.Status),
		RecalledAtMs:   msg.RecalledAtMs,
	}).Error
}

func (s *GORMStore) Get(ctx context.Context, msgID uint64) (Message, error) {
	var row GORMMessage
	if err := s.db.WithContext(ctx).First(&row, msgID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Message{}, apperrors.AppError{Code: apperrors.MsgNotFound}
		}
		return Message{}, err
	}
	return Message{
		ID:             row.ID,
		ConversationID: row.ConversationID,
		Seq:            row.Seq,
		SenderID:       row.SenderID,
		SenderDeviceID: row.SenderDeviceID,
		ClientMsgID:    row.ClientMsgID,
		Type:           row.Type,
		Payload:        append([]byte(nil), row.Payload...),
		CreatedAtMs:    row.CreatedAtMs,
		Status:         MessageStatus(row.Status),
		RecalledAtMs:   row.RecalledAtMs,
	}, nil
}

func (s *GORMStore) Recall(ctx context.Context, msgID uint64, recalledAtMs int64) error {
	result := s.db.WithContext(ctx).
		Model(&GORMMessage{}).
		Where("id = ?", msgID).
		Updates(map[string]interface{}{
			"status":         uint8(MessageStatusRecalled),
			"recalled_at_ms": recalledAtMs,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.AppError{Code: apperrors.MsgNotFound}
	}
	return nil
}

func (s *GORMStore) Delete(ctx context.Context, msgID uint64, deletedAtMs int64) error {
	result := s.db.WithContext(ctx).
		Model(&GORMMessage{}).
		Where("id = ?", msgID).
		Updates(map[string]interface{}{
			"status":         uint8(MessageStatusDeleted),
			"recalled_at_ms": deletedAtMs,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.AppError{Code: apperrors.MsgNotFound}
	}
	return nil
}

func (s *GORMStore) ListAfter(ctx context.Context, conversationID, fromSeq uint64, limit int) ([]Message, error) {
	var rows []GORMMessage
	if err := s.db.WithContext(ctx).
		Where("conversation_id = ? AND seq >= ?", conversationID, fromSeq).
		Order("seq ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(rows))
	for _, row := range rows {
		out = append(out, Message{
			ID:             row.ID,
			ConversationID: row.ConversationID,
			Seq:            row.Seq,
			SenderID:       row.SenderID,
			SenderDeviceID: row.SenderDeviceID,
			ClientMsgID:    row.ClientMsgID,
			Type:           row.Type,
			Payload:        append([]byte(nil), row.Payload...),
			CreatedAtMs:    row.CreatedAtMs,
			Status:         MessageStatus(row.Status),
			RecalledAtMs:   row.RecalledAtMs,
		})
	}
	return out, nil
}
