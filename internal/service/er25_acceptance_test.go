package service_test

import (
	"context"
	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/seminar"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"testing"
	"time"
)

type closeAgendaStore struct {
	repository.Store
	agenda seminar.Agenda
}

func (s *closeAgendaStore) GetAgenda(context.Context, string, string) (seminar.Agenda, error) {
	return s.agenda, nil
}
func (*closeAgendaStore) GetProgram(context.Context, string, string) (program.Program, error) {
	return program.Program{ID: "program", CoordinatorID: "coord"}, nil
}
func (*closeAgendaStore) ListAgendaItems(context.Context, string, string) ([]seminar.Item, error) {
	return []seminar.Item{{ID: "item", State: seminar.ItemDiscussed}}, nil
}
func (s *closeAgendaStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	return fn(ctx, &closeAgendaTx{s: s})
}

type closeAgendaTx struct {
	repository.Tx
	s *closeAgendaStore
}

func (*closeAgendaTx) ListAgendaItems(context.Context, string, string) ([]seminar.Item, error) {
	return []seminar.Item{{ID: "item", State: seminar.ItemPending}}, nil
}
func (t *closeAgendaTx) UpdateAgenda(_ context.Context, a seminar.Agenda, _ int64) error {
	t.s.agenda = a
	return nil
}
func (*closeAgendaTx) InsertOutbox(context.Context, repository.OutboxEvent) error { return nil }
func (*closeAgendaTx) InsertAudit(context.Context, audit.Event) error             { return nil }
func TestAgendaCloseCannotRaceAnUnresolvedItem(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	store := &closeAgendaStore{agenda: seminar.Agenda{ID: "agenda", TenantID: "tenant", ProgramID: "program", State: seminar.InSession, Version: 1}}
	svc := service.SeminarService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	_, _ = svc.Close(context.Background(), service.Actor{TenantID: "tenant", UserID: "coord", RequestID: "close"}, "agenda")
	if store.agenda.State == seminar.Closed {
		t.Fatal("agenda closed with pending item")
	}
}
