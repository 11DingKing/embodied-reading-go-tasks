package reading

import (
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type AssignmentState string

const (
	AssignmentReserved  AssignmentState = "reserved"
	AssignmentInReading AssignmentState = "in_reading"
	AssignmentSubmitted AssignmentState = "submitted"
	AssignmentReleased  AssignmentState = "released"
	AssignmentExpired   AssignmentState = "expired"
)

type PageRange struct {
	Start int
	End   int
}

func (p PageRange) Validate() error {
	if p.Start < 1 || p.End < p.Start {
		return fault.Invalid("page_range", "must be positive and ordered")
	}
	return nil
}

func (p PageRange) Overlaps(other PageRange) bool {
	return p.Start <= other.End && other.Start <= p.End
}

type Assignment struct {
	ID          string
	TenantID    string
	ProgramID   string
	EditionID   string
	MemberID    string
	Pages       PageRange
	WindowStart time.Time
	WindowEnd   time.Time
	State       AssignmentState
	LeaseToken  int64
	LeaseOwner  string
	LeaseUntil  time.Time
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewAssignment(id, tenantID, programID, editionID, memberID string, pages PageRange, start, end, now time.Time) (Assignment, error) {
	if id == "" || tenantID == "" || programID == "" || editionID == "" || memberID == "" {
		return Assignment{}, fault.Invalid("assignment", "requires all ownership identifiers")
	}
	if err := pages.Validate(); err != nil {
		return Assignment{}, err
	}
	start, end = start.UTC(), end.UTC()
	if !end.After(start) {
		return Assignment{}, fault.Invalid("assignment_window", "must end after it starts")
	}
	now = now.UTC()
	return Assignment{
		ID: id, TenantID: tenantID, ProgramID: programID, EditionID: editionID, MemberID: memberID,
		Pages: pages, WindowStart: start, WindowEnd: end, State: AssignmentReserved,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (a Assignment) Conflicts(other Assignment) bool {
	if a.TenantID != other.TenantID || a.ProgramID != other.ProgramID || a.EditionID != other.EditionID {
		return false
	}
	if a.State == AssignmentReleased || a.State == AssignmentExpired || other.State == AssignmentReleased || other.State == AssignmentExpired {
		return false
	}
	timeOverlap := a.WindowStart.Before(other.WindowEnd) && other.WindowStart.Before(a.WindowEnd)
	return timeOverlap && a.Pages.Overlaps(other.Pages)
}

func (a *Assignment) AcquireReadingLease(owner string, until, now time.Time) error {
	return a.Start(owner, until, now)
}

func (a *Assignment) Start(owner string, until, now time.Time) error {
	if a.State != AssignmentReserved {
		return fault.StateConflict("assignment", string(a.State), string(AssignmentInReading))
	}
	if owner == "" || !until.After(now) {
		return fault.Invalid("lease", "requires an owner and future expiry")
	}
	a.State = AssignmentInReading
	a.LeaseOwner = owner
	a.LeaseUntil = until.UTC()
	a.LeaseToken++
	a.Version++
	a.UpdatedAt = now.UTC()
	return nil
}

func (a *Assignment) Submit(owner string, token int64, now time.Time) error {
	if a.State != AssignmentInReading || a.LeaseOwner != owner || a.LeaseToken != token {
		return fault.New(fault.Conflict, "assignment_ownership_lost", "assignment is no longer owned by this reading session")
	}
	a.State = AssignmentSubmitted
	a.LeaseOwner = ""
	a.LeaseUntil = time.Time{}
	a.Version++
	a.UpdatedAt = now.UTC()
	return nil
}

func (a *Assignment) Release(now time.Time) error {
	if a.State == AssignmentSubmitted {
		return fault.StateConflict("assignment", string(a.State), string(AssignmentReleased))
	}
	if a.State == AssignmentReleased || a.State == AssignmentExpired {
		return nil
	}
	a.State = AssignmentReleased
	a.LeaseOwner = ""
	a.LeaseUntil = time.Time{}
	a.Version++
	a.UpdatedAt = now.UTC()
	return nil
}

func (a *Assignment) Expire(expectedToken int64, now time.Time) error {
	if a.LeaseToken != expectedToken {
		return fault.New(fault.Conflict, "assignment_lease_changed", "assignment lease changed before expiry")
	}
	if a.State != AssignmentReserved && a.State != AssignmentInReading {
		return fault.StateConflict("assignment", string(a.State), string(AssignmentExpired))
	}
	if now.UTC().Before(a.WindowEnd) && now.UTC().Before(a.LeaseUntil) {
		return fault.New(fault.Precondition, "assignment_not_expired", "assignment is still active")
	}
	a.State = AssignmentExpired
	a.LeaseOwner = ""
	a.LeaseUntil = time.Time{}
	a.Version++
	a.UpdatedAt = now.UTC()
	return nil
}
