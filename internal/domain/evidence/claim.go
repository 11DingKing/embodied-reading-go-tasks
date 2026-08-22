package evidence

import (
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type ClaimState string

const (
	ClaimPending  ClaimState = "pending"
	ClaimAssigned ClaimState = "assigned"
	ClaimVerified ClaimState = "verified"
	ClaimRejected ClaimState = "rejected"
	ClaimDisputed ClaimState = "disputed"
	ClaimResolved ClaimState = "resolved"
)

type Claim struct {
	ID              string
	TenantID        string
	SessionID       string
	EditionID       string
	AuthorID        string
	PageStart       int
	PageEnd         int
	QuotedText      string
	Interpretation  string
	State           ClaimState
	CurrentReviewID string
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func NewClaim(id, tenantID, sessionID, editionID, authorID string, start, end int, quoted, interpretation string, now time.Time) (Claim, error) {
	if id == "" || tenantID == "" || sessionID == "" || editionID == "" || authorID == "" {
		return Claim{}, fault.Invalid("claim", "requires all ownership identifiers")
	}
	if start < 1 || end < start {
		return Claim{}, fault.Invalid("claim_pages", "must be positive and ordered")
	}
	quoted, interpretation = strings.TrimSpace(quoted), strings.TrimSpace(interpretation)
	if len(quoted) < 4 || len(interpretation) < 12 {
		return Claim{}, fault.Invalid("claim_content", "requires a quotation and substantive interpretation")
	}
	now = now.UTC()
	return Claim{
		ID: id, TenantID: tenantID, SessionID: sessionID, EditionID: editionID, AuthorID: authorID,
		PageStart: start, PageEnd: end, QuotedText: quoted, Interpretation: interpretation,
		State: ClaimPending, Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (c *Claim) Assign(reviewID string, now time.Time) error {
	if c.State != ClaimPending && c.State != ClaimDisputed {
		return fault.StateConflict("claim", string(c.State), string(ClaimAssigned))
	}
	if reviewID == "" {
		return fault.Invalid("review_id", "must not be empty")
	}
	c.State = ClaimAssigned
	c.CurrentReviewID = reviewID
	c.Version++
	c.UpdatedAt = now.UTC()
	return nil
}

func (c *Claim) Decide(accepted bool, reviewID string, now time.Time) error {
	if c.State != ClaimAssigned || c.CurrentReviewID != reviewID {
		return fault.New(fault.Conflict, "claim_review_changed", "claim is assigned to another review")
	}
	if accepted {
		c.State = ClaimVerified
	} else {
		c.State = ClaimRejected
	}
	c.Version++
	c.UpdatedAt = now.UTC()
	return nil
}

func (c *Claim) Dispute(now time.Time) error {
	if c.State != ClaimRejected {
		return fault.StateConflict("claim", string(c.State), string(ClaimDisputed))
	}
	c.State = ClaimDisputed
	c.CurrentReviewID = ""
	c.Version++
	c.UpdatedAt = now.UTC()
	return nil
}

func (c *Claim) Resolve(accepted bool, reviewID string, now time.Time) error {
	if c.State != ClaimAssigned || c.CurrentReviewID != reviewID {
		return fault.New(fault.Conflict, "claim_dispute_review_changed", "disputed claim is assigned elsewhere")
	}
	c.State = ClaimResolved
	if !accepted {
		c.Interpretation = "[rejected after dispute] " + c.Interpretation
	}
	c.Version++
	c.UpdatedAt = now.UTC()
	return nil
}

func (c Claim) Final() bool {
	return c.State == ClaimVerified || c.State == ClaimResolved
}
