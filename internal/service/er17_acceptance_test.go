package service_test

import (
	"context"
	"errors"
	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/evidence"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"testing"
	"time"
)

type outboxSubmitStore struct {
	repository.Store
	session    reading.Session
	assignment reading.Assignment
	member     program.Member
	edition    catalog.Edition
	claims     int
}

func (s *outboxSubmitStore) GetReadingSession(context.Context, string, string) (reading.Session, error) {
	return s.session, nil
}
func (s *outboxSubmitStore) GetAssignment(context.Context, string, string) (reading.Assignment, error) {
	return s.assignment, nil
}
func (s *outboxSubmitStore) GetMember(context.Context, string, string, string) (program.Member, error) {
	return s.member, nil
}
func (s *outboxSubmitStore) GetEdition(context.Context, string, string) (catalog.Edition, error) {
	return s.edition, nil
}
func (s *outboxSubmitStore) InsertOutbox(context.Context, repository.OutboxEvent) error {
	return errors.New("outbox unavailable")
}
func (s *outboxSubmitStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	oldS, oldA, oldC := s.session, s.assignment, s.claims
	err := fn(ctx, &outboxSubmitTx{s: s})
	if err != nil {
		s.session, s.assignment, s.claims = oldS, oldA, oldC
	}
	return err
}

type outboxSubmitTx struct {
	repository.Tx
	s *outboxSubmitStore
}

func (t *outboxSubmitTx) UpdateReadingSession(_ context.Context, v reading.Session, _ int64) error {
	t.s.session = v
	return nil
}
func (t *outboxSubmitTx) UpdateAssignment(_ context.Context, v reading.Assignment, _ int64) error {
	t.s.assignment = v
	return nil
}
func (*outboxSubmitTx) InsertObservation(context.Context, reading.Observation) error { return nil }
func (t *outboxSubmitTx) InsertClaim(context.Context, evidence.Claim) error          { t.s.claims++; return nil }
func (*outboxSubmitTx) InsertAudit(context.Context, audit.Event) error               { return nil }
func (*outboxSubmitTx) InsertOutbox(context.Context, repository.OutboxEvent) error {
	return errors.New("outbox unavailable")
}
func TestClaimOutboxFailureRollsBackReadingSubmission(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	a, _ := reading.NewAssignment("a", "tenant", "program", "edition", "member", reading.PageRange{Start: 10, End: 20}, now.Add(-time.Hour), now.Add(time.Hour), now)
	s, _ := reading.NewSession("session", "tenant", "program", a.ID, "member", "Read every line closely", now)
	_ = s.Start(now)
	_ = a.Start(s.ID, now.Add(time.Hour), now)
	store := &outboxSubmitStore{session: s, assignment: a, member: program.Member{ID: "member", State: program.MemberActive}, edition: catalog.Edition{ID: "edition", State: catalog.EditionActive, PageCount: 100}}
	svc := service.ReadingService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	input := service.SubmitReadingInput{SessionID: s.ID, Observations: []service.ObservationInput{{Page: 10, Channel: reading.Visual, Body: "A detailed visual observation", Sequence: 1}}, Claims: []service.ClaimInput{{PageStart: 10, PageEnd: 10, QuotedText: "first quote", Interpretation: "first substantive interpretation"}}}
	_, _ = svc.Submit(context.Background(), service.Actor{TenantID: "tenant", UserID: "user", RequestID: "submit"}, input)
	if store.session.State != reading.SessionActive || store.claims != 0 {
		t.Fatalf("outbox failure left submitted reading state=%s claims=%d", store.session.State, store.claims)
	}
}
