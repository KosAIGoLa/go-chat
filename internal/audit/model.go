package audit

type Event struct {
	ID, TenantID, ActorUserID        uint64
	Action, ResourceType, ResourceID string
	CreatedAtMs                      int64
}
