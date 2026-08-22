package service_test

import (
	"context"
	"errors"
	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"testing"
	"time"
)

type poisonStore struct {
	repository.Store
	value  program.Program
	user   identity.User
	idem   *repository.IdempotencyRecord
	member *program.Member
}

func (s *poisonStore) GetUser(context.Context, string, string) (identity.User, error) {
	return s.user, nil
}
func (s *poisonStore) InsertIdempotency(_ context.Context, r repository.IdempotencyRecord) error {
	s.idem = &r
	return nil
}
func (s *poisonStore) GetMember(context.Context, string, string, string) (program.Member, error) {
	if s.member == nil {
		return program.Member{}, fault.Missing("member", "user")
	}
	return *s.member, nil
}
func (s *poisonStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	oldI, oldM := s.idem, s.member
	err := fn(ctx, &poisonTx{s: s})
	if err != nil {
		s.idem, s.member = oldI, oldM
	}
	return err
}

type poisonTx struct {
	repository.Tx
	s *poisonStore
}

func (t *poisonTx) GetIdempotency(context.Context, string, string, string, string, string) (repository.IdempotencyRecord, error) {
	if t.s.idem == nil {
		return repository.IdempotencyRecord{}, fault.Missing("idempotency", "key")
	}
	return *t.s.idem, nil
}
func (t *poisonTx) GetMember(c context.Context, a, b, d string) (program.Member, error) {
	return t.s.GetMember(c, a, b, d)
}
func (t *poisonTx) GetProgram(context.Context, string, string) (program.Program, error) {
	return t.s.value, nil
}
func (*poisonTx) CountActiveMembers(context.Context, string, string) (int, error) { return 0, nil }
func (t *poisonTx) InsertMember(_ context.Context, m program.Member) error {
	t.s.member = &m
	return nil
}
func (t *poisonTx) InsertIdempotency(c context.Context, r repository.IdempotencyRecord) error {
	return t.s.InsertIdempotency(c, r)
}
func (*poisonTx) InsertAudit(context.Context, audit.Event) error {
	return errors.New("audit unavailable")
}
func TestFailedJoinDoesNotPoisonIdempotencyKey(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	store := &poisonStore{value: program.Program{ID: "program", TenantID: "tenant", State: program.Enrolling, Capacity: 2, StartsAt: now.Add(time.Hour)}, user: identity.User{ID: "user", TenantID: "tenant", Active: true, Roles: []identity.Role{identity.Researcher}}}
	svc := service.ProgramService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	actor := service.Actor{TenantID: "tenant", UserID: "user", RequestID: "join"}
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	_, first := svc.Join(context.Background(), actor, "program", "same-key", hash)
	_, second := svc.Join(context.Background(), actor, "program", "same-key", hash)
	if first == nil || second == nil || store.idem != nil {
		t.Fatal("failed join poisoned idempotency key")
	}
}
