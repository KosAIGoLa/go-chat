package sequence

import "sync"

type Allocator struct {
	mu  sync.Mutex
	seq map[uint64]uint64
}

func NewAllocator() *Allocator { return &Allocator{seq: make(map[uint64]uint64)} }

func (a *Allocator) Next(conversationID uint64) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq[conversationID]++
	return a.seq[conversationID]
}
