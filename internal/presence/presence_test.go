package presence

import "testing"

func TestPresenceStore(t *testing.T) {
	s := NewStore()
	if snap := s.Snapshot(1); snap.Status != StatusOffline {
		t.Fatalf("expected offline")
	}
	s.Online(1, Device{DeviceID: "ios", GatewayID: "gw"})
	if snap := s.Snapshot(1); snap.Status != StatusOnline || len(snap.Devices) != 1 {
		t.Fatalf("unexpected online snapshot: %+v", snap)
	}
	s.Offline(1, "ios")
	if snap := s.Snapshot(1); snap.Status != StatusOffline {
		t.Fatalf("expected offline after remove: %+v", snap)
	}
}
