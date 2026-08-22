package service

import (
	"context"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/evidence"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

type ReadingService struct {
	Dependencies
	SessionLeaseTTL time.Duration
}

type ReserveInput struct {
	ProgramID string
	Pages     reading.PageRange
	StartsAt  time.Time
	EndsAt    time.Time
}

func (s ReadingService) Reserve(ctx context.Context, actor Actor, input ReserveInput) (reading.Assignment, error) {
	if err := actor.Validate(); err != nil {
		return reading.Assignment{}, err
	}
	value, err := s.Store.GetProgram(ctx, actor.TenantID, input.ProgramID)
	if err != nil {
		return reading.Assignment{}, err
	}
	if value.State != program.Active {
		return reading.Assignment{}, fault.New(fault.Precondition, "program_not_active", "page sections can only be reserved in an active program")
	}
	member, err := s.Store.GetMember(ctx, actor.TenantID, input.ProgramID, actor.UserID)
	if err != nil {
		return reading.Assignment{}, err
	}
	if member.State != program.MemberActive {
		return reading.Assignment{}, fault.New(fault.Precondition, "member_not_active", "active membership is required")
	}
	edition, err := s.Store.GetEdition(ctx, actor.TenantID, value.EditionID)
	if err != nil {
		return reading.Assignment{}, err
	}
	if edition.State != catalog.EditionActive {
		return reading.Assignment{}, fault.New(fault.Precondition, "edition_inactive", "physical edition is no longer active")
	}
	if err := edition.ValidatePageRange(input.Pages.Start, input.Pages.End); err != nil {
		return reading.Assignment{}, err
	}
	if input.StartsAt.Before(value.StartsAt) || input.EndsAt.After(value.EndsAt) {
		return reading.Assignment{}, fault.New(fault.Precondition, "assignment_outside_program", "assignment must remain inside the program window")
	}
	now := s.Clock.Now()
	assignment, err := reading.NewAssignment(s.IDs.New("assignment"), actor.TenantID, value.ID, edition.ID, member.ID, input.Pages, input.StartsAt, input.EndsAt, now)
	if err != nil {
		return reading.Assignment{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		conflicts, err := tx.FindAssignmentConflicts(ctx, actor.TenantID, value.ID, edition.ID, input.Pages, input.StartsAt, input.EndsAt, "")
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			return fault.New(fault.Conflict, "page_section_already_reserved", "requested page section overlaps an active reservation")
		}
		if err := tx.InsertAssignment(ctx, assignment); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "reading.reserve", "section_assignment", assignment.ID, audit.Succeeded,
			map[string]any{"page_start": assignment.Pages.Start, "page_end": assignment.Pages.End}, now)
	})
	if err != nil {
		return reading.Assignment{}, err
	}
	return assignment, nil
}

func (s ReadingService) MoveReservation(ctx context.Context, actor Actor, assignmentID string, pages reading.PageRange, start, end time.Time) (reading.Assignment, error) {
	assignment, err := s.Store.GetAssignment(ctx, actor.TenantID, assignmentID)
	if err != nil {
		return reading.Assignment{}, err
	}
	member, err := s.Store.GetMember(ctx, actor.TenantID, assignment.ProgramID, actor.UserID)
	if err != nil {
		return reading.Assignment{}, err
	}
	if member.ID != assignment.MemberID || assignment.State != reading.AssignmentReserved {
		return reading.Assignment{}, fault.New(fault.Forbidden, "reservation_not_movable", "only the owner can move an unused reservation")
	}
	edition, err := s.Store.GetEdition(ctx, actor.TenantID, assignment.EditionID)
	if err != nil {
		return reading.Assignment{}, err
	}
	if err := edition.ValidatePageRange(pages.Start, pages.End); err != nil {
		return reading.Assignment{}, err
	}
	if !end.After(start) {
		return reading.Assignment{}, fault.Invalid("assignment_window", "must end after it starts")
	}
	previous := assignment.Version
	now := s.Clock.Now()
	assignment.Pages = pages
	assignment.WindowStart = start.UTC()
	assignment.WindowEnd = end.UTC()
	assignment.Version++
	assignment.UpdatedAt = now
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		conflicts, err := tx.FindAssignmentConflicts(ctx, assignment.TenantID, assignment.ProgramID, assignment.EditionID, pages, start, end, assignment.ID)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			return fault.New(fault.Conflict, "page_section_already_reserved", "new page section overlaps an active reservation")
		}
		if err := tx.UpdateAssignment(ctx, assignment, previous); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "reading.reservation_move", "section_assignment", assignment.ID, audit.Succeeded, map[string]any{"page_start": pages.Start, "page_end": pages.End}, now)
	})
	return assignment, err
}

