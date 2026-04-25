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

func TestStoreMarkDeliveredBatchUsesMaxSeq(t *testing.T) {
	s := NewStore()

	r := s.MarkDeliveredBatch(1, 2, "ios", []uint64{3, 9, 5}, 100)
	if r.ConversationID != 1 || r.UserID != 2 || r.DeviceID != "ios" {
		t.Fatalf("unexpected receipt identity: %+v", r)
	}
	if r.DeliveredSeq != 9 || r.ReadSeq != 0 || r.UpdatedAtMs != 100 {
		t.Fatalf("unexpected delivered batch receipt: %+v", r)
	}

	stored, ok := s.Get(1, 2, "ios")
	if !ok || stored.DeliveredSeq != 9 || stored.ReadSeq != 0 {
		t.Fatalf("unexpected stored delivered batch receipt: %+v ok=%v", stored, ok)
	}
}

func TestStoreMarkReadBatchUsesMaxSeqAndAdvancesDelivered(t *testing.T) {
	s := NewStore()

	r := s.MarkReadBatch(1, 2, "ios", []uint64{4, 11, 7}, 200)
	if r.DeliveredSeq != 11 || r.ReadSeq != 11 || r.UpdatedAtMs != 200 {
		t.Fatalf("unexpected read batch receipt: %+v", r)
	}

	stored, ok := s.Get(1, 2, "ios")
	if !ok || stored.DeliveredSeq != 11 || stored.ReadSeq != 11 {
		t.Fatalf("unexpected stored read batch receipt: %+v ok=%v", stored, ok)
	}
}

func TestStoreBatchReceiptsRemainMonotonic(t *testing.T) {
	s := NewStore()
	s.MarkDeliveredBatch(1, 2, "ios", []uint64{8, 10}, 100)
	delivered := s.MarkDeliveredBatch(1, 2, "ios", []uint64{3, 9}, 101)
	if delivered.DeliveredSeq != 10 {
		t.Fatalf("expected delivered seq to remain monotonic, got %+v", delivered)
	}

	s.MarkReadBatch(1, 2, "ios", []uint64{12, 14}, 102)
	read := s.MarkReadBatch(1, 2, "ios", []uint64{7, 13}, 103)
	if read.DeliveredSeq != 14 || read.ReadSeq != 14 {
		t.Fatalf("expected read seq to remain monotonic, got %+v", read)
	}
}

func TestStoreBatchReceiptsEmptyIsNoop(t *testing.T) {
	s := NewStore()
	s.MarkRead(1, 2, "ios", 6, 100)

	delivered := s.MarkDeliveredBatch(1, 2, "ios", nil, 101)
	if delivered.DeliveredSeq != 6 || delivered.ReadSeq != 6 || delivered.UpdatedAtMs != 100 {
		t.Fatalf("expected empty delivered batch to be no-op, got %+v", delivered)
	}

	read := s.MarkReadBatch(1, 2, "ios", []uint64{}, 102)
	if read.DeliveredSeq != 6 || read.ReadSeq != 6 || read.UpdatedAtMs != 100 {
		t.Fatalf("expected empty read batch to be no-op, got %+v", read)
	}
}
