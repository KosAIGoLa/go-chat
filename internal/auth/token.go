package auth

type Principal struct {
	TenantID uint64
	UserID   uint64
	DeviceID string
	Scopes   []string
}
