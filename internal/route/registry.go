package route

import "sync"

type Registry struct {
	mu     sync.RWMutex
	byUser map[uint64]map[string]DeviceRoute
}

func NewRegistry() *Registry { return &Registry{byUser: make(map[uint64]map[string]DeviceRoute)} }

func (r *Registry) Register(route DeviceRoute) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byUser[route.UserID] == nil {
		r.byUser[route.UserID] = make(map[string]DeviceRoute)
	}
	r.byUser[route.UserID][route.DeviceID] = route
}

func (r *Registry) Unregister(userID uint64, deviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byUser[userID], deviceID)
	if len(r.byUser[userID]) == 0 {
		delete(r.byUser, userID)
	}
}

func (r *Registry) Get(userID uint64) []DeviceRoute {
	r.mu.RLock()
	defer r.mu.RUnlock()
	routes := make([]DeviceRoute, 0, len(r.byUser[userID]))
	for _, route := range r.byUser[userID] {
		routes = append(routes, route)
	}
	return routes
}
