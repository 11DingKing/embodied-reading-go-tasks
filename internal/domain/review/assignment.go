package review

import (
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type AssignmentState string

const (
	Queued    AssignmentState = "queued"
	Leased    AssignmentState = "leased"
	Decided   AssignmentState = "decided"
	Expired   AssignmentState = "expired"
	Cancelled AssignmentState = "cancelled"
)

type Assignment struct {
	ID            string
	TenantID      string
	ClaimID       string
	ClaimAuthorID string
	ReviewerID    string
	State         AssignmentState
	LeaseOwner    string
	LeaseToken    int64
	LeaseUntil    time.Time
	Attempt       int
	Version       int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewAssignment(id, tenantID, claimID, authorID, reviewerID string, now time.Time) (Assignment, error) {
	if id == "" || tenantID == "" || claimID == "" || authorID == "" || reviewerID == "" {
		return Assignment{}, fault.Invalid("review_assignment", "requires all identifiers")
	}
	if authorID == reviewerID {
		return Assignment{}, fault.New(fault.Precondition, "self_review_forbidden", "claim author cannot review their own claim")
	}
	now = now.UTC()
	return Assignment{
		ID: id, TenantID: tenantID, ClaimID: claimID, ClaimAuthorID: authorID, ReviewerID: reviewerID,
		State: Queued, Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (a *Assignment) Claim(owner string, ttl time.Duration, now time.Time) error {
	now = now.UTC()
	if owner == "" || ttl <= 0 {
		return fault.Invalid("review_lease", "requires owner and positive ttl")
	}
	if a.State == Leased && now.Before(a.LeaseUntil) {
		return fault.New(fault.Conflict, "review_already_leased", "review is already held by another worker")
	}
	if a.State != Queued && a.State != Leased && a.State != Expired {
		return fault.StateConflict("review_assignment", string(a.State), string(Leased))
	}
	a.State = Leased
	a.LeaseOwner = owner
	a.LeaseToken++
	a.LeaseUntil = now.Add(ttl)
	a.Attempt++
	a.Version++
	a.UpdatedAt = now
	return nil
}

func (a *Assignment) Renew(owner string, token int64, ttl time.Duration, now time.Time) error {
	if a.State != Leased || a.LeaseOwner != owner || a.LeaseToken != token {
		return fault.New(fault.Conflict, "review_lease_lost", "review lease is no longer owned by this worker")
	}
	if !now.UTC().Before(a.LeaseUntil) {
		return fault.New(fault.Conflict, "review_lease_expired", "review lease has expired")
	}
	a.LeaseUntil = now.UTC().Add(ttl)
	a.Version++
	a.UpdatedAt = now.UTC()
	return nil
}

func (a *Assignment) Complete(owner string, token int64, now time.Time) error {
	if a.State != Leased || a.LeaseOwner != owner || a.LeaseToken != token || !now.UTC().Before(a.LeaseUntil) {
		return fault.New(fault.Conflict, "review_lease_lost", "review decision arrived without a valid lease")
	}
	a.State = Decided
	a.LeaseOwner = ""
	a.LeaseUntil = time.Time{}
	a.Version++
	a.UpdatedAt = now.UTC()
	return nil
}
