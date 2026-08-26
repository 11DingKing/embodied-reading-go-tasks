package service_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/evidence"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	storepkg "github.com/11DingKing/embodied-reading-studio/internal/storage/sqlite"
	_ "modernc.org/sqlite"
)

func TestReviewAssignmentFailureLeavesNoOrphanTask(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "assign-review.db")
	store, err := storepkg.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_ = store.EnsureTenant(ctx, "tenant", "Studio", now)
	coord, _ := identity.NewUser("coord", "tenant", "coord@example.test", "Coordinator", []identity.Role{identity.Coordinator}, []byte("hash"), now)
	author, _ := identity.NewUser("author", "tenant", "author@example.test", "Author", []identity.Role{identity.Researcher}, []byte("hash"), now)
	reviewer, _ := identity.NewUser("reviewer", "tenant", "reviewer@example.test", "Reviewer", []identity.Role{identity.Reviewer}, []byte("hash"), now)
	_ = store.InsertUser(ctx, coord)
	_ = store.InsertUser(ctx, author)
	_ = store.InsertUser(ctx, reviewer)
	work, _ := catalog.NewWork("work", "tenant", "Dream", "Cao", "reading", now)
	_ = store.InsertWork(ctx, work)
	edition, _ := catalog.NewEdition("edition", "tenant", work.ID, "Print", "Press", now.AddDate(-1, 0, 0), 1200, "physical-copy-2026", now)
	_ = edition.Activate(now)
	_ = store.InsertEdition(ctx, edition)
	value, _ := program.New("program", "tenant", edition.ID, coord.ID, "Seminar", "Asia/Shanghai", 8, now.Add(-time.Hour), now.Add(24*time.Hour), now.Add(-48*time.Hour))
	_ = value.OpenEnrollment(now.Add(-47 * time.Hour))
	_ = value.Start(now)
	_ = store.InsertProgram(ctx, value)
	member, _ := program.NewMember("member", "tenant", value.ID, author.ID, now)
	_ = store.InsertMember(ctx, member)
	assignment, _ := reading.NewAssignment("section", "tenant", value.ID, edition.ID, member.ID, reading.PageRange{Start: 10, End: 20}, now.Add(-time.Hour), now.Add(time.Hour), now)
	_ = store.InsertAssignment(ctx, assignment)
	session, _ := reading.NewSession("session", "tenant", value.ID, assignment.ID, member.ID, "Read every line closely", now)
	_ = session.Start(now)
	_ = store.InsertReadingSession(ctx, session)
	claim, _ := evidence.NewClaim("claim", "tenant", session.ID, edition.ID, author.ID, 10, 12, "quoted words", "A substantive interpretation", now)
	_ = store.InsertClaim(ctx, claim)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_claim_assignment BEFORE UPDATE ON quotation_claims BEGIN SELECT RAISE(ABORT, 'claim unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	svc := service.ReviewService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	actor := service.Actor{TenantID: "tenant", UserID: coord.ID, RequestID: "assign"}
	if _, err := svc.Assign(ctx, actor, claim.ID, reviewer.ID); err == nil {
		t.Fatal("assignment unexpectedly succeeded")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM review_assignments`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed assignment left %d orphan review tasks", count)
	}
}
