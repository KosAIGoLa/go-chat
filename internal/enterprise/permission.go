package enterprise

import "sync"

type Permission string

const (
	PermissionMessageSend Permission = "message:send"
	PermissionMessageSync Permission = "message:sync"
	PermissionAuditRead   Permission = "audit:read"
	PermissionOrgManage   Permission = "org:manage"
)

type CheckRequest struct {
	TenantID   uint64
	UserID     uint64
	Permission Permission
}

type CheckResponse struct {
	Allowed bool
	Reason  string
}

type PermissionService struct {
	directory *Directory
	mu        sync.RWMutex
	grants    map[uint64]map[uint64]map[Permission]struct{}
}

func NewPermissionService(directory *Directory) *PermissionService {
	return &PermissionService{directory: directory, grants: make(map[uint64]map[uint64]map[Permission]struct{})}
}

func (s *PermissionService) Grant(tenantID, userID uint64, permission Permission) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.grants[tenantID] == nil {
		s.grants[tenantID] = make(map[uint64]map[Permission]struct{})
	}
	if s.grants[tenantID][userID] == nil {
		s.grants[tenantID][userID] = make(map[Permission]struct{})
	}
	s.grants[tenantID][userID][permission] = struct{}{}
}

func (s *PermissionService) Check(req CheckRequest) CheckResponse {
	tenant, ok := s.directory.Tenant(req.TenantID)
	if !ok {
		return CheckResponse{Allowed: false, Reason: "tenant_not_found"}
	}
	if tenant.Status != 1 {
		return CheckResponse{Allowed: false, Reason: "tenant_disabled"}
	}
	member, ok := s.directory.Member(req.TenantID, req.UserID)
	if !ok {
		return CheckResponse{Allowed: false, Reason: "member_not_found"}
	}
	if member.Status != MemberActive {
		return CheckResponse{Allowed: false, Reason: "member_inactive"}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, allowed := s.grants[req.TenantID][req.UserID][req.Permission]
	if !allowed {
		return CheckResponse{Allowed: false, Reason: "permission_denied"}
	}
	return CheckResponse{Allowed: true}
}
