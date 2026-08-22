package service_test

import (
	"context"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/archive"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"testing"
	"time"
)

type cancelledArchiveStore struct {
	repository.Store
	job     archive.Job
	cancel  context.CancelFunc
	updates int
}

func (s *cancelledArchiveStore) GetArchiveJob(context.Context, string, string) (archive.Job, error) {
	return s.job, nil
}
func (s *cancelledArchiveStore) UpdateArchiveJob(ctx context.Context, j archive.Job, _ int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.job = j
	s.updates++
	if s.updates == 1 {
		s.cancel()
	}
	return nil
}
func TestCancelledArchiveWorkerPersistsRetryableJobBeforeExit(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	job, _ := archive.NewJob("job", "tenant", "program", 3, now)
	ctx, cancel := context.WithCancel(context.Background())
	store := &cancelledArchiveStore{job: job, cancel: cancel}
	svc := service.ArchiveService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}, LeaseTTL: time.Hour, MaxAttempts: 3}
	_ = svc.Process(ctx, "tenant", "job", "worker")
	if store.job.State == archive.Leased {
		t.Fatal("cancelled archive remained leased")
	}
}
