package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
)

type reservationStore struct {
	repository.Store
	mu          sync.Mutex
	barrierMu   sync.Mutex
	arrivals    int
	release     chan struct{}
	assignments []reading.Assignment
	value       program.Program
	members     map[string]program.Member
	edition     catalog.Edition
}

func (s *reservationStore) GetProgram(context.Context, string, string) (program.Program, error) {
	return s.value, nil
}
func (s *reservationStore) GetMember(_ context.Context, _, _, user string) (program.Member, error) {
	return s.members[user], nil
}
func (s *reservationStore) GetEdition(context.Context, string, string) (catalog.Edition, error) {
	return s.edition, nil
}
func (s *reservationStore) FindAssignmentConflicts(context.Context, string, string, string, reading.PageRange, time.Time, time.Time, string) ([]reading.Assignment, error) {
	s.mu.Lock()
	snapshot := append([]reading.Assignment(nil), s.assignments...)
	s.mu.Unlock()
	s.barrierMu.Lock()
	s.arrivals++
	if s.arrivals == 2 {
		close(s.release)
	}
	s.barrierMu.Unlock()
	<-s.release
	return snapshot, nil
}
func (s *reservationStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(ctx, &reservationTx{s: s})
}

type reservationTx struct {
	repository.Tx
	s *reservationStore
}

func (t *reservationTx) InsertAssignment(_ context.Context, a reading.Assignment) error {
	t.s.assignments = append(t.s.assignments, a)
	return nil
}
func (*reservationTx) InsertAudit(context.Context, audit.Event) error { return nil }
func TestConcurrentOverlappingReservationsHaveSingleWinner(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	store := &reservationStore{release: make(chan struct{}), value: program.Program{ID: "program", TenantID: "tenant", EditionID: "edition", State: program.Active, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(24 * time.Hour)}, edition: catalog.Edition{ID: "edition", TenantID: "tenant", State: catalog.EditionActive, PageCount: 1200}, members: map[string]program.Member{"first": {ID: "m1", State: program.MemberActive}, "second": {ID: "m2", State: program.MemberActive}}}
	svc := service.ReadingService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	input := service.ReserveInput{ProgramID: "program", Pages: reading.PageRange{Start: 10, End: 20}, StartsAt: now, EndsAt: now.Add(time.Hour)}
	var wg sync.WaitGroup
	wg.Add(2)
	for _, id := range []string{"first", "second"} {
		go func(id string) {
			defer wg.Done()
			_, _ = svc.Reserve(context.Background(), service.Actor{TenantID: "tenant", UserID: id, RequestID: id}, input)
		}(id)
	}
	wg.Wait()
	store.mu.Lock()
	count := len(store.assignments)
	store.mu.Unlock()
	if count != 1 {
		t.Fatalf("concurrent reservations produced %d owners for one page range", count)
	}
}
