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
