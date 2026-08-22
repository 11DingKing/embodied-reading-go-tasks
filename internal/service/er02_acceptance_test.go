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

func TestLogoutAuditFailurePreservesActiveSession(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "logout.db")
	store, err := storepkg.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	auth := service.AuthService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}, SessionTTL: time.Hour}
	user, err := auth.BootstrapCoordinator(ctx, "tenant", "Studio", "reader@example.test", "a-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	login, err := auth.Login(ctx, "login-request", service.LoginInput{TenantID: "tenant", Email: user.Email, Password: "a-long-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_logout_audit BEFORE INSERT ON audit_events WHEN NEW.action='session.logout' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	actor := service.Actor{TenantID: "tenant", UserID: user.ID, RequestID: "logout-request"}
	if err := auth.Logout(ctx, actor, login.Token); err == nil {
		t.Fatal("logout unexpectedly succeeded")
	}
	if _, _, err := auth.Authenticate(ctx, login.Token); err != nil {
		t.Fatalf("failed logout revoked the session: %v", err)
	}
}
