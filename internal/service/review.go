package service

import (
	"context"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/evidence"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/review"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

type ReviewService struct {
	Dependencies
	LeaseTTL time.Duration
}

func (s ReviewService) Assign(ctx context.Context, actor Actor, claimID, reviewerID string) (review.Assignment, error) {
	coordinator, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return review.Assignment{}, err
	}
	if !coordinator.CanCoordinate() {
		return review.Assignment{}, fault.New(fault.Forbidden, "coordinator_required", "a coordinator is required")
	}
	claim, err := s.Store.GetClaim(ctx, actor.TenantID, claimID)
	if err != nil {
		return review.Assignment{}, err
	}
	reviewer, err := s.Store.GetUser(ctx, actor.TenantID, reviewerID)
	if err != nil {
		return review.Assignment{}, err
	}
	if !reviewer.CanReview() {
		return review.Assignment{}, fault.New(fault.Precondition, "reviewer_ineligible", "reviewer must have an active review role")
	}
	now := s.Clock.Now()
	assignment, err := review.NewAssignment(s.IDs.New("review"), actor.TenantID, claim.ID, claim.AuthorID, reviewer.ID, now)
	if err != nil {
		return review.Assignment{}, err
	}
	claimVersion := claim.Version
	if err := claim.Assign(assignment.ID, now); err != nil {
		return review.Assignment{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		fresh, err := tx.GetClaim(ctx, actor.TenantID, claimID)
		if err != nil {
			return err
		}
		if fresh.Version != claimVersion || fresh.AuthorID == reviewerID {
			return fault.New(fault.Conflict, "claim_assignment_changed", "claim changed while review was assigned")
		}
		if err := tx.InsertReviewAssignment(ctx, assignment); err != nil {
			return err
		}
		if err := tx.UpdateClaim(ctx, claim, claimVersion); err != nil {
			return err
		}
		if err := addOutbox(ctx, tx, s.IDs, actor.TenantID, "review.assigned", assignment.ID, map[string]string{"reviewer_id": reviewerID, "claim_id": claimID}, now); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "review.assign", "review_assignment", assignment.ID, audit.Succeeded, map[string]string{"claim_id": claimID, "reviewer_id": reviewerID}, now)
	})
	if err != nil {
		return review.Assignment{}, err
	}
	return assignment, nil
}

func (s ReviewService) Claim(ctx context.Context, actor Actor, assignmentID, workerID string) (review.Assignment, error) {
	user, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return review.Assignment{}, err
	}
	if !user.CanReview() {
		return review.Assignment{}, fault.New(fault.Forbidden, "reviewer_required", "an active reviewer is required")
	}
	assignment, err := s.Store.GetReviewAssignment(ctx, actor.TenantID, assignmentID)
	if err != nil {
		return review.Assignment{}, err
	}
	if assignment.ReviewerID != actor.UserID {
		return review.Assignment{}, fault.New(fault.Forbidden, "reviewer_assignment_mismatch", "review is assigned to another reviewer")
	}
	now := s.Clock.Now()
	previous := assignment.Version
	if err := assignment.Claim(workerID, s.LeaseTTL, now); err != nil {
		return review.Assignment{}, err
	}
	if err := s.Store.UpdateReviewAssignment(ctx, assignment, previous); err != nil {
		return review.Assignment{}, err
	}
	return assignment, nil
}

func (s ReviewService) Renew(ctx context.Context, actor Actor, assignmentID, workerID string, token int64) (review.Assignment, error) {
	assignment, err := s.Store.GetReviewAssignment(ctx, actor.TenantID, assignmentID)
	if err != nil {
		return review.Assignment{}, err
	}
	if assignment.ReviewerID != actor.UserID {
		return review.Assignment{}, fault.New(fault.Forbidden, "reviewer_assignment_mismatch", "review is assigned to another reviewer")
	}
	now := s.Clock.Now()
	previous := assignment.Version
	if err := assignment.Renew(workerID, token, s.LeaseTTL, now); err != nil {
		return review.Assignment{}, err
	}
	if err := s.Store.UpdateReviewAssignment(ctx, assignment, previous); err != nil {
		return review.Assignment{}, err
	}
	return assignment, nil
}

type DecideInput struct {
	AssignmentID string
	WorkerID     string
	LeaseToken   int64
	Outcome      review.Outcome
	Rationale    string
}

type DecideResult struct {
	Assignment review.Assignment
	Claim      evidence.Claim
	Decision   review.Decision
}

