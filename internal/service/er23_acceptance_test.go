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

func TestDisputeResolutionVersionConflictRollsBackBothAggregates(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "resolve.db")
	store, err := storepkg.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_ = store.EnsureTenant(ctx, "tenant", "Studio", now)
	author, _ := identity.NewUser("author", "tenant", "author@example.test", "Author", []identity.Role{identity.Researcher}, []byte("hash"), now)
	resolver, _ := identity.NewUser("resolver", "tenant", "resolver@example.test", "Resolver", []identity.Role{identity.Reviewer}, []byte("hash"), now)
	_ = store.InsertUser(ctx, author)
	_ = store.InsertUser(ctx, resolver)
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
	_ = claim.Assign("old-review", now)
	_ = claim.Decide(false, "old-review", now)
	_ = claim.Dispute(now)
	_ = store.InsertClaim(ctx, claim)
	dispute, _ := evidence.NewDispute("dispute", "tenant", claim.ID, author.ID, "The physical text supports the original claim.", now)
	_ = dispute.Assign(resolver.ID)
	_ = store.InsertDispute(ctx, dispute)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_claim_resolution BEFORE UPDATE ON quotation_claims BEGIN SELECT RAISE(ABORT, 'version conflict'); END`); err != nil {
		t.Fatal(err)
	}
	svc := service.ReviewService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	actor := service.Actor{TenantID: "tenant", UserID: resolver.ID, RequestID: "resolve"}
	input := service.ResolveDisputeInput{DisputeID: dispute.ID, ReviewID: "new-review", ResolverID: resolver.ID, Uphold: true, Resolution: "The physical edition confirms the disputed quotation."}
	if _, _, err := svc.ResolveDispute(ctx, actor, input); err == nil {
		t.Fatal("resolution unexpectedly succeeded")
	}
	persisted, err := store.GetDispute(ctx, "tenant", dispute.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != evidence.DisputeAssigned {
		t.Fatalf("failed resolution committed dispute state %s", persisted.State)
	}
}
