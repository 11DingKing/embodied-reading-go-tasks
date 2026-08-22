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

func TestReadingStartFailureDoesNotLeakAssignmentLease(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "start.db")
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
	_ = store.InsertAssignment(ctx, assignment)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_reading_session BEFORE INSERT ON reading_sessions BEGIN SELECT RAISE(ABORT, 'session unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	svc := service.ReadingService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}, SessionLeaseTTL: time.Hour}
	actor := service.Actor{TenantID: "tenant", UserID: user.ID, RequestID: "start"}
	if _, err := svc.Start(ctx, actor, service.StartReadingInput{AssignmentID: assignment.ID, Intention: "Read every line closely"}); err == nil {
		t.Fatal("start unexpectedly succeeded")
	}
	persisted, err := store.GetAssignment(ctx, "tenant", assignment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != reading.AssignmentReserved || persisted.LeaseOwner != "" {
		t.Fatalf("failed start leaked lease: %+v", persisted)
	}
}
