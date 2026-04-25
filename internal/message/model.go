package message

type ConversationType uint8

const (
	ConversationTypeSingle ConversationType = 1
	ConversationTypeGroup  ConversationType = 2
)

type MessageStatus uint8

const (
	MessageStatusNormal   MessageStatus = 0
	MessageStatusRecalled MessageStatus = 1
	MessageStatusDeleted  MessageStatus = 2
	MessageStatusEdited   MessageStatus = 3
)

type Message struct {
	ID             uint64
	ConversationID uint64
	Seq            uint64
	SenderID       uint64
	SenderDeviceID string
	ClientMsgID    string
	Type           int32
	Payload        []byte
	CreatedAtMs    int64
	Status         MessageStatus
	RecalledAtMs   int64
	EditedAtMs     int64
}
