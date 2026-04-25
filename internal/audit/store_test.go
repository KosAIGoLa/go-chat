package audit

import "testing"

func TestStoreListByTenant(t *testing.T) {
	s := NewStore()
	s.Append(Event{ID: 1, TenantID: 10, Action: "message.send"})
	s.Append(Event{ID: 2, TenantID: 20, Action: "message.send"})
	got := s.ListByTenant(10)
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("unexpected events: %+v", got)
	}
}
