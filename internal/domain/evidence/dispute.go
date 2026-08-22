package evidence

import (
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type DisputeState string

const (
	DisputeOpen     DisputeState = "open"
	DisputeAssigned DisputeState = "assigned"
	DisputeUpheld   DisputeState = "upheld"
	DisputeDenied   DisputeState = "denied"
)

type Dispute struct {
	ID         string
	TenantID   string
	ClaimID    string
	OpenedBy   string
	Reason     string
	State      DisputeState
	ResolverID string
	Resolution string
	Version    int64
	OpenedAt   time.Time
	ResolvedAt *time.Time
}

func NewDispute(id, tenantID, claimID, openedBy, reason string, now time.Time) (Dispute, error) {
	if id == "" || tenantID == "" || claimID == "" || openedBy == "" {
		return Dispute{}, fault.Invalid("dispute", "requires id, tenant, claim, and actor")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 12 {
		return Dispute{}, fault.Invalid("dispute_reason", "must explain the evidentiary disagreement")
	}
	return Dispute{ID: id, TenantID: tenantID, ClaimID: claimID, OpenedBy: openedBy, Reason: reason, State: DisputeOpen, Version: 1, OpenedAt: now.UTC()}, nil
}

func (d *Dispute) Assign(resolverID string) error {
	if d.State != DisputeOpen {
		return fault.StateConflict("dispute", string(d.State), string(DisputeAssigned))
	}
	if resolverID == "" || resolverID == d.OpenedBy {
		return fault.New(fault.Precondition, "invalid_dispute_resolver", "dispute requires an independent resolver")
	}
	d.State = DisputeAssigned
	d.ResolverID = resolverID
	d.Version++
	return nil
}

func (d *Dispute) Resolve(uphold bool, resolverID, resolution string, now time.Time) error {
	if d.State != DisputeAssigned || d.ResolverID != resolverID {
		return fault.New(fault.Conflict, "dispute_resolver_changed", "dispute is assigned to another resolver")
	}
	resolution = strings.TrimSpace(resolution)
	if len(resolution) < 12 {
		return fault.Invalid("resolution", "must record the evidentiary conclusion")
	}
	if uphold {
		d.State = DisputeUpheld
	} else {
		d.State = DisputeDenied
	}
	now = now.UTC()
	d.Resolution = resolution
	d.ResolvedAt = &now
	d.Version++
	return nil
}
