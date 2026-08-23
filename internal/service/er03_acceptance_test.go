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

func TestDeactivateUserRollsBackUserAndAllSessions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deactivate.db")
	store, err := storepkg.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	auth := service.AuthService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}, SessionTTL: time.Hour}
	coordinator, err := auth.BootstrapCoordinator(ctx, "tenant", "Studio", "coordinator@example.test", "a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	actor := service.Actor{TenantID: "tenant", UserID: coordinator.ID, RequestID: "register"}
	reader, err := auth.Register(ctx, actor, service.RegisterUserInput{TenantID: "tenant", Email: "reader@example.test", DisplayName: "Reader", Password: "another-long-password", Roles: []identity.Role{identity.Researcher}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := auth.Login(ctx, "login", service.LoginInput{TenantID: "tenant", Email: reader.Email, Password: "another-long-password"}); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_deactivate_audit BEFORE INSERT ON audit_events WHEN NEW.action='user.deactivate' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	actor.RequestID = "deactivate"
	if err := auth.DeactivateUser(ctx, actor, reader.ID); err == nil {
		t.Fatal("deactivation unexpectedly succeeded")
	}
	persisted, err := store.GetUser(ctx, "tenant", reader.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.Active {
		t.Fatal("failed deactivation left user inactive")
	}
	var active int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_sessions WHERE user_id=? AND state='active'`, reader.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 2 {
		t.Fatalf("active sessions = %d, want 2", active)
	}
}
