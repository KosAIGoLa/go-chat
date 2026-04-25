package inbox

import "sync"

type Entry struct {
	UserID         uint64
	ConversationID uint64
	Seq            uint64
	MsgID          uint64
	SenderID       uint64
	CreatedAtMs    int64
}

type Store struct {
	mu      sync.RWMutex
	entries map[uint64][]Entry
}

func NewStore() *Store { return &Store{entries: make(map[uint64][]Entry)} }

func (s *Store) Add(entry Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[entry.UserID] = append(s.entries[entry.UserID], entry)
}

func (s *Store) List(userID uint64) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, len(s.entries[userID]))
	copy(out, s.entries[userID])
	return out
}
