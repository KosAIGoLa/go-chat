package delivery

import (
	"context"
	"sync"

	"github.com/ck-chat/ck-chat/internal/message"
	"github.com/ck-chat/ck-chat/internal/route"
)

type Task struct {
	TargetUserID uint64
	Message      message.Message
}

type Result struct {
	TargetUserID uint64
	OnlineRoutes []route.DeviceRoute
	Offline      bool
}

type Service struct {
	routes *route.Registry
	mu     sync.Mutex
	sent   []Result
}

func NewService(routes *route.Registry) *Service { return &Service{routes: routes} }

func (s *Service) Deliver(_ context.Context, task Task) Result {
	routes := s.routes.Get(task.TargetUserID)
	result := Result{TargetUserID: task.TargetUserID, OnlineRoutes: routes, Offline: len(routes) == 0}
	s.mu.Lock()
	s.sent = append(s.sent, result)
	s.mu.Unlock()
	return result
}

func (s *Service) Results() []Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Result, len(s.sent))
	copy(out, s.sent)
	return out
}
