package message

import "encoding/json"

type PublishedEvent struct {
	EventID        string `json:"event_id"`
	TraceID        string `json:"trace_id"`
	MsgID          uint64 `json:"msg_id"`
	ConversationID uint64 `json:"conversation_id"`
	Seq            uint64 `json:"seq"`
	SenderID       uint64 `json:"sender_id"`
	SenderDeviceID string `json:"sender_device_id"`
	ClientMsgID    string `json:"client_msg_id"`
	MsgType        int32  `json:"msg_type"`
	Payload        []byte `json:"payload"`
	CreatedAtMs    int64  `json:"created_at_ms"`
}

func EventFromMessage(msg Message) PublishedEvent {
	return PublishedEvent{EventID: msg.ClientMsgID, MsgID: msg.ID, ConversationID: msg.ConversationID, Seq: msg.Seq, SenderID: msg.SenderID, SenderDeviceID: msg.SenderDeviceID, ClientMsgID: msg.ClientMsgID, MsgType: msg.Type, Payload: msg.Payload, CreatedAtMs: msg.CreatedAtMs}
}

func (e PublishedEvent) ToMessage() Message {
	return Message{ID: e.MsgID, ConversationID: e.ConversationID, Seq: e.Seq, SenderID: e.SenderID, SenderDeviceID: e.SenderDeviceID, ClientMsgID: e.ClientMsgID, Type: e.MsgType, Payload: e.Payload, CreatedAtMs: e.CreatedAtMs}
}

func MarshalEvent(event PublishedEvent) ([]byte, error) { return json.Marshal(event) }
func UnmarshalEvent(data []byte) (PublishedEvent, error) {
	var event PublishedEvent
	err := json.Unmarshal(data, &event)
	return event, err
}
