package service

import (
	"context"
	"errors"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

type ProgramService struct{ Dependencies }

type CreateProgramInput struct {
	EditionID string
	Name      string
	TimeZone  string
	Capacity  int
	StartsAt  time.Time
	EndsAt    time.Time
}

func (s ProgramService) Create(ctx context.Context, actor Actor, input CreateProgramInput) (program.Program, error) {
	if err := actor.Validate(); err != nil {
		return program.Program{}, err
	}
	coordinator, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return program.Program{}, err
	}
	if !coordinator.CanCoordinate() {
		return program.Program{}, fault.New(fault.Forbidden, "coordinator_required", "a coordinator is required")
	}
	edition, err := s.Store.GetEdition(ctx, actor.TenantID, input.EditionID)
	if err != nil {
		return program.Program{}, err
	}
	if edition.State != catalog.EditionActive {
		return program.Program{}, fault.New(fault.Precondition, "edition_inactive", "program requires an active physical edition")
	}
	now := s.Clock.Now()
	value, err := program.New(s.IDs.New("program"), actor.TenantID, edition.ID, actor.UserID, input.Name, input.TimeZone, input.Capacity, input.StartsAt, input.EndsAt, now)
	if err != nil {
		return program.Program{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.InsertProgram(ctx, value); err != nil {
			return err
		}
		member, err := program.NewMember(s.IDs.New("member"), actor.TenantID, value.ID, actor.UserID, now)
		if err != nil {
			return err
		}
		if err := tx.InsertMember(ctx, member); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "program.create", "program", value.ID, audit.Succeeded, map[string]any{"edition_id": edition.ID}, now)
	})
	if err != nil {
		return program.Program{}, err
	}
	return value, nil
}

func (s ProgramService) OpenEnrollment(ctx context.Context, actor Actor, programID string) (program.Program, error) {
	return s.transition(ctx, actor, programID, "program.open_enrollment", func(value *program.Program, now time.Time) error {
		return value.OpenEnrollment(now)
	})
}

func (s ProgramService) Start(ctx context.Context, actor Actor, programID string) (program.Program, error) {
	return s.transition(ctx, actor, programID, "program.start", func(value *program.Program, now time.Time) error {
		count, err := s.Store.CountActiveMembers(ctx, actor.TenantID, value.ID)
		if err != nil {
			return err
		}
		if count < 2 {
			return fault.New(fault.Precondition, "program_members_insufficient", "program requires at least two active members")
		}
		return value.Start(now)
	})
}

func (s ProgramService) transition(ctx context.Context, actor Actor, programID, action string, change func(*program.Program, time.Time) error) (program.Program, error) {
	if err := actor.Validate(); err != nil {
		return program.Program{}, err
	}
	user, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return program.Program{}, err
	}
	if !user.CanCoordinate() {
		return program.Program{}, fault.New(fault.Forbidden, "coordinator_required", "a coordinator is required")
	}
	value, err := s.Store.GetProgram(ctx, actor.TenantID, programID)
	if err != nil {
		return program.Program{}, err
	}
	if value.CoordinatorID != actor.UserID {
		return program.Program{}, fault.New(fault.Forbidden, "program_coordinator_mismatch", "only the assigned coordinator can change this program")
	}
	now := s.Clock.Now()
	previous := value.Version
	if err := change(&value, now); err != nil {
		return program.Program{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.UpdateProgram(ctx, value, previous); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, action, "program", value.ID, audit.Succeeded, map[string]any{"state": value.State}, now)
	})
	return value, err
}

func (s ProgramService) Join(ctx context.Context, actor Actor, programID, idempotencyKey, requestHash string) (program.Member, error) {
	if err := actor.Validate(); err != nil {
		return program.Member{}, err
	}
	user, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return program.Member{}, err
	}
	if !user.CanResearch() {
		return program.Member{}, fault.New(fault.Forbidden, "researcher_required", "a researcher role is required")
	}
	now := s.Clock.Now()
	value, err := program.NewMember(s.IDs.New("member"), actor.TenantID, programID, actor.UserID, now)
	if err != nil {
		return program.Member{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		existing, err := tx.GetIdempotency(ctx, actor.TenantID, idempotencyKey, "POST", "/v1/programs/"+programID+"/members", actor.UserID)
		if err == nil {
			if !existing.Matches("POST", "/v1/programs/"+programID+"/members", actor.UserID, requestHash) {
				return fault.New(fault.Conflict, "idempotency_payload_changed", "idempotency key was already used for a different request")
			}
			persisted, getErr := tx.GetMember(ctx, actor.TenantID, programID, actor.UserID)
			if getErr != nil {
				return getErr
			}
			value = persisted
			return nil
		}
		if !fault.IsKind(err, fault.NotFound) {
			return err
		}
		current, err := tx.GetProgram(ctx, actor.TenantID, programID)
		if err != nil {
			return err
		}
		if !current.CanAcceptMembers(now) {
			return fault.New(fault.Precondition, "program_not_enrolling", "program is not accepting members")
		}
		count, err := tx.CountActiveMembers(ctx, actor.TenantID, programID)
		if err != nil {
			return err
		}
		if count >= current.Capacity {
			return fault.New(fault.Conflict, "program_capacity_reached", "program has no remaining places")
		}
		if err := tx.InsertMember(ctx, value); err != nil {
			return err
		}
		record, err := repository.NewIdempotencyRecord(actor.TenantID, idempotencyKey, "POST", "/v1/programs/"+programID+"/members", actor.UserID, requestHash, 201, []byte(value.ID), now, 24*time.Hour)
		if err != nil {
			return err
		}
		if err := tx.InsertIdempotency(ctx, record); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "program.join", "program_member", value.ID, audit.Succeeded, map[string]any{"program_id": programID}, now)
	})
	if err != nil {
		if fault.IsKind(err, fault.Conflict) {
			existing, getErr := s.Store.GetMember(ctx, actor.TenantID, programID, actor.UserID)
			if getErr == nil {
				return existing, nil
			}
		}
		return program.Member{}, err
	}
	return value, nil
}

