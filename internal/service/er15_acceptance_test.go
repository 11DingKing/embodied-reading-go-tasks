package service_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	storepkg "github.com/11DingKing/embodied-reading-studio/internal/storage/sqlite"
	_ "modernc.org/sqlite"
)

func TestAbandonFailureCannotSplitSessionAndAssignmentState(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "abandon.db")
	store, err := storepkg.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_ = store.EnsureTenant(ctx, "tenant", "Studio", now)
	user, _ := identity.NewUser("user", "tenant", "reader@example.test", "Reader", []identity.Role{identity.Researcher}, []byte("hash"), now)
	_ = store.InsertUser(ctx, user)
	work, _ := catalog.NewWork("work", "tenant", "Dream", "Cao", "reading", now)
	_ = store.InsertWork(ctx, work)
	edition, _ := catalog.NewEdition("edition", "tenant", work.ID, "Print", "Press", now.AddDate(-1, 0, 0), 1200, "copy", now)
	_ = edition.Activate(now)
	_ = store.InsertEdition(ctx, edition)
	value, _ := program.New("program", "tenant", edition.ID, user.ID, "Seminar", "Asia/Shanghai", 8, now.Add(-time.Hour), now.Add(24*time.Hour), now.Add(-48*time.Hour))
	_ = value.OpenEnrollment(now.Add(-47 * time.Hour))
	_ = value.Start(now)
	_ = store.InsertProgram(ctx, value)
	member, _ := program.NewMember("member", "tenant", value.ID, user.ID, now)
	_ = store.InsertMember(ctx, member)
	assignment, _ := reading.NewAssignment("assignment", "tenant", value.ID, edition.ID, member.ID, reading.PageRange{Start: 10, End: 20}, now.Add(-time.Hour), now.Add(time.Hour), now)
	session, _ := reading.NewSession("session", "tenant", value.ID, assignment.ID, member.ID, "Read every line closely", now)
	_ = session.Start(now)
	_ = assignment.Start(session.ID, now.Add(time.Hour), now)
	_ = store.InsertAssignment(ctx, assignment)
	_ = store.InsertReadingSession(ctx, session)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_abandon_audit BEFORE INSERT ON audit_events WHEN NEW.action='reading.abandon' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	svc := service.ReadingService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	actor := service.Actor{TenantID: "tenant", UserID: user.ID, RequestID: "abandon"}
	if err := svc.Abandon(ctx, actor, session.ID, "room was unexpectedly closed"); err == nil {
		t.Fatal("abandon unexpectedly succeeded")
	}
	persistedSession, err := store.GetReadingSession(ctx, "tenant", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	persistedAssignment, err := store.GetAssignment(ctx, "tenant", assignment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persistedSession.State != reading.SessionActive || persistedAssignment.State != reading.AssignmentInReading {
		t.Fatalf("failed abandon split states: session=%s assignment=%s", persistedSession.State, persistedAssignment.State)
	}
}
