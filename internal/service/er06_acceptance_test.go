package service_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	storepkg "github.com/11DingKing/embodied-reading-studio/internal/storage/sqlite"
	_ "modernc.org/sqlite"
)

func TestProgramCreationRollsBackWhenCoordinatorMembershipFails(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "program.db")
	store, err := storepkg.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	deps := service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}
	auth := service.AuthService{Dependencies: deps}
	user, err := auth.BootstrapCoordinator(ctx, "tenant", "Studio", "coordinator@example.test", "a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	actor := service.Actor{TenantID: "tenant", UserID: user.ID, RequestID: "setup"}
	catalog := service.CatalogService{Dependencies: deps}
	edition, err := catalog.RegisterEdition(ctx, actor, service.RegisterEditionInput{WorkTitle: "Dream", WorkAuthor: "Cao", WorkDescription: "reading", EditionLabel: "Print", Publisher: "Press", PublishedAt: now.AddDate(-1, 0, 0), PageCount: 1200, Fingerprint: "physical-copy-2026"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_initial_member BEFORE INSERT ON program_members BEGIN SELECT RAISE(ABORT, 'membership unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	actor.RequestID = "create-program"
	programs := service.ProgramService{Dependencies: deps}
	input := service.CreateProgramInput{EditionID: edition.Edition.ID, Name: "Chapter 77 Seminar", TimeZone: "Asia/Shanghai", Capacity: 8, StartsAt: now.Add(time.Hour), EndsAt: now.Add(48 * time.Hour)}
	if _, err := programs.Create(ctx, actor, input); err == nil {
		t.Fatal("creation unexpectedly succeeded")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM study_programs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed creation left %d empty programs", count)
	}
}
