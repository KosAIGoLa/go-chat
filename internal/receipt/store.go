package receipt

import "sync"

type Store struct {
	mu       sync.RWMutex
	receipts map[key]Receipt
}
type key struct {
	conversationID uint64
	userID         uint64
	deviceID       string
}

func NewStore() *Store { return &Store{receipts: make(map[key]Receipt)} }

func (s *Store) MarkDelivered(conversationID, userID uint64, deviceID string, seq uint64, updatedAtMs int64) Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{conversationID: conversationID, userID: userID, deviceID: deviceID}
	r := s.receipts[k]
	r.ConversationID, r.UserID, r.DeviceID = conversationID, userID, deviceID
	if seq > r.DeliveredSeq {
		r.DeliveredSeq = seq
	}
	r.UpdatedAtMs = updatedAtMs
	s.receipts[k] = r
	return r
}

func (s *Store) MarkRead(conversationID, userID uint64, deviceID string, seq uint64, updatedAtMs int64) Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{conversationID: conversationID, userID: userID, deviceID: deviceID}
	r := s.receipts[k]
	r.ConversationID, r.UserID, r.DeviceID = conversationID, userID, deviceID
	if seq > r.ReadSeq {
		r.ReadSeq = seq
	}
	if seq > r.DeliveredSeq {
		r.DeliveredSeq = seq
	}
	r.UpdatedAtMs = updatedAtMs
	s.receipts[k] = r
	return r
}

func (s *Store) Get(conversationID, userID uint64, deviceID string) (Receipt, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.receipts[key{conversationID: conversationID, userID: userID, deviceID: deviceID}]
	return r, ok
}

// MarkDeliveredBatch records the maximum seq from seqs as the new delivered
// cursor for the given device. Seqs that are lower than the current cursor are
// ignored (monotonic). If seqs is empty the call is a no-op and the current
// receipt is returned unchanged.
func (s *Store) MarkDeliveredBatch(conversationID, userID uint64, deviceID string, seqs []uint64, updatedAtMs int64) Receipt {
	if len(seqs) == 0 {
		r, _ := s.Get(conversationID, userID, deviceID)
		return r
	}
	var maxSeq uint64
	for _, seq := range seqs {
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	return s.MarkDelivered(conversationID, userID, deviceID, maxSeq, updatedAtMs)
}

// MarkReadBatch records the maximum seq from seqs as the new read cursor for
// the given device (also advances delivered if needed).
func (s *Store) MarkReadBatch(conversationID, userID uint64, deviceID string, seqs []uint64, updatedAtMs int64) Receipt {
	if len(seqs) == 0 {
		r, _ := s.Get(conversationID, userID, deviceID)
		return r
	}
	var maxSeq uint64
	for _, seq := range seqs {
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	return s.MarkRead(conversationID, userID, deviceID, maxSeq, updatedAtMs)
}
