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

type submitStore struct {
	repository.Store
	session      reading.Session
	assignment   reading.Assignment
	member       program.Member
	edition      catalog.Edition
	observations int
	claims       int
}

func (s *submitStore) GetReadingSession(context.Context, string, string) (reading.Session, error) {
	return s.session, nil
}
func (s *submitStore) GetAssignment(context.Context, string, string) (reading.Assignment, error) {
	return s.assignment, nil
}
func (s *submitStore) GetMember(context.Context, string, string, string) (program.Member, error) {
	return s.member, nil
}
func (s *submitStore) GetEdition(context.Context, string, string) (catalog.Edition, error) {
	return s.edition, nil
}
func (s *submitStore) UpdateReadingSession(_ context.Context, v reading.Session, _ int64) error {
	s.session = v
	return nil
}
func (s *submitStore) UpdateAssignment(_ context.Context, v reading.Assignment, _ int64) error {
	s.assignment = v
	return nil
}
func (s *submitStore) InsertObservation(context.Context, reading.Observation) error {
	s.observations++
	return nil
}
func (s *submitStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	oldS, oldA, oldO, oldC := s.session, s.assignment, s.observations, s.claims
	err := fn(ctx, &submitTx{s: s})
	if err != nil {
		s.session, s.assignment, s.observations, s.claims = oldS, oldA, oldO, oldC
	}
	return err
}

type submitTx struct {
	repository.Tx
	s *submitStore
}

func (t *submitTx) UpdateReadingSession(c context.Context, v reading.Session, x int64) error {
	return t.s.UpdateReadingSession(c, v, x)
}
func (t *submitTx) UpdateAssignment(c context.Context, v reading.Assignment, x int64) error {
	return t.s.UpdateAssignment(c, v, x)
}
func (t *submitTx) InsertObservation(c context.Context, v reading.Observation) error {
	return t.s.InsertObservation(c, v)
}
func (t *submitTx) InsertClaim(context.Context, evidence.Claim) error {
	t.s.claims++
	if t.s.claims == 2 {
		return errors.New("second claim unavailable")
	}
	return nil
}
func (*submitTx) InsertOutbox(context.Context, repository.OutboxEvent) error { return nil }
func (*submitTx) InsertAudit(context.Context, audit.Event) error             { return nil }
func TestReadingSubmissionRollsBackEveryArtifactOnMidBatchFailure(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	a, _ := reading.NewAssignment("a", "tenant", "program", "edition", "member", reading.PageRange{Start: 10, End: 20}, now.Add(-time.Hour), now.Add(time.Hour), now)
	s, _ := reading.NewSession("session", "tenant", "program", a.ID, "member", "Read every line closely", now)
	_ = s.Start(now)
	_ = a.Start(s.ID, now.Add(time.Hour), now)
	store := &submitStore{session: s, assignment: a, member: program.Member{ID: "member", State: program.MemberActive}, edition: catalog.Edition{ID: "edition", State: catalog.EditionActive, PageCount: 100}}
	svc := service.ReadingService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	input := service.SubmitReadingInput{SessionID: s.ID, Observations: []service.ObservationInput{{Page: 10, Channel: reading.Visual, Body: "A detailed visual observation", Sequence: 1}}, Claims: []service.ClaimInput{{PageStart: 10, PageEnd: 10, QuotedText: "first quote", Interpretation: "first substantive interpretation"}, {PageStart: 11, PageEnd: 11, QuotedText: "second quote", Interpretation: "second substantive interpretation"}}}
	_, _ = svc.Submit(context.Background(), service.Actor{TenantID: "tenant", UserID: "user", RequestID: "submit"}, input)
	if store.session.State != reading.SessionActive || store.assignment.State != reading.AssignmentInReading || store.observations != 0 || store.claims != 0 {
		t.Fatalf("failed submission persisted session=%s assignment=%s observations=%d claims=%d", store.session.State, store.assignment.State, store.observations, store.claims)
	}
}
