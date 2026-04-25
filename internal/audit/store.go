package audit

import "sync"

type Store struct {
	mu     sync.RWMutex
	events []Event
}

func NewStore() *Store { return &Store{} }

func (s *Store) Append(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *Store) ListByTenant(tenantID uint64) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, 0)
	for _, event := range s.events {
		if event.TenantID == tenantID {
			out = append(out, event)
		}
	}
	return out
}

func (s *Store) All() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}
