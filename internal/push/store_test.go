package push

import "testing"

func TestStoreAddListCopies(t *testing.T) {
	s := NewStore()
	s.Add(Task{TargetUserID: 1, ConversationID: 2, MsgID: 3, Seq: 4, SenderID: 5})
	tasks := s.List(1)
	if len(tasks) != 1 || tasks[0].Seq != 4 {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	tasks[0].Seq = 99
	if got := s.List(1)[0].Seq; got != 4 {
		t.Fatalf("store leaked internal slice, got %d", got)
	}
}
