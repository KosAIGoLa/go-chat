package conversation

// UserConversationSettings stores per-user, per-conversation product settings
// (mute, pin, DND). The zero value means "no custom settings".
type UserConversationSettings struct {
	UserID         uint64
	ConversationID uint64
	// MuteUntilMs is the Unix millisecond timestamp until which the
	// conversation is muted. 0 means not muted.
	MuteUntilMs int64
	// IsPinned indicates whether the user has pinned this conversation.
	IsPinned    bool
	UpdatedAtMs int64
}
