package enterprise

import "sync"

type MemberStatus uint8

const (
	MemberActive    MemberStatus = 1
	MemberSuspended MemberStatus = 2
	MemberResigned  MemberStatus = 3
)

type Member struct {
	TenantID     uint64
	UserID       uint64
	EmployeeNo   string
	DisplayName  string
	DepartmentID uint64
	Status       MemberStatus
}

type Directory struct {
	mu          sync.RWMutex
	tenants     map[uint64]Tenant
	departments map[uint64]map[uint64]Department
	members     map[uint64]map[uint64]Member
}

func NewDirectory() *Directory {
	return &Directory{tenants: make(map[uint64]Tenant), departments: make(map[uint64]map[uint64]Department), members: make(map[uint64]map[uint64]Member)}
}

func (d *Directory) UpsertTenant(tenant Tenant) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tenants[tenant.ID] = tenant
}

func (d *Directory) UpsertDepartment(dept Department) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.departments[dept.TenantID] == nil {
		d.departments[dept.TenantID] = make(map[uint64]Department)
	}
	d.departments[dept.TenantID][dept.ID] = dept
}

func (d *Directory) UpsertMember(member Member) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.members[member.TenantID] == nil {
		d.members[member.TenantID] = make(map[uint64]Member)
	}
	d.members[member.TenantID][member.UserID] = member
}

func (d *Directory) Tenant(id uint64) (Tenant, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	t, ok := d.tenants[id]
	return t, ok
}

func (d *Directory) Member(tenantID, userID uint64) (Member, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	m, ok := d.members[tenantID][userID]
	return m, ok
}
