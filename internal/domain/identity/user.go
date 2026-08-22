package identity

import (
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type Role string

const (
	Coordinator Role = "coordinator"
	Researcher  Role = "researcher"
	Reviewer    Role = "reviewer"
)

type User struct {
	ID           string
	TenantID     string
	Email        string
	DisplayName  string
	PasswordHash []byte `json:"-"`
	Roles        []Role
	Active       bool
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func NewUser(id, tenantID, email, displayName string, roles []Role, passwordHash []byte, now time.Time) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if id == "" || tenantID == "" {
		return User{}, fault.Invalid("identity", "must include id and tenant")
	}
	if !strings.Contains(email, "@") {
		return User{}, fault.Invalid("email", "must be a valid address")
	}
	if strings.TrimSpace(displayName) == "" {
		return User{}, fault.Invalid("display_name", "must not be empty")
	}
	if len(passwordHash) == 0 {
		return User{}, fault.Invalid("password", "must not be empty")
	}
	if err := ValidateRoles(roles); err != nil {
		return User{}, err
	}
	return User{
		ID: id, TenantID: tenantID, Email: email, DisplayName: strings.TrimSpace(displayName),
		PasswordHash: append([]byte(nil), passwordHash...), Roles: append([]Role(nil), roles...),
		Active: true, Version: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}, nil
}

func ValidateRoles(roles []Role) error {
	if len(roles) == 0 {
		return fault.Invalid("roles", "must contain at least one role")
	}
	seen := map[Role]struct{}{}
	for _, role := range roles {
		switch role {
		case Coordinator, Researcher, Reviewer:
		default:
			return fault.Invalid("role", "is not supported")
		}
		if _, exists := seen[role]; exists {
			return fault.Invalid("roles", "must not contain duplicates")
		}
		seen[role] = struct{}{}
	}
	return nil
}

func (u User) HasRole(role Role) bool {
	for _, candidate := range u.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func (u User) CanCoordinate() bool { return u.Active && u.HasRole(Coordinator) }
func (u User) CanResearch() bool   { return u.Active && u.HasRole(Researcher) }
func (u User) CanReview() bool     { return u.Active && u.HasRole(Reviewer) }

func (u *User) Deactivate(now time.Time) error {
	if !u.Active {
		return nil
	}
	u.Active = false
	u.Version++
	u.UpdatedAt = now.UTC()
	return nil
}

func (u User) Clone() User {
	clone := u
	clone.PasswordHash = append([]byte(nil), u.PasswordHash...)
	clone.Roles = append([]Role(nil), u.Roles...)
	return clone
}
