package service_test

import (
	"context"
	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/archive"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"testing"
	"time"
)

type archiveRequestStore struct {
	repository.Store
	jobs int
}

func (*archiveRequestStore) GetProgram(context.Context, string, string) (program.Program, error) {
	return program.Program{ID: "program", TenantID: "tenant", CoordinatorID: "coord", State: program.ReviewPending}, nil
}
func (*archiveRequestStore) GetProgramReadiness(context.Context, string, string, time.Time) (repository.ProgramReadiness, error) {
	return repository.ProgramReadiness{ActiveMembers: 1}, nil
}
func (s *archiveRequestStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	return fn(ctx, &archiveRequestTx{s: s})
}

type archiveRequestTx struct {
	repository.Tx
	s *archiveRequestStore
}

func (*archiveRequestTx) GetProgramReadiness(context.Context, string, string, time.Time) (repository.ProgramReadiness, error) {
	return repository.ProgramReadiness{ActiveMembers: 1, ActiveAssignments: 1}, nil
}
func (t *archiveRequestTx) InsertArchiveJob(context.Context, archive.Job) error {
	t.s.jobs++
	return nil
}
func (*archiveRequestTx) InsertOutbox(context.Context, repository.OutboxEvent) error { return nil }
func (*archiveRequestTx) InsertAudit(context.Context, audit.Event) error             { return nil }
func TestArchiveRequestCannotRaceNewActiveResource(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	store := &archiveRequestStore{}
	svc := service.ArchiveService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}, MaxAttempts: 3}
	_, _, _ = svc.Request(context.Background(), service.Actor{TenantID: "tenant", UserID: "coord", RequestID: "archive"}, "program")
	if store.jobs != 0 {
		t.Fatal("archive request queued after readiness changed")
	}
}
