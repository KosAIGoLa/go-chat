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
