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

func TestEditionRegistrationFailureLeavesNoOrphanWork(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.db")
	store, err := storepkg.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	auth := service.AuthService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	user, err := auth.BootstrapCoordinator(ctx, "tenant", "Studio", "coordinator@example.test", "a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_edition BEFORE INSERT ON editions BEGIN SELECT RAISE(ABORT, 'edition unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	catalog := service.CatalogService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(100)}}
	input := service.RegisterEditionInput{WorkTitle: "Dream of the Red Chamber", WorkAuthor: "Cao Xueqin", WorkDescription: "close reading", EditionLabel: "Annotated", Publisher: "Reader Press", PublishedAt: now.AddDate(-1, 0, 0), PageCount: 1200, Fingerprint: "physical-copy-1"}
	actor := service.Actor{TenantID: "tenant", UserID: user.ID, RequestID: "edition"}
	if _, err := catalog.RegisterEdition(ctx, actor, input); err == nil {
		t.Fatal("registration unexpectedly succeeded")
	}
	var works int
	if err := db.QueryRow(`SELECT COUNT(*) FROM works`).Scan(&works); err != nil {
		t.Fatal(err)
	}
	if works != 0 {
		t.Fatalf("failed registration left %d orphan works", works)
	}
}