type BatchMemberResult struct {
	UserID string
	Member *program.Member
	Error  error
}

func (s ProgramService) AddMembersAtomically(ctx context.Context, actor Actor, programID string, userIDs []string) ([]BatchMemberResult, error) {
	if len(userIDs) == 0 {
		return nil, fault.Invalid("members", "must not be empty")
	}
	seen := map[string]struct{}{}
	results := make([]BatchMemberResult, len(userIDs))
	now := s.Clock.Now()
	err := s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		value, err := tx.GetProgram(ctx, actor.TenantID, programID)
		if err != nil {
			return err
		}
		if value.CoordinatorID != actor.UserID || !value.CanAcceptMembers(now) {
			return fault.New(fault.Forbidden, "program_enrollment_forbidden", "program cannot be managed by this actor")
		}
		count, err := tx.CountActiveMembers(ctx, actor.TenantID, programID)
		if err != nil {
			return err
		}
		if count+len(userIDs) > value.Capacity {
			return fault.New(fault.Conflict, "program_capacity_reached", "batch would exceed program capacity")
		}
		for index, userID := range userIDs {
			results[index].UserID = userID
			if _, duplicate := seen[userID]; duplicate {
				return fault.Invalid("members", "must not contain duplicates")
			}
			seen[userID] = struct{}{}
			user, err := tx.GetUser(ctx, actor.TenantID, userID)
			if err != nil {
				return err
			}
			if !user.CanResearch() {
				return fault.New(fault.Precondition, "member_not_researcher", "every member must be an active researcher")
			}
			member, err := program.NewMember(s.IDs.New("member"), actor.TenantID, programID, userID, now)
			if err != nil {
				return err
			}
			if err := tx.InsertMember(ctx, member); err != nil {
				return err
			}
			results[index].Member = &member
		}
		return addAudit(ctx, tx, s.IDs, actor, "program.members.batch_add", "program", programID, audit.Succeeded, map[string]any{"member_count": len(userIDs)}, now)
	})
	if err != nil {
		for index := range results {
			results[index].Error = err
			results[index].Member = nil
		}
		return results, err
	}
	return results, nil
}

func (s ProgramService) RequestReview(ctx context.Context, actor Actor, programID string) (program.Program, repository.ProgramReadiness, error) {
	value, err := s.Store.GetProgram(ctx, actor.TenantID, programID)
	if err != nil {
		return program.Program{}, repository.ProgramReadiness{}, err
	}
	if value.CoordinatorID != actor.UserID {
		return program.Program{}, repository.ProgramReadiness{}, fault.New(fault.Forbidden, "program_coordinator_mismatch", "only the assigned coordinator can request review")
	}
	now := s.Clock.Now()
	readiness, err := s.Store.GetProgramReadiness(ctx, actor.TenantID, programID, now)
	if err != nil {
		return program.Program{}, repository.ProgramReadiness{}, err
	}
	if !readiness.ReadyForReview() {
		return program.Program{}, readiness, fault.New(fault.Precondition, "program_reading_incomplete", "all reading activity must finish before review")
	}
	previous := value.Version
	if err := value.RequestReview(now); err != nil {
		return program.Program{}, readiness, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		currentReadiness := repository.PreserveReadinessSnapshot(ctx, tx, readiness)
		if !currentReadiness.ReadyForReview() {
			return fault.New(fault.Conflict, "program_readiness_changed", "reading activity changed while review was requested")
		}
		if err := tx.UpdateProgram(ctx, value, previous); err != nil {
			return err
		}
		if err := addOutbox(ctx, tx, s.IDs, actor.TenantID, "program.review_requested", programID, map[string]string{"program_id": programID}, now); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "program.request_review", "program", programID, audit.Succeeded, readiness, now)
	})
	return value, readiness, err
}

func isMissing(err error) bool {
	return errors.Is(err, context.Canceled) || fault.IsKind(err, fault.NotFound)
}

var _ = identity.Coordinator
