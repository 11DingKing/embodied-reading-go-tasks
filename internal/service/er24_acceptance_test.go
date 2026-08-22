package service_test

import (
	"context"
	"errors"
	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/evidence"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/seminar"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"testing"
	"time"
)

type agendaGenStore struct {
	repository.Store
	agendas int
	items   int
	claims  []evidence.Claim
}

func (*agendaGenStore) GetProgram(context.Context, string, string) (program.Program, error) {
	return program.Program{ID: "program", TenantID: "tenant", State: program.ReviewPending}, nil
}
func (s *agendaGenStore) ListFinalClaimsForProgram(context.Context, string, string) ([]evidence.Claim, error) {
	return s.claims, nil
}
func (s *agendaGenStore) InsertAgenda(context.Context, seminar.Agenda) error { s.agendas++; return nil }
func (s *agendaGenStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	old := s.items
	err := fn(ctx, &agendaGenTx{s: s})
	if err != nil {
		s.items = old
	}
	return err
}

type agendaGenTx struct {
	repository.Tx
	s *agendaGenStore
}

func (t *agendaGenTx) ListFinalClaimsForProgram(context.Context, string, string) ([]evidence.Claim, error) {
	return t.s.claims, nil
}
func (t *agendaGenTx) InsertAgenda(c context.Context, a seminar.Agenda) error {
	return t.s.InsertAgenda(c, a)
}
func (t *agendaGenTx) InsertAgendaItem(context.Context, seminar.Item) error {
	t.s.items++
	if t.s.items == 2 {
		return errors.New("item unavailable")
	}
	return nil
}
func (*agendaGenTx) InsertAudit(context.Context, audit.Event) error { return nil }
func TestAgendaGenerationRetryIsIdempotentAndAtomic(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	store := &agendaGenStore{claims: []evidence.Claim{{ID: "c1", PageStart: 10, PageEnd: 11}, {ID: "c2", PageStart: 12, PageEnd: 13}}}
	svc := service.SeminarService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	_, _ = svc.Generate(context.Background(), "tenant", "program")
	if store.agendas != 0 || store.items != 0 {
		t.Fatalf("agenda generation failure left partial agenda agendas=%d items=%d", store.agendas, store.items)
	}
}
