package protocol

type Command uint32

const (
	CommandAuth              Command = 1
	CommandHeartbeat         Command = 2
	CommandSendMessage       Command = 100
	CommandSendMessageAck    Command = 101
	CommandPushMessage       Command = 102
	CommandDeliveryAck       Command = 103
	CommandReadAck           Command = 104
	CommandSyncConversation  Command = 200
	CommandSyncMessage       Command = 201
	CommandPresenceSubscribe Command = 300
	CommandPresenceEvent     Command = 301
)
