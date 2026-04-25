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

type RouteDelivery struct {
	Route     route.DeviceRoute
	Delivered bool
}

type Result struct {
	TargetUserID uint64
	OnlineRoutes []route.DeviceRoute
	RouteResults []RouteDelivery
	Offline      bool
}

type Pusher interface {
	PushToDevice(userID uint64, deviceID string, msg message.Message) bool
}

type Service struct {
	routes *route.Registry
	pusher Pusher
	mu     sync.Mutex
	sent   []Result
}

func NewService(routes *route.Registry) *Service { return &Service{routes: routes} }

func (s *Service) WithPusher(pusher Pusher) *Service {
	s.pusher = pusher
	return s
}

func (s *Service) Deliver(_ context.Context, task Task) Result {
	routes := s.routes.Get(task.TargetUserID)
	result := Result{TargetUserID: task.TargetUserID, OnlineRoutes: routes, Offline: len(routes) == 0}
	for _, onlineRoute := range routes {
		delivered := false
		if s.pusher != nil {
			delivered = s.pusher.PushToDevice(task.TargetUserID, onlineRoute.DeviceID, task.Message)
		}
		result.RouteResults = append(result.RouteResults, RouteDelivery{Route: onlineRoute, Delivered: delivered})
	}
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
