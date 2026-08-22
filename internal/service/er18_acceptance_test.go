package service_test

import (
	"context"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"testing"
	"time"
)

type expiryStore struct {
	repository.Store
	stale, current reading.Assignment
}

func (s *expiryStore) ListExpiredAssignments(context.Context, time.Time, repository.Page) ([]reading.Assignment, error) {
	return []reading.Assignment{s.stale}, nil
}
func (s *expiryStore) UpdateAssignment(_ context.Context, v reading.Assignment, expected int64) error {
	if expected != s.current.Version {
		return fault.New(fault.Conflict, "version_conflict", "renewed")
	}
	s.current = v
	return nil
}
func TestExpiryWorkerCannotRevokeRenewedAssignment(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	stale, _ := reading.NewAssignment("a", "tenant", "program", "edition", "member", reading.PageRange{Start: 10, End: 20}, now.Add(-2*time.Hour), now.Add(-time.Hour), now.Add(-3*time.Hour))
	current := stale
	current.Version = 2
	current.LeaseToken = 2
	current.WindowEnd = now.Add(time.Hour)
	store := &expiryStore{stale: stale, current: current}
	svc := service.ReadingService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	_, _ = svc.ExpireAssignments(context.Background(), 10)
	if store.current.State == reading.AssignmentExpired {
		t.Fatal("expiry worker overwrote renewed lease")
	}
}
