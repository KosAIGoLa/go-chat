package inbox

import "testing"

func TestStoreAddListCopies(t *testing.T) {
	s := NewStore()
	s.Add(Entry{UserID: 1, ConversationID: 2, Seq: 3, MsgID: 4})
	entries := s.List(1)
	if len(entries) != 1 || entries[0].Seq != 3 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	entries[0].Seq = 99
	if got := s.List(1)[0].Seq; got != 3 {
		t.Fatalf("store leaked internal slice, got %d", got)
	}
}