type StartReadingInput struct {
	AssignmentID string
	Intention    string
}

type StartReadingResult struct {
	Session    reading.Session
	Assignment reading.Assignment
}

func (s ReadingService) Start(ctx context.Context, actor Actor, input StartReadingInput) (StartReadingResult, error) {
	assignment, err := s.Store.GetAssignment(ctx, actor.TenantID, input.AssignmentID)
	if err != nil {
		return StartReadingResult{}, err
	}
	member, err := s.Store.GetMember(ctx, actor.TenantID, assignment.ProgramID, actor.UserID)
	if err != nil {
		return StartReadingResult{}, err
	}
	if member.ID != assignment.MemberID || member.State != program.MemberActive {
		return StartReadingResult{}, fault.New(fault.Forbidden, "assignment_owner_required", "active assignment owner is required")
	}
	now := s.Clock.Now()
	if now.Before(assignment.WindowStart) || !now.Before(assignment.WindowEnd) {
		return StartReadingResult{}, fault.New(fault.Precondition, "outside_assignment_window", "reading must begin inside the reserved window")
	}
	session, err := reading.NewSession(s.IDs.New("reading"), actor.TenantID, assignment.ProgramID, assignment.ID, member.ID, input.Intention, now)
	if err != nil {
		return StartReadingResult{}, err
	}
	if err := session.Start(now); err != nil {
		return StartReadingResult{}, err
	}
	previous := assignment.Version
	if err := assignment.AcquireReadingLease(session.ID, now.Add(s.SessionLeaseTTL), now); err != nil {
		return StartReadingResult{}, err
	}
	fresh, err := s.Store.GetAssignment(ctx, actor.TenantID, assignment.ID)
	if err != nil {
		return StartReadingResult{}, err
	}
	if fresh.Version != previous || fresh.State != reading.AssignmentReserved {
		return StartReadingResult{}, fault.New(fault.Conflict, "assignment_changed", "assignment changed before reading began")
	}
	if err := s.Store.UpdateAssignment(ctx, assignment, previous); err != nil {
		return StartReadingResult{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.InsertReadingSession(ctx, session); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "reading.start", "reading_session", session.ID, audit.Succeeded, map[string]string{"assignment_id": assignment.ID}, now)
	})
	if err != nil {
		return StartReadingResult{}, err
	}
	return StartReadingResult{Session: session, Assignment: assignment}, nil
}

func (s ReadingService) Abandon(ctx context.Context, actor Actor, sessionID, reason string) error {
	session, err := s.Store.GetReadingSession(ctx, actor.TenantID, sessionID)
	if err != nil {
		return err
	}
	assignment, err := s.Store.GetAssignment(ctx, actor.TenantID, session.AssignmentID)
	if err != nil {
		return err
	}
	member, err := s.Store.GetMember(ctx, actor.TenantID, session.ProgramID, actor.UserID)
	if err != nil {
		return err
	}
	if session.MemberID != member.ID {
		return fault.New(fault.Forbidden, "reading_owner_required", "only the reader can abandon this session")
	}
	now := s.Clock.Now()
	sessionVersion, assignmentVersion := session.Version, assignment.Version
	if err := session.Abandon(reason, now); err != nil {
		return err
	}
	if err := assignment.Release(now); err != nil {
		return err
	}
	return s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.UpdateReadingSession(ctx, session, sessionVersion); err != nil {
			return err
		}
		if err := tx.UpdateAssignment(ctx, assignment, assignmentVersion); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "reading.abandon", "reading_session", session.ID, audit.Succeeded, map[string]string{"reason": reason}, now)
	})
}

type ObservationInput struct {
	Page     int
	Channel  reading.SensoryChannel
	Body     string
	Sequence int
}

type ClaimInput struct {
	PageStart      int
	PageEnd        int
	QuotedText     string
	Interpretation string
}

type SubmitReadingInput struct {
	SessionID    string
	Observations []ObservationInput
	Claims       []ClaimInput
}

type SubmitReadingResult struct {
	Session      reading.Session
	Observations []reading.Observation
	Claims       []evidence.Claim
}

