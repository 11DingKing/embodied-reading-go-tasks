package archive

import (
	"context"
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type State string

const (
	Queued          State = "queued"
	Leased          State = "leased"
	Writing         State = "writing"
	RetryWait       State = "retry_wait"
	Complete        State = "complete"
	PermanentFailed State = "permanent_failed"
)

type Job struct {
	ID           string
	TenantID     string
	ProgramID    string
	State        State
	LeaseOwner   string
	LeaseToken   int64
	LeaseUntil   time.Time
	Attempt      int
	MaxAttempts  int
	NextTryAt    time.Time
	LastError    string
	SnapshotHash string
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func NewJob(id, tenantID, programID string, maxAttempts int, now time.Time) (Job, error) {
	if id == "" || tenantID == "" || programID == "" {
		return Job{}, fault.Invalid("archive_job", "requires id, tenant, and program")
	}
	if maxAttempts < 1 {
		return Job{}, fault.Invalid("max_attempts", "must be positive")
	}
	now = now.UTC()
	return Job{ID: id, TenantID: tenantID, ProgramID: programID, State: Queued, MaxAttempts: maxAttempts, NextTryAt: now, Version: 1, CreatedAt: now, UpdatedAt: now}, nil
}

func CleanupContext(ctx context.Context) context.Context { return ctx }

func (j *Job) Claim(owner string, ttl time.Duration, now time.Time) error {
	now = now.UTC()
	if owner == "" || ttl <= 0 {
		return fault.Invalid("archive_lease", "requires owner and positive ttl")
	}
	if j.State == Leased || j.State == Writing {
		if now.Before(j.LeaseUntil) {
			return fault.New(fault.Conflict, "archive_job_leased", "archive job is already leased")
		}
	}
	if j.State != Queued && j.State != RetryWait && j.State != Leased && j.State != Writing {
		return fault.StateConflict("archive_job", string(j.State), string(Leased))
	}
	if now.Before(j.NextTryAt) {
		return fault.New(fault.Precondition, "archive_retry_not_due", "archive job retry is not due")
	}
	j.State = Leased
	j.LeaseOwner = owner
	j.LeaseToken++
	j.LeaseUntil = now.Add(ttl)
	j.Attempt++
	j.Version++
	j.UpdatedAt = now
	return nil
}

func (j *Job) BeginWrite(owner string, token int64, now time.Time) error {
	if j.State != Leased || j.LeaseOwner != owner || j.LeaseToken != token || !now.UTC().Before(j.LeaseUntil) {
		return fault.New(fault.Conflict, "archive_lease_lost", "archive lease is no longer valid")
	}
	j.State = Writing
	j.Version++
	j.UpdatedAt = now.UTC()
	return nil
}

func (j *Job) Succeed(owner string, token int64, hash string, now time.Time) error {
	if j.State != Writing || j.LeaseOwner != owner || j.LeaseToken != token || !now.UTC().Before(j.LeaseUntil) {
		return fault.New(fault.Conflict, "archive_lease_lost", "archive completion arrived without ownership")
	}
	if len(strings.TrimSpace(hash)) < 16 {
		return fault.Invalid("snapshot_hash", "must identify the archived content")
	}
	j.State = Complete
	j.SnapshotHash = strings.TrimSpace(hash)
	j.LeaseOwner = ""
	j.LeaseUntil = time.Time{}
	j.Version++
	j.UpdatedAt = now.UTC()
	return nil
}

func (j *Job) Fail(owner string, token int64, cause error, backoff time.Duration, now time.Time) error {
	if j.State != Leased && j.State != Writing {
		return fault.StateConflict("archive_job", string(j.State), string(RetryWait))
	}
	if j.LeaseOwner != owner || j.LeaseToken != token {
		return fault.New(fault.Conflict, "archive_lease_lost", "archive failure arrived without ownership")
	}
	if cause == nil {
		return fault.Invalid("archive_error", "must not be nil")
	}
	j.LastError = cause.Error()
	j.LeaseOwner = ""
	j.LeaseUntil = time.Time{}
	if j.Attempt >= j.MaxAttempts {
		j.State = PermanentFailed
	} else {
		j.State = RetryWait
		j.NextTryAt = now.UTC().Add(backoff)
	}
	j.Version++
	j.UpdatedAt = now.UTC()
	return nil
}
