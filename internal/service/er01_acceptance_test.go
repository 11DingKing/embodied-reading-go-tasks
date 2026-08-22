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

func TestLoginAuditFailureLeavesNoUsableSession(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "login.db")
	store, err := storepkg.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	auth := service.AuthService{
		Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)},
		SessionTTL:   12 * time.Hour, TouchInterval: 5 * time.Minute,
	}
	if _, err := auth.BootstrapCoordinator(ctx, "tenant", "Reading Studio", "coordinator@example.test", "a-long-test-password"); err != nil {
		t.Fatal(err)
	}
	inspector, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer inspector.Close()
	if _, err := inspector.Exec(`CREATE TRIGGER reject_login_audit BEFORE INSERT ON audit_events
		WHEN NEW.action = 'session.login' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	input := service.LoginInput{TenantID: "tenant", Email: "coordinator@example.test", Password: "a-long-test-password"}
	if _, err := auth.Login(ctx, "request-failed", input); err == nil {
		t.Fatal("login unexpectedly succeeded while audit persistence failed")
	}
	var sessions int
	if err := inspector.QueryRow(`SELECT COUNT(*) FROM auth_sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("failed login left %d usable session records", sessions)
	}
	if _, err := inspector.Exec(`DROP TRIGGER reject_login_audit`); err != nil {
		t.Fatal(err)
	}
	result, err := auth.Login(ctx, "request-success", input)
	if err != nil {
		t.Fatalf("valid login failed after dependency recovered: %v", err)
	}
	if result.Token == "" {
		t.Fatal("valid login returned an empty token")
	}
	if _, _, err := auth.Authenticate(ctx, result.Token); err != nil {
		t.Fatalf("successful login token was not usable: %v", err)
	}
}
