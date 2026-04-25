package push

import "sync"

type Task struct {
	TargetUserID   uint64
	ConversationID uint64
	MsgID          uint64
	Seq            uint64
	SenderID       uint64
	CreatedAtMs    int64
}

type Store struct {
	mu    sync.RWMutex
	tasks map[uint64][]Task
}

func NewStore() *Store {
	return &Store{tasks: make(map[uint64][]Task)}
}

func (s *Store) Add(task Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.TargetUserID] = append(s.tasks[task.TargetUserID], task)
}

func (s *Store) List(userID uint64) []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, len(s.tasks[userID]))
	copy(out, s.tasks[userID])
	return out
}
