package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/archive"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

type ArchiveService struct {
	Dependencies
	LeaseTTL    time.Duration
	MaxAttempts int
	Backoff     func(int) time.Duration
}

func (s ArchiveService) Request(ctx context.Context, actor Actor, programID string) (archive.Job, repository.ProgramReadiness, error) {
	value, err := s.Store.GetProgram(ctx, actor.TenantID, programID)
	if err != nil {
		return archive.Job{}, repository.ProgramReadiness{}, err
	}
	if value.CoordinatorID != actor.UserID || value.State != program.ReviewPending {
		return archive.Job{}, repository.ProgramReadiness{}, fault.New(fault.Forbidden, "archive_request_forbidden", "only the coordinator can archive a review-pending program")
	}
	now := s.Clock.Now()
	readiness, err := s.Store.GetProgramReadiness(ctx, actor.TenantID, programID, now)
	if err != nil {
		return archive.Job{}, repository.ProgramReadiness{}, err
	}
	if !readiness.ReadyForArchive() {
		return archive.Job{}, readiness, fault.New(fault.Precondition, "program_not_archivable", "program still owns active reading, evidence, seminar, or worker resources")
	}
	job, err := archive.NewJob(s.IDs.New("archive"), actor.TenantID, programID, s.MaxAttempts, now)
	if err != nil {
		return archive.Job{}, readiness, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		freshReadiness, err := tx.GetProgramReadiness(ctx, actor.TenantID, programID, now)
		if err != nil {
			return err
		}
		if !freshReadiness.ReadyForArchive() {
			return fault.New(fault.Conflict, "archive_readiness_changed", "program resources changed before archive job was queued")
		}
		if err := tx.InsertArchiveJob(ctx, job); err != nil {
			return err
		}
		if err := addOutbox(ctx, tx, s.IDs, actor.TenantID, "archive.requested", programID, map[string]string{"job_id": job.ID}, now); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "archive.request", "archive_job", job.ID, audit.Succeeded, readiness, now)
	})
	return job, readiness, err
}

type ArchivePayload struct {
	Program    program.Program             `json:"program"`
	Readiness  repository.ProgramReadiness `json:"readiness"`
	Claims     any                         `json:"claims"`
	ArchivedAt time.Time                   `json:"archived_at"`
}

func (s ArchiveService) Process(ctx context.Context, tenantID, jobID, workerID string) error {
	job, err := s.Store.GetArchiveJob(ctx, tenantID, jobID)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	claimVersion := job.Version
	if err := job.Claim(workerID, s.LeaseTTL, now); err != nil {
		return err
	}
	if err := s.Store.UpdateArchiveJob(ctx, job, claimVersion); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return s.failJob(context.WithoutCancel(ctx), job, workerID, err)
	}
	beginVersion := job.Version
	if err := job.BeginWrite(workerID, job.LeaseToken, s.Clock.Now()); err != nil {
		return err
	}
	if err := s.Store.UpdateArchiveJob(ctx, job, beginVersion); err != nil {
		return err
	}
	value, err := s.Store.GetProgram(ctx, tenantID, job.ProgramID)
	if err != nil {
		return s.failJob(context.WithoutCancel(ctx), job, workerID, err)
	}
	readiness, err := s.Store.GetProgramReadiness(ctx, tenantID, job.ProgramID, s.Clock.Now())
	if err != nil {
		return s.failJob(context.WithoutCancel(ctx), job, workerID, err)
	}
	if !readiness.ReadyForArchive() {
		return s.failJob(context.WithoutCancel(ctx), job, workerID, fault.New(fault.Precondition, "program_no_longer_archivable", "program resources changed during archive processing"))
	}
	claims, err := s.Store.ListFinalClaimsForProgram(ctx, tenantID, job.ProgramID)
	if err != nil {
		return s.failJob(context.WithoutCancel(ctx), job, workerID, err)
	}
	payload, err := json.Marshal(ArchivePayload{Program: value, Readiness: readiness, Claims: claims, ArchivedAt: s.Clock.Now()})
	if err != nil {
		return s.failJob(context.WithoutCancel(ctx), job, workerID, err)
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	completeVersion := job.Version
	if err := job.Succeed(workerID, job.LeaseToken, hash, s.Clock.Now()); err != nil {
		return err
	}
	programVersion := value.Version
	if err := value.Archive(s.Clock.Now()); err != nil {
		return s.failJob(context.WithoutCancel(ctx), job, workerID, err)
	}
	systemActor := Actor{TenantID: tenantID, UserID: "system", RequestID: s.IDs.New("request")}
	snapshot := repository.ArchiveSnapshot{ID: s.IDs.New("snapshot"), TenantID: tenantID, ProgramID: job.ProgramID, Hash: hash, Payload: payload, CreatedAt: s.Clock.Now()}
	return s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		freshReadiness, err := tx.GetProgramReadiness(ctx, tenantID, job.ProgramID, s.Clock.Now())
		if err != nil {
			return err
		}
		if !freshReadiness.ReadyForArchive() {
			return fault.New(fault.Conflict, "archive_readiness_changed", "program resources changed before snapshot commit")
		}
		if err := tx.InsertArchiveSnapshot(ctx, snapshot); err != nil {
			return err
		}
		if err := tx.UpdateArchiveJob(ctx, job, completeVersion); err != nil {
			return err
		}
		if err := tx.UpdateProgram(ctx, value, programVersion); err != nil {
			return err
		}
		if err := addOutbox(ctx, tx, s.IDs, tenantID, "archive.completed", job.ProgramID, map[string]string{"hash": hash}, s.Clock.Now()); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, systemActor, "archive.complete", "archive_job", job.ID, audit.Succeeded, map[string]string{"snapshot_hash": hash}, s.Clock.Now())
	})
}

func (s ArchiveService) failJob(ctx context.Context, job archive.Job, workerID string, cause error) error {
	previous := job.Version
	backoff := time.Minute
	if s.Backoff != nil {
		backoff = s.Backoff(job.Attempt)
	}
	if err := job.Fail(workerID, job.LeaseToken, cause, backoff, s.Clock.Now()); err != nil {
		return err
	}
	if err := s.Store.UpdateArchiveJob(ctx, job, previous); err != nil {
		return err
	}
	return cause
}
