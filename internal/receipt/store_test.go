package receipt

import "testing"

func TestStoreMonotonicReceipt(t *testing.T) {
	s := NewStore()
	s.MarkDelivered(1, 2, "ios", 10, 100)
	s.MarkDelivered(1, 2, "ios", 8, 101)
	r, ok := s.Get(1, 2, "ios")
	if !ok || r.DeliveredSeq != 10 {
		t.Fatalf("unexpected delivered receipt: %+v", r)
	}
	s.MarkRead(1, 2, "ios", 12, 102)
	r, _ = s.Get(1, 2, "ios")
	if r.ReadSeq != 12 || r.DeliveredSeq != 12 {
		t.Fatalf("unexpected read receipt: %+v", r)
	}
}
