package program

import (
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type State string

const (
	Draft         State = "draft"
	Enrolling     State = "enrolling"
	Active        State = "active"
	ReviewPending State = "review_pending"
	Archived      State = "archived"
	Cancelled     State = "cancelled"
)

type Program struct {
	ID            string
	TenantID      string
	EditionID     string
	CoordinatorID string
	Name          string
	TimeZone      string
	Capacity      int
	StartsAt      time.Time
	EndsAt        time.Time
	State         State
	Version       int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func New(id, tenantID, editionID, coordinatorID, name, timezone string, capacity int, startsAt, endsAt, now time.Time) (Program, error) {
	if id == "" || tenantID == "" || editionID == "" || coordinatorID == "" {
		return Program{}, fault.Invalid("program", "requires id, tenant, edition, and coordinator")
	}
	if strings.TrimSpace(name) == "" {
		return Program{}, fault.Invalid("name", "must not be empty")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Program{}, fault.Invalid("timezone", "must be an IANA location")
	}
	if capacity < 2 {
		return Program{}, fault.Invalid("capacity", "must allow at least two members")
	}
	startsAt, endsAt = startsAt.UTC(), endsAt.UTC()
	if !endsAt.After(startsAt) {
		return Program{}, fault.Invalid("window", "must end after it starts")
	}
	now = now.UTC()
	return Program{
		ID: id, TenantID: tenantID, EditionID: editionID, CoordinatorID: coordinatorID,
		Name: strings.TrimSpace(name), TimeZone: timezone, Capacity: capacity, StartsAt: startsAt,
		EndsAt: endsAt, State: Draft, Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (p *Program) transition(next State, now time.Time, allowed ...State) error {
	for _, state := range allowed {
		if p.State == state {
			p.State = next
			p.Version++
			p.UpdatedAt = now.UTC()
			return nil
		}
	}
	return fault.StateConflict("program", string(p.State), string(next))
}

func (p *Program) OpenEnrollment(now time.Time) error {
	if !now.UTC().Before(p.StartsAt) {
		return fault.New(fault.Precondition, "enrollment_too_late", "enrollment must open before the program starts")
	}
	return p.transition(Enrolling, now, Draft)
}

func (p *Program) Start(now time.Time) error {
	now = now.UTC()
	if now.Before(p.StartsAt) || !now.Before(p.EndsAt) {
		return fault.New(fault.Precondition, "outside_program_window", "program can only start within its scheduled window")
	}
	return p.transition(Active, now, Enrolling)
}

func (p *Program) RequestReview(now time.Time) error {
	return p.transition(ReviewPending, now, Active)
}

func (p *Program) Archive(now time.Time) error {
	return p.transition(Archived, now, ReviewPending)
}

func (p *Program) Cancel(now time.Time) error {
	return p.transition(Cancelled, now, Draft, Enrolling)
}

func (p Program) CanAcceptMembers(now time.Time) bool {
	return p.State == Enrolling && now.UTC().Before(p.StartsAt)
}
