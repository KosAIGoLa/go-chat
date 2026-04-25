package group

type Group struct {
	ID, OwnerID, ConversationID uint64
	Name                        string
	MemberCount                 uint32
}
type Member struct {
	GroupID, UserID uint64
	Role            uint8
	LastReadSeq     uint64
}
