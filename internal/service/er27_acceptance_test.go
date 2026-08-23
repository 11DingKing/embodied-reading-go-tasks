package service_test

import (
	"context"
	"errors"
	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/archive"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/evidence"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"testing"
	"time"
)

type archiveCommitStore struct {
	repository.Store
	job       archive.Job
	value     program.Program
	snapshots int
}

func (s *archiveCommitStore) GetArchiveJob(context.Context, string, string) (archive.Job, error) {
	return s.job, nil
}
func (s *archiveCommitStore) UpdateArchiveJob(_ context.Context, j archive.Job, _ int64) error {
	s.job = j
	return nil
}
func (s *archiveCommitStore) GetProgram(context.Context, string, string) (program.Program, error) {
	return s.value, nil
}
func (*archiveCommitStore) GetProgramReadiness(context.Context, string, string, time.Time) (repository.ProgramReadiness, error) {
	return repository.ProgramReadiness{ActiveMembers: 1}, nil
}
func (*archiveCommitStore) ListFinalClaimsForProgram(context.Context, string, string) ([]evidence.Claim, error) {
	return nil, nil
}
func (s *archiveCommitStore) InsertArchiveSnapshot(context.Context, repository.ArchiveSnapshot) error {
	s.snapshots++
	return nil
}
func (s *archiveCommitStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	oldJ, oldV, oldS := s.job, s.value, s.snapshots
	err := fn(ctx, &archiveCommitTx{s: s})
	if err != nil {
		s.job, s.value, s.snapshots = oldJ, oldV, oldS
	}
	return err
}

type archiveCommitTx struct {
	repository.Tx
	s *archiveCommitStore
}

func (*archiveCommitTx) GetProgramReadiness(context.Context, string, string, time.Time) (repository.ProgramReadiness, error) {
	return repository.ProgramReadiness{ActiveMembers: 1}, nil
}
func (t *archiveCommitTx) InsertArchiveSnapshot(c context.Context, v repository.ArchiveSnapshot) error {
	return t.s.InsertArchiveSnapshot(c, v)
}
func (*archiveCommitTx) UpdateArchiveJob(context.Context, archive.Job, int64) error {
	return errors.New("archive job version changed")
}
func (*archiveCommitTx) UpdateProgram(context.Context, program.Program, int64) error { return nil }
func (*archiveCommitTx) InsertOutbox(context.Context, repository.OutboxEvent) error  { return nil }
func (*archiveCommitTx) InsertAudit(context.Context, audit.Event) error              { return nil }
func TestArchiveCommitFailureExposesNoPartialSnapshotOrTerminalState(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	job, _ := archive.NewJob("job", "tenant", "program", 3, now)
	value := program.Program{ID: "program", TenantID: "tenant", State: program.ReviewPending, Version: 1}
	store := &archiveCommitStore{job: job, value: value}
	svc := service.ArchiveService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}, LeaseTTL: time.Hour, MaxAttempts: 3}
	_ = svc.Process(context.Background(), "tenant", "job", "worker")
	if store.snapshots != 0 {
		t.Fatal("archive failure exposed snapshot")
	}
}
