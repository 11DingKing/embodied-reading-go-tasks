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

func TestDisputeCreationFailurePreservesRejectedClaim(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "dispute.db")
	store, err := storepkg.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_ = store.EnsureTenant(ctx, "tenant", "Studio", now)
	author, _ := identity.NewUser("author", "tenant", "author@example.test", "Author", []identity.Role{identity.Researcher}, []byte("hash"), now)
	_ = store.InsertUser(ctx, author)
	work, _ := catalog.NewWork("work", "tenant", "Dream", "Cao", "reading", now)
	_ = store.InsertWork(ctx, work)
	edition, _ := catalog.NewEdition("edition", "tenant", work.ID, "Print", "Press", now.AddDate(-1, 0, 0), 1200, "physical-copy-2026", now)
	_ = edition.Activate(now)
	_ = store.InsertEdition(ctx, edition)
	value, _ := program.New("program", "tenant", edition.ID, author.ID, "Seminar", "Asia/Shanghai", 8, now.Add(-time.Hour), now.Add(24*time.Hour), now.Add(-48*time.Hour))
	_ = value.OpenEnrollment(now.Add(-47 * time.Hour))
	_ = value.Start(now)
	_ = store.InsertProgram(ctx, value)
	member, _ := program.NewMember("member", "tenant", value.ID, author.ID, now)
	_ = store.InsertMember(ctx, member)
	section, _ := reading.NewAssignment("section", "tenant", value.ID, edition.ID, member.ID, reading.PageRange{Start: 10, End: 20}, now.Add(-time.Hour), now.Add(time.Hour), now)
	_ = store.InsertAssignment(ctx, section)
	session, _ := reading.NewSession("session", "tenant", value.ID, section.ID, member.ID, "Read every line closely", now)
	_ = session.Start(now)
	_ = store.InsertReadingSession(ctx, session)
	claim, _ := evidence.NewClaim("claim", "tenant", session.ID, edition.ID, author.ID, 10, 12, "quoted words", "A substantive interpretation", now)
	_ = claim.Assign("review", now)
	_ = claim.Decide(false, "review", now)
	_ = store.InsertClaim(ctx, claim)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_dispute BEFORE INSERT ON claim_disputes BEGIN SELECT RAISE(ABORT, 'dispute unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	svc := service.ReviewService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	actor := service.Actor{TenantID: "tenant", UserID: author.ID, RequestID: "dispute"}
	if _, err := svc.OpenDispute(ctx, actor, claim.ID, "The printed punctuation supports a different reading."); err == nil {
		t.Fatal("dispute unexpectedly succeeded")
	}
	persisted, err := store.GetClaim(ctx, "tenant", claim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != evidence.ClaimRejected || persisted.CurrentReviewID != "review" {
		t.Fatalf("failed dispute changed rejected claim: %+v", persisted)
	}
}
