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

// UpdateLastSeen refreshes the LastSeenMs timestamp for a specific device route
// without requiring a full re-registration. If the route does not exist, it is
// a no-op. This is more efficient than calling Register for heartbeat updates
// when the route metadata (GatewayID, ConnID, etc.) has not changed.
func (r *Registry) UpdateLastSeen(userID uint64, deviceID string, nowMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if devMap, ok := r.byUser[userID]; ok {
		if entry, ok := devMap[deviceID]; ok {
			entry.LastSeenMs = nowMs
			devMap[deviceID] = entry
		}
	}
}

// PruneStale removes all device routes whose LastSeenMs is strictly less than
// olderThanMs. This is used to clean up stale routes left behind after a
// gateway node crash where the normal Unregister path was never executed.
// Empty user buckets are also removed to avoid memory leaks.
func (r *Registry) PruneStale(olderThanMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for userID, devMap := range r.byUser {
		for deviceID, entry := range devMap {
			if entry.LastSeenMs < olderThanMs {
				delete(devMap, deviceID)
			}
		}
		if len(devMap) == 0 {
			delete(r.byUser, userID)
		}
	}
}

// Count returns the total number of registered device routes across all users.
// Useful for metrics and health checks.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	total := 0
	for _, devMap := range r.byUser {
		total += len(devMap)
	}
	return total
}
