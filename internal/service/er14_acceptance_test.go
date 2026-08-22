package service_test

import (
	"context"
	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"testing"
	"time"
)

type cancelStartStore struct {
	repository.Store
	assignment reading.Assignment
	member     program.Member
	sessions   int
}

func (s *cancelStartStore) GetAssignment(context.Context, string, string) (reading.Assignment, error) {
	return s.assignment, nil
}
func (s *cancelStartStore) GetMember(context.Context, string, string, string) (program.Member, error) {
	return s.member, nil
}
func (s *cancelStartStore) WithinTx(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(ctx, &cancelStartTx{s: s})
}

type cancelStartTx struct {
	repository.Tx
	s *cancelStartStore
}

func (t *cancelStartTx) GetAssignment(context.Context, string, string) (reading.Assignment, error) {
	return t.s.assignment, nil
}
func (t *cancelStartTx) UpdateAssignment(_ context.Context, a reading.Assignment, _ int64) error {
	t.s.assignment = a
	return nil
}
func (t *cancelStartTx) InsertReadingSession(context.Context, reading.Session) error {
	t.s.sessions++
	return nil
}
func (*cancelStartTx) InsertAudit(context.Context, audit.Event) error { return nil }
func TestCancelledReadingStartLeavesNoPersistentResources(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	a, _ := reading.NewAssignment("assignment", "tenant", "program", "edition", "member", reading.PageRange{Start: 10, End: 20}, now.Add(-time.Hour), now.Add(time.Hour), now)
	store := &cancelStartStore{assignment: a, member: program.Member{ID: "member", State: program.MemberActive}}
	svc := service.ReadingService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}, SessionLeaseTTL: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = svc.Start(ctx, service.Actor{TenantID: "tenant", UserID: "user", RequestID: "start"}, service.StartReadingInput{AssignmentID: a.ID, Intention: "Read every line closely"})
	if store.sessions != 0 || store.assignment.State != reading.AssignmentReserved {
		t.Fatal("cancelled start persisted resources")
	}
}
