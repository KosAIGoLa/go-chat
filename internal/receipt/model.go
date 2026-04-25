package receipt

type Receipt struct {
	ConversationID, UserID uint64
	DeviceID               string
	DeliveredSeq, ReadSeq  uint64
	UpdatedAtMs            int64
}
