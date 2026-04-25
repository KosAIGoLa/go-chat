package enterprise

type Tenant struct {
	ID         uint64
	Name, Code string
	Status     uint8
}
type Department struct {
	ID, TenantID, ParentID uint64
	Name, Path             string
}
type Role struct {
	ID, TenantID uint64
	Code, Name   string
}
