package codec

type Packet struct {
	Version   uint32
	Command   uint32
	RequestID string
	TraceID   string
	Payload   []byte
}