func (s ReviewService) Decide(ctx context.Context, actor Actor, input DecideInput) (DecideResult, error) {
	assignment, err := s.Store.GetReviewAssignment(ctx, actor.TenantID, input.AssignmentID)
	if err != nil {
		return DecideResult{}, err
	}
	if assignment.ReviewerID != actor.UserID {
		return DecideResult{}, fault.New(fault.Forbidden, "reviewer_assignment_mismatch", "review is assigned to another reviewer")
	}
	claim, err := s.Store.GetClaim(ctx, actor.TenantID, assignment.ClaimID)
	if err != nil {
		return DecideResult{}, err
	}
	now := s.Clock.Now()
	decision, err := review.NewDecision(s.IDs.New("decision"), actor.TenantID, assignment.ID, claim.ID, actor.UserID, input.Outcome, input.Rationale, now)
	if err != nil {
		return DecideResult{}, err
	}
	assignmentVersion, claimVersion := assignment.Version, claim.Version
	if err := assignment.Complete(input.WorkerID, input.LeaseToken, now); err != nil {
		return DecideResult{}, err
	}
	if err := claim.Decide(input.Outcome == review.Verified, assignment.ID, now); err != nil {
		return DecideResult{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		fresh, err := tx.GetReviewAssignment(ctx, actor.TenantID, assignment.ID)
		if err != nil {
			return err
		}
		if fresh.Version != assignmentVersion || fresh.LeaseToken != input.LeaseToken || fresh.LeaseOwner != input.WorkerID {
			return fault.New(fault.Conflict, "review_lease_changed", "review lease changed before decision committed")
		}
		if err := tx.UpdateReviewAssignment(ctx, assignment, assignmentVersion); err != nil {
			return err
		}
		if err := tx.UpdateClaim(ctx, claim, claimVersion); err != nil {
			return err
		}
		if err := tx.InsertReviewDecision(ctx, decision); err != nil {
			return err
		}
		if err := addOutbox(ctx, tx, s.IDs, actor.TenantID, "review.decided", decision.ID, map[string]any{"claim_id": claim.ID, "outcome": decision.Outcome}, now); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "review.decide", "review_assignment", assignment.ID, audit.Succeeded, map[string]any{"outcome": decision.Outcome}, now)
	})
	if err != nil {
		return DecideResult{}, err
	}
	return DecideResult{Assignment: assignment, Claim: claim, Decision: decision}, nil
}

func (s ReviewService) OpenDispute(ctx context.Context, actor Actor, claimID, reason string) (evidence.Dispute, error) {
	claim, err := s.Store.GetClaim(ctx, actor.TenantID, claimID)
	if err != nil {
		return evidence.Dispute{}, err
	}
	if claim.AuthorID != actor.UserID {
		return evidence.Dispute{}, fault.New(fault.Forbidden, "claim_author_required", "only the claim author can open a dispute")
	}
	now := s.Clock.Now()
	dispute, err := evidence.NewDispute(s.IDs.New("dispute"), actor.TenantID, claim.ID, actor.UserID, reason, now)
	if err != nil {
		return evidence.Dispute{}, err
	}
	claimVersion := claim.Version
	if err := claim.Dispute(now); err != nil {
		return evidence.Dispute{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.UpdateClaim(ctx, claim, claimVersion); err != nil {
			return err
		}
		if err := tx.InsertDispute(ctx, dispute); err != nil {
			return err
		}
		if err := addOutbox(ctx, tx, s.IDs, actor.TenantID, "claim.disputed", claim.ID, map[string]string{"dispute_id": dispute.ID}, now); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "claim.dispute", "claim", claim.ID, audit.Succeeded, map[string]string{"dispute_id": dispute.ID}, now)
	})
	return dispute, err
}

type ResolveDisputeInput struct {
	DisputeID  string
	ReviewID   string
	ResolverID string
	Uphold     bool
	Resolution string
}

func (s ReviewService) ResolveDispute(ctx context.Context, actor Actor, input ResolveDisputeInput) (evidence.Dispute, evidence.Claim, error) {
	dispute, err := s.Store.GetDispute(ctx, actor.TenantID, input.DisputeID)
	if err != nil {
		return evidence.Dispute{}, evidence.Claim{}, err
	}
	claim, err := s.Store.GetClaim(ctx, actor.TenantID, dispute.ClaimID)
	if err != nil {
		return evidence.Dispute{}, evidence.Claim{}, err
	}
	resolver, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return evidence.Dispute{}, evidence.Claim{}, err
	}
	if !resolver.CanReview() || actor.UserID != input.ResolverID || actor.UserID == claim.AuthorID {
		return evidence.Dispute{}, evidence.Claim{}, fault.New(fault.Forbidden, "independent_resolver_required", "an independent reviewer must resolve the dispute")
	}
	if dispute.State == evidence.DisputeOpen {
		if err := dispute.Assign(actor.UserID); err != nil {
			return evidence.Dispute{}, evidence.Claim{}, err
		}
	}
	now := s.Clock.Now()
	disputeVersion, claimVersion := dispute.Version, claim.Version
	if err := dispute.FinishResolution(input.Uphold, actor.UserID, input.Resolution, now); err != nil {
		return evidence.Dispute{}, evidence.Claim{}, err
	}
	if claim.State == evidence.ClaimDisputed {
		if err := claim.Assign(input.ReviewID, now); err != nil {
			return evidence.Dispute{}, evidence.Claim{}, err
		}
	}
	if err := claim.Resolve(input.Uphold, input.ReviewID, now); err != nil {
		return evidence.Dispute{}, evidence.Claim{}, err
	}
	if err := s.Store.UpdateDispute(ctx, dispute, disputeVersion); err != nil {
		return evidence.Dispute{}, evidence.Claim{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.UpdateClaim(ctx, claim, claimVersion); err != nil {
			return err
		}
		if err := addOutbox(ctx, tx, s.IDs, actor.TenantID, "claim.dispute_resolved", claim.ID, map[string]any{"uphold": input.Uphold}, now); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "claim.resolve_dispute", "claim", claim.ID, audit.Succeeded, map[string]any{"uphold": input.Uphold}, now)
	})
	return dispute, claim, err
}

func (s ReviewService) Queue(ctx context.Context, actor Actor, states []review.AssignmentState, page repository.Page) (repository.ReviewQueuePage, error) {
	user, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return repository.ReviewQueuePage{}, err
	}
	if !user.CanReview() {
		return repository.ReviewQueuePage{}, fault.New(fault.Forbidden, "reviewer_required", "an active reviewer is required")
	}
	return s.Store.ListReviewQueue(ctx, repository.ReviewQueueFilter{TenantID: actor.TenantID, ReviewerID: actor.UserID, States: states, Page: page})
}
