package route

type DeviceRoute struct {
	UserID     uint64
	DeviceID   string
	GatewayID  string
	ConnID     string
	Platform   string
	Protocol   string
	LastSeenMs int64
}
