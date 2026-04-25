package presence

import (
	"sync"
	"time"
)

type Status uint8

const (
	StatusOffline   Status = 0
	StatusOnline    Status = 1
	StatusBusy      Status = 2
	StatusInvisible Status = 3
)

type Device struct {
	DeviceID  string
	GatewayID string
	SeenAt    time.Time
}

type Snapshot struct {
	UserID  uint64
	Status  Status
	Devices []Device
}

type Store struct {
	mu    sync.RWMutex
	users map[uint64]map[string]Device
}

func NewStore() *Store { return &Store{users: make(map[uint64]map[string]Device)} }

func (s *Store) Online(userID uint64, device Device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users[userID] == nil {
		s.users[userID] = make(map[string]Device)
	}
	if device.SeenAt.IsZero() {
		device.SeenAt = time.Now()
	}
	s.users[userID][device.DeviceID] = device
}

func (s *Store) Offline(userID uint64, deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users[userID], deviceID)
	if len(s.users[userID]) == 0 {
		delete(s.users, userID)
	}
}

func (s *Store) Snapshot(userID uint64) Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	devices := make([]Device, 0, len(s.users[userID]))
	for _, d := range s.users[userID] {
		devices = append(devices, d)
	}
	status := StatusOffline
	if len(devices) > 0 {
		status = StatusOnline
	}
	return Snapshot{UserID: userID, Status: status, Devices: devices}
}
