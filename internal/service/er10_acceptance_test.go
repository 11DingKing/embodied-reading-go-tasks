package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
)

type readinessStore struct {
	repository.Store
	mu    sync.Mutex
	value program.Program
}

func (s *readinessStore) GetProgram(context.Context, string, string) (program.Program, error) {
	return s.value, nil
}
func (*readinessStore) GetProgramReadiness(context.Context, string, string, time.Time) (repository.ProgramReadiness, error) {
	return repository.ProgramReadiness{ActiveMembers: 1}, nil
}
func (s *readinessStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(ctx, &readinessTx{s: s})
}

type readinessTx struct {
	repository.Tx
	s *readinessStore
}

func (*readinessTx) GetProgramReadiness(context.Context, string, string, time.Time) (repository.ProgramReadiness, error) {
	return repository.ProgramReadiness{ActiveMembers: 1, ActiveAssignments: 1}, nil
}
func (t *readinessTx) UpdateProgram(_ context.Context, p program.Program, _ int64) error {
	t.s.value = p
	return nil
}
func (*readinessTx) InsertOutbox(context.Context, repository.OutboxEvent) error { return nil }
func (*readinessTx) InsertAudit(context.Context, audit.Event) error             { return nil }
func TestReviewRequestCannotRaceNewReadingActivity(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	store := &readinessStore{value: program.Program{ID: "program", TenantID: "tenant", CoordinatorID: "coord", State: program.Active, Version: 1}}
	svc := service.ProgramService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	_, _, err := svc.RequestReview(context.Background(), service.Actor{TenantID: "tenant", UserID: "coord", RequestID: "review"}, "program")
	if err == nil || store.value.State == program.ReviewPending {
		t.Fatal("review transition ignored changed readiness")
	}
}
