package review

import (
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type Outcome string

const (
	Verified Outcome = "verified"
	Rejected Outcome = "rejected"
)

type Decision struct {
	ID           string
	TenantID     string
	AssignmentID string
	ClaimID      string
	ReviewerID   string
	Outcome      Outcome
	Rationale    string
	CreatedAt    time.Time
}

func NewDecision(id, tenantID, assignmentID, claimID, reviewerID string, outcome Outcome, rationale string, now time.Time) (Decision, error) {
	if id == "" || tenantID == "" || assignmentID == "" || claimID == "" || reviewerID == "" {
		return Decision{}, fault.Invalid("review_decision", "requires all identifiers")
	}
	if outcome != Verified && outcome != Rejected {
		return Decision{}, fault.Invalid("review_outcome", "must be verified or rejected")
	}
	rationale = strings.TrimSpace(rationale)
	if len(rationale) < 12 {
		return Decision{}, fault.Invalid("rationale", "must explain the decision")
	}
	return Decision{ID: id, TenantID: tenantID, AssignmentID: assignmentID, ClaimID: claimID, ReviewerID: reviewerID, Outcome: outcome, Rationale: rationale, CreatedAt: now.UTC()}, nil
}
