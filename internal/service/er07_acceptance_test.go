package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
)

type capacityStore struct {
	repository.Store
	program   program.Program
	users     map[string]identity.User
	mu        sync.Mutex
	barrierMu sync.Mutex
	arrivals  int
	release   chan struct{}
	members   []program.Member
}

func (s *capacityStore) GetUser(_ context.Context, _, id string) (identity.User, error) {
	return s.users[id], nil
}
func (s *capacityStore) CountActiveMembers(context.Context, string, string) (int, error) {
	s.mu.Lock()
	snapshot := len(s.members)
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
func (s *capacityStore) GetMember(_ context.Context, _, _, userID string) (program.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.members {
		if m.UserID == userID {
			return m, nil
		}
	}
	return program.Member{}, fault.Missing("member", userID)
}
func (s *capacityStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(ctx, &capacityTx{s: s})
}

type capacityTx struct {
	repository.Tx
	s *capacityStore
}

func (t *capacityTx) GetIdempotency(context.Context, string, string, string, string, string) (repository.IdempotencyRecord, error) {
	return repository.IdempotencyRecord{}, fault.Missing("idempotency", "key")
}
func (t *capacityTx) GetProgram(context.Context, string, string) (program.Program, error) {
	return t.s.program, nil
}
func (t *capacityTx) InsertMember(_ context.Context, m program.Member) error {
	t.s.members = append(t.s.members, m)
	return nil
}
func (*capacityTx) InsertIdempotency(context.Context, repository.IdempotencyRecord) error { return nil }
func (*capacityTx) InsertAudit(context.Context, audit.Event) error                        { return nil }
func (t *capacityTx) GetMember(_ context.Context, _, _, userID string) (program.Member, error) {
	for _, m := range t.s.members {
		if m.UserID == userID {
			return m, nil
		}
	}
	return program.Member{}, fault.Missing("member", userID)
}

func TestConcurrentJoinNeverExceedsProgramCapacity(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	p := program.Program{ID: "program", TenantID: "tenant", State: program.Enrolling, Capacity: 2, StartsAt: now.Add(time.Hour)}
	users := map[string]identity.User{}
	for _, id := range []string{"first", "second"} {
		users[id] = identity.User{ID: id, TenantID: "tenant", Active: true, Roles: []identity.Role{identity.Researcher}}
	}
	store := &capacityStore{program: p, users: users, release: make(chan struct{}), members: []program.Member{{ID: "existing", State: program.MemberActive}}}
	svc := service.ProgramService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	var wg sync.WaitGroup
	wg.Add(2)
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, id := range []string{"first", "second"} {
		go func(id string) {
			defer wg.Done()
			if _, err := svc.Join(context.Background(), service.Actor{TenantID: "tenant", UserID: id, RequestID: id}, p.ID, "key-"+id, hash); err != nil {
				t.Errorf("join %s: %v", id, err)
			}
		}(id)
	}
	wg.Wait()
	store.mu.Lock()
	count := len(store.members)
	store.mu.Unlock()
	if count > p.Capacity {
		t.Fatalf("concurrent joins exceeded capacity: got %d members for capacity %d", count, p.Capacity)
	}
}
