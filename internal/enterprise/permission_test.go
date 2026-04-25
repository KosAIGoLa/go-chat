package enterprise

import "testing"

func TestPermissionService(t *testing.T) {
	dir := NewDirectory()
	dir.UpsertTenant(Tenant{ID: 1, Name: "Acme", Code: "acme", Status: 1})
	dir.UpsertMember(Member{TenantID: 1, UserID: 2, DisplayName: "Alice", Status: MemberActive})
	perms := NewPermissionService(dir)
	if got := perms.Check(CheckRequest{TenantID: 1, UserID: 2, Permission: PermissionMessageSend}); got.Allowed || got.Reason != "permission_denied" {
		t.Fatalf("unexpected before grant: %+v", got)
	}
	perms.Grant(1, 2, PermissionMessageSend)
	if got := perms.Check(CheckRequest{TenantID: 1, UserID: 2, Permission: PermissionMessageSend}); !got.Allowed {
		t.Fatalf("expected allowed: %+v", got)
	}
}

func TestPermissionServiceRejectsInactiveMember(t *testing.T) {
	dir := NewDirectory()
	dir.UpsertTenant(Tenant{ID: 1, Status: 1})
	dir.UpsertMember(Member{TenantID: 1, UserID: 2, Status: MemberSuspended})
	perms := NewPermissionService(dir)
	perms.Grant(1, 2, PermissionMessageSend)
	if got := perms.Check(CheckRequest{TenantID: 1, UserID: 2, Permission: PermissionMessageSend}); got.Allowed || got.Reason != "member_inactive" {
		t.Fatalf("unexpected response: %+v", got)
	}
}
