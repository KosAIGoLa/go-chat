package push

import (
	"testing"

	"github.com/ck-chat/ck-chat/internal/message"
)

func TestOfflinePusherStoresOfflineTask(t *testing.T) {
	store := NewStore()
	pusher := NewOfflinePusher(store)

	pusher.PushOffline(7, message.Message{
		ID:             11,
		ConversationID: 10,
		Seq:            9,
		SenderID:       2,
		CreatedAtMs:    1234,
	})

	tasks := store.List(7)
	if len(tasks) != 1 {
		t.Fatalf("expected one offline task, got %+v", tasks)
	}
	if tasks[0].TargetUserID != 7 || tasks[0].MsgID != 11 || tasks[0].ConversationID != 10 || tasks[0].Seq != 9 || tasks[0].SenderID != 2 || tasks[0].CreatedAtMs != 1234 {
		t.Fatalf("unexpected stored task: %+v", tasks[0])
	}
}
