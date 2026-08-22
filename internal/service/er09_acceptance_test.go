package service_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	storepkg "github.com/11DingKing/embodied-reading-studio/internal/storage/sqlite"
	_ "modernc.org/sqlite"
)

func TestAtomicMemberBatchRollsBackEarlierItems(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "batch.db")
	store, err := storepkg.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	deps := service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}
	auth := service.AuthService{Dependencies: deps}
	coordinator, err := auth.BootstrapCoordinator(ctx, "tenant", "Studio", "coord@example.test", "a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	actor := service.Actor{TenantID: "tenant", UserID: coordinator.ID, RequestID: "setup"}
	register := func(email string) string {
		user, err := auth.Register(ctx, actor, service.RegisterUserInput{TenantID: "tenant", Email: email, DisplayName: email, Password: "another-long-password", Roles: []identity.Role{identity.Researcher}})
		if err != nil {
			t.Fatal(err)
		}
		return user.ID
	}
	first, second := register("first@example.test"), register("second@example.test")
	catalog := service.CatalogService{Dependencies: deps}
	edition, err := catalog.RegisterEdition(ctx, actor, service.RegisterEditionInput{WorkTitle: "Dream", WorkAuthor: "Cao", WorkDescription: "reading", EditionLabel: "Print", Publisher: "Press", PublishedAt: now.AddDate(-1, 0, 0), PageCount: 1200, Fingerprint: "copy"})
	if err != nil {
		t.Fatal(err)
	}
	programs := service.ProgramService{Dependencies: deps}
	value, err := programs.Create(ctx, actor, service.CreateProgramInput{EditionID: edition.Edition.ID, Name: "Seminar", TimeZone: "Asia/Shanghai", Capacity: 8, StartsAt: now.Add(time.Hour), EndsAt: now.Add(48 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := programs.OpenEnrollment(ctx, actor, value.ID); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_second_member BEFORE INSERT ON program_members WHEN NEW.user_id=? BEGIN SELECT RAISE(ABORT, 'member unavailable'); END`, second); err != nil {
		t.Fatal(err)
	}
	actor.RequestID = "batch-add"
	if _, err := programs.AddMembersAtomically(ctx, actor, value.ID, []string{first, second}); err == nil {
		t.Fatal("batch unexpectedly succeeded")
	}
	count, err := store.CountActiveMembers(ctx, "tenant", value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("failed batch left %d active members, want coordinator only", count)
	}
}
