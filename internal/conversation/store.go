package conversation

import (
	"sync"

	"github.com/ck-chat/ck-chat/pkg/timeutil"
)

// Store is a thread-safe, in-memory store for per-user conversation settings.
type Store struct {
	mu   sync.RWMutex
	data map[storeKey]UserConversationSettings
}

type storeKey struct {
	userID         uint64
	conversationID uint64
}

// NewStore creates an empty in-memory conversation settings store.
func NewStore() *Store {
	return &Store{data: make(map[storeKey]UserConversationSettings)}
}

// Get returns the settings for the given user+conversation pair.
// If no explicit settings exist, the zero-value UserConversationSettings
// with the supplied IDs populated is returned (found == false).
func (s *Store) Get(userID, conversationID uint64) (UserConversationSettings, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[storeKey{userID, conversationID}]
	return v, ok
}

// List returns all explicitly-stored settings for the given user, ordered by
// insertion order (map iteration, so not deterministic — sufficient for MVP).
func (s *Store) List(userID uint64) []UserConversationSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []UserConversationSettings
	for k, v := range s.data {
		if k.userID == userID {
			out = append(out, v)
		}
	}
	return out
}

// Upsert creates or replaces the conversation settings for a user.
func (s *Store) Upsert(settings UserConversationSettings) UserConversationSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if settings.UpdatedAtMs == 0 {
		settings.UpdatedAtMs = timeutil.UnixMilli()
	}
	s.data[storeKey{settings.UserID, settings.ConversationID}] = settings
	return settings
}
