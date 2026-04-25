package errors

type Code string

const (
	SysInternal      Code = "SYS_INTERNAL"
	SysUnavailable   Code = "SYS_UNAVAILABLE"
	SysTimeout       Code = "SYS_TIMEOUT"
	SysBadRequest    Code = "SYS_BAD_REQUEST"
	AuthTokenInvalid Code = "AUTH_TOKEN_INVALID"
	PermDenied       Code = "PERM_DENIED"
	MsgDuplicated    Code = "MSG_DUPLICATED"
	MsgRateLimited   Code = "MSG_RATE_LIMITED"
	MsgQueueFailed   Code = "MSG_QUEUE_FAILED"
	RouteNotFound    Code = "ROUTE_NOT_FOUND"
	DeliverTimeout   Code = "DELIVER_TIMEOUT"
)

type AppError struct {
	Code      Code
	Message   string
	Retryable bool
}

func (e AppError) Error() string { return string(e.Code) + ": " + e.Message }
