package route

import "testing"

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register(DeviceRoute{UserID: 1, DeviceID: "ios", GatewayID: "gw-1", ConnID: "c-1"})
	if got := r.Get(1); len(got) != 1 || got[0].GatewayID != "gw-1" {
		t.Fatalf("unexpected routes: %+v", got)
	}
	r.Unregister(1, "ios")
	if got := r.Get(1); len(got) != 0 {
		t.Fatalf("expected no routes, got %+v", got)
	}
}

func TestRegistryCount(t *testing.T) {
	r := NewRegistry()
	if n := r.Count(); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
	r.Register(DeviceRoute{UserID: 1, DeviceID: "ios", LastSeenMs: 1000})
	r.Register(DeviceRoute{UserID: 1, DeviceID: "pc", LastSeenMs: 1000})
	r.Register(DeviceRoute{UserID: 2, DeviceID: "android", LastSeenMs: 1000})
	if n := r.Count(); n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}
}

func TestRegistryUpdateLastSeen(t *testing.T) {
	r := NewRegistry()
	r.Register(DeviceRoute{UserID: 1, DeviceID: "ios", GatewayID: "gw-1", LastSeenMs: 100})

	// Update the timestamp.
	r.UpdateLastSeen(1, "ios", 999)

	got := r.Get(1)
	if len(got) != 1 {
		t.Fatalf("expected 1 route, got %d", len(got))
	}
	if got[0].LastSeenMs != 999 {
		t.Fatalf("expected LastSeenMs=999, got %d", got[0].LastSeenMs)
	}
	// Other fields should be preserved.
	if got[0].GatewayID != "gw-1" {
		t.Fatalf("GatewayID should be preserved, got %q", got[0].GatewayID)
	}
}

func TestRegistryUpdateLastSeenNoopIfMissing(t *testing.T) {
	r := NewRegistry()
	// Should not panic even if the user/device does not exist.
	r.UpdateLastSeen(99, "ghost", 12345)
	if n := r.Count(); n != 0 {
		t.Fatalf("expected 0 routes after noop update, got %d", n)
	}
}

func TestRegistryPruneStaleRemovesOldRoutes(t *testing.T) {
	r := NewRegistry()
	// Register three routes with different timestamps.
	r.Register(DeviceRoute{UserID: 1, DeviceID: "ios", LastSeenMs: 100})
	r.Register(DeviceRoute{UserID: 1, DeviceID: "pc", LastSeenMs: 500})
	r.Register(DeviceRoute{UserID: 2, DeviceID: "android", LastSeenMs: 200})

	// Prune anything last seen before ts=300.
	// Expected: user1/ios (100) and user2/android (200) should be removed;
	//           user1/pc (500) should survive.
	r.PruneStale(300)

	if n := r.Count(); n != 1 {
		t.Fatalf("expected 1 route after pruning, got %d", n)
	}
	remaining := r.Get(1)
	if len(remaining) != 1 || remaining[0].DeviceID != "pc" {
		t.Fatalf("expected only user1/pc to survive, got %+v", remaining)
	}
	if ghost := r.Get(2); len(ghost) != 0 {
		t.Fatalf("expected user2 bucket removed, got %+v", ghost)
	}
}

func TestRegistryPruneStaleRemovesEmptyBuckets(t *testing.T) {
	r := NewRegistry()
	r.Register(DeviceRoute{UserID: 5, DeviceID: "tablet", LastSeenMs: 50})

	// Prune everything.
	r.PruneStale(1000)

	if n := r.Count(); n != 0 {
		t.Fatalf("expected 0 routes, got %d", n)
	}
	// The user bucket itself should be cleaned up (no leak).
	if routes := r.Get(5); len(routes) != 0 {
		t.Fatalf("expected empty slice for removed user, got %+v", routes)
	}
}

func TestRegistryPruneStaleKeepsExactBoundary(t *testing.T) {
	r := NewRegistry()
	// A route with LastSeenMs == olderThanMs should NOT be pruned
	// (PruneStale removes strictly less-than).
	r.Register(DeviceRoute{UserID: 3, DeviceID: "watch", LastSeenMs: 300})

	r.PruneStale(300)

	if n := r.Count(); n != 1 {
		t.Fatalf("expected route at boundary to survive, got count=%d", n)
	}
}

func TestRegistryPruneStaleNoopOnEmpty(t *testing.T) {
	r := NewRegistry()
	// Should not panic on empty registry.
	r.PruneStale(9999999)
	if n := r.Count(); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}