func (s ReadingService) Submit(ctx context.Context, actor Actor, input SubmitReadingInput) (SubmitReadingResult, error) {
	if len(input.Observations) == 0 || len(input.Claims) == 0 {
		return SubmitReadingResult{}, fault.New(fault.Precondition, "reading_evidence_required", "submission requires observations and quotation claims")
	}
	session, err := s.Store.GetReadingSession(ctx, actor.TenantID, input.SessionID)
	if err != nil {
		return SubmitReadingResult{}, err
	}
	assignment, err := s.Store.GetAssignment(ctx, actor.TenantID, session.AssignmentID)
	if err != nil {
		return SubmitReadingResult{}, err
	}
	member, err := s.Store.GetMember(ctx, actor.TenantID, session.ProgramID, actor.UserID)
	if err != nil {
		return SubmitReadingResult{}, err
	}
	if member.ID != session.MemberID || assignment.LeaseOwner != session.ID {
		return SubmitReadingResult{}, fault.New(fault.Conflict, "reading_ownership_lost", "reading session no longer owns its page section")
	}
	edition, err := s.Store.GetEdition(ctx, actor.TenantID, assignment.EditionID)
	if err != nil {
		return SubmitReadingResult{}, err
	}
	now := s.Clock.Now()
	observations := make([]reading.Observation, len(input.Observations))
	seenSequence := map[int]struct{}{}
	for index, item := range input.Observations {
		if _, duplicate := seenSequence[item.Sequence]; duplicate {
			return SubmitReadingResult{}, fault.Invalid("observation_sequence", "must be unique")
		}
		seenSequence[item.Sequence] = struct{}{}
		if item.Page < assignment.Pages.Start || item.Page > assignment.Pages.End {
			return SubmitReadingResult{}, fault.New(fault.Precondition, "observation_outside_assignment", "observation page is outside the reserved section")
		}
		observations[index], err = reading.NewObservation(s.IDs.New("observation"), actor.TenantID, session.ID, item.Page, item.Channel, item.Body, item.Sequence, now)
		if err != nil {
			return SubmitReadingResult{}, err
		}
	}
	claims := make([]evidence.Claim, len(input.Claims))
	for index, item := range input.Claims {
		if err := edition.ValidatePageRange(item.PageStart, item.PageEnd); err != nil {
			return SubmitReadingResult{}, err
		}
		if item.PageStart < assignment.Pages.Start || item.PageEnd > assignment.Pages.End {
			return SubmitReadingResult{}, fault.New(fault.Precondition, "claim_outside_assignment", "quotation claim is outside the reserved section")
		}
		claims[index], err = evidence.NewClaim(s.IDs.New("claim"), actor.TenantID, session.ID, edition.ID, actor.UserID, item.PageStart, item.PageEnd, item.QuotedText, item.Interpretation, now)
		if err != nil {
			return SubmitReadingResult{}, err
		}
	}
	sessionVersion, assignmentVersion := session.Version, assignment.Version
	if err := session.Submit(now); err != nil {
		return SubmitReadingResult{}, err
	}
	if err := assignment.Submit(session.ID, assignment.LeaseToken, now); err != nil {
		return SubmitReadingResult{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.UpdateReadingSession(ctx, session, sessionVersion); err != nil {
			return err
		}
		if err := tx.UpdateAssignment(ctx, assignment, assignmentVersion); err != nil {
			return err
		}
		for _, observation := range observations {
			if err := tx.InsertObservation(ctx, observation); err != nil {
				return err
			}
		}
		for _, claim := range claims {
			if err := tx.InsertClaim(ctx, claim); err != nil {
				return err
			}
			if err := addOutbox(ctx, tx, s.IDs, actor.TenantID, "claim.submitted", claim.ID, map[string]string{"claim_id": claim.ID}, now); err != nil {
				return err
			}
		}
		return addAudit(ctx, tx, s.IDs, actor, "reading.submit", "reading_session", session.ID, audit.Succeeded,
			map[string]any{"observations": len(observations), "claims": len(claims)}, now)
	})
	if err != nil {
		return SubmitReadingResult{}, err
	}
	return SubmitReadingResult{Session: session, Observations: observations, Claims: claims}, nil
}

func (s ReadingService) ExpireAssignments(ctx context.Context, limit int) (int, error) {
	now := s.Clock.Now()
	values, err := s.Store.ListExpiredAssignments(ctx, now, repository.Page{Limit: limit})
	if err != nil {
		return 0, err
	}
	completed := 0
	for _, value := range values {
		if err := ctx.Err(); err != nil {
			return completed, fault.Wrap(fault.Cancelled, "assignment_expiry_cancelled", "expire assignments", err)
		}
		previous := value.Version
		if err := value.Expire(value.LeaseToken, now); err != nil {
			if fault.IsKind(err, fault.Conflict) || fault.IsKind(err, fault.Precondition) {
				continue
			}
			return completed, err
		}
		if err := s.Store.UpdateAssignment(ctx, value, previous); err != nil {
			if fault.IsKind(err, fault.Conflict) {
				continue
			}
			return completed, err
		}
		completed++
	}
	return completed, nil
}
