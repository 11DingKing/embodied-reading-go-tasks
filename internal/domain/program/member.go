package program

import (
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type MemberState string

const (
	MemberActive    MemberState = "active"
	MemberCompleted MemberState = "completed"
	MemberWithdrawn MemberState = "withdrawn"
)

type Member struct {
	ID        string
	TenantID  string
	ProgramID string
	UserID    string
	State     MemberState
	JoinedAt  time.Time
	UpdatedAt time.Time
	Version   int64
}

func NewMember(id, tenantID, programID, userID string, now time.Time) (Member, error) {
	if id == "" || tenantID == "" || programID == "" || userID == "" {
		return Member{}, fault.Invalid("member", "requires id, tenant, program, and user")
	}
	now = now.UTC()
	return Member{ID: id, TenantID: tenantID, ProgramID: programID, UserID: userID, State: MemberActive, JoinedAt: now, UpdatedAt: now, Version: 1}, nil
}

func (m *Member) Complete(now time.Time) error {
	if m.State != MemberActive {
		return fault.StateConflict("member", string(m.State), string(MemberCompleted))
	}
	m.State = MemberCompleted
	m.Version++
	m.UpdatedAt = now.UTC()
	return nil
}

func (m *Member) Withdraw(now time.Time) error {
	if m.State != MemberActive {
		return fault.StateConflict("member", string(m.State), string(MemberWithdrawn))
	}
	m.State = MemberWithdrawn
	m.Version++
	m.UpdatedAt = now.UTC()
	return nil
}
