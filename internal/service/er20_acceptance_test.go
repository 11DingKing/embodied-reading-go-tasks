package service_test

import (
	"context"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/review"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"sync"
	"testing"
	"time"
)

type reviewClaimStore struct {
	repository.Store
	mu       sync.Mutex
	barrier  sync.Mutex
	arrivals int
	release  chan struct{}
	value    review.Assignment
	wins     int
}

func (s *reviewClaimStore) GetUser(context.Context, string, string) (identity.User, error) {
	return identity.User{ID: "reviewer", TenantID: "tenant", Active: true, Roles: []identity.Role{identity.Reviewer}}, nil
}
func (s *reviewClaimStore) GetReviewAssignment(context.Context, string, string) (review.Assignment, error) {
	s.mu.Lock()
	snapshot := s.value
	s.mu.Unlock()
	s.barrier.Lock()
	s.arrivals++
	if s.arrivals == 2 {
		close(s.release)
	}
	s.barrier.Unlock()
	<-s.release
	return snapshot, nil
}
func (s *reviewClaimStore) UpdateReviewAssignment(_ context.Context, v review.Assignment, expected int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected == v.Version {
		s.value = v
		s.wins++
		return nil
	}
	if expected != s.value.Version {
		return fault.New(fault.Conflict, "version_conflict", "lost")
	}
	s.value = v
	s.wins++
	return nil
}
func TestConcurrentReviewClaimHasExactlyOneLeaseOwner(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	a, _ := review.NewAssignment("review", "tenant", "claim", "author", "reviewer", now)
	store := &reviewClaimStore{value: a, release: make(chan struct{})}
	svc := service.ReviewService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}, LeaseTTL: time.Hour}
	actor := service.Actor{TenantID: "tenant", UserID: "reviewer", RequestID: "claim"}
	var wg sync.WaitGroup
	wg.Add(2)
	for _, workerID := range []string{"worker-a", "worker-b"} {
		go func(id string) { defer wg.Done(); _, _ = svc.Claim(context.Background(), actor, a.ID, id) }(workerID)
	}
	wg.Wait()
	if store.wins != 1 {
		t.Fatalf("concurrent review claims produced %d lease winners", store.wins)
	}
}
