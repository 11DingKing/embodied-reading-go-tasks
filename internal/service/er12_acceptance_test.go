package service_test

import (
	"context"
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
)

func TestFailedReservationMovePreservesOriginalOwnership(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	store, err := storepkg.Open(ctx, filepath.Join(t.TempDir(), "move.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.EnsureTenant(ctx, "tenant", "Studio", now); err != nil {
		t.Fatal(err)
	}
	user, _ := identity.NewUser("user", "tenant", "reader@example.test", "Reader", []identity.Role{identity.Researcher}, []byte("hash"), now)
	if err := store.InsertUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	work, _ := catalog.NewWork("work", "tenant", "Dream", "Cao", "reading", now)
	if err := store.InsertWork(ctx, work); err != nil {
		t.Fatal(err)
	}
	edition, _ := catalog.NewEdition("edition", "tenant", work.ID, "Print", "Press", now.AddDate(-1, 0, 0), 1200, "copy", now)
	_ = edition.Activate(now)
	if err := store.InsertEdition(ctx, edition); err != nil {
		t.Fatal(err)
	}
	value, _ := program.New("program", "tenant", edition.ID, user.ID, "Seminar", "Asia/Shanghai", 8, now.Add(-time.Hour), now.Add(24*time.Hour), now.Add(-48*time.Hour))
	_ = value.OpenEnrollment(now.Add(-47 * time.Hour))
	_ = value.Start(now)
	if err := store.InsertProgram(ctx, value); err != nil {
		t.Fatal(err)
	}
	member, _ := program.NewMember("member", "tenant", value.ID, user.ID, now)
	if err := store.InsertMember(ctx, member); err != nil {
		t.Fatal(err)
	}
	original, _ := reading.NewAssignment("original", "tenant", value.ID, edition.ID, member.ID, reading.PageRange{Start: 10, End: 20}, now, now.Add(2*time.Hour), now)
	if err := store.InsertAssignment(ctx, original); err != nil {
		t.Fatal(err)
	}
	other, _ := reading.NewAssignment("other", "tenant", value.ID, edition.ID, member.ID, reading.PageRange{Start: 30, End: 40}, now, now.Add(2*time.Hour), now)
	if err := store.InsertAssignment(ctx, other); err != nil {
		t.Fatal(err)
	}
	svc := service.ReadingService{Dependencies: service.Dependencies{Store: store, Clock: clock.NewManual(now), IDs: idgen.NewSequence(1)}}
	actor := service.Actor{TenantID: "tenant", UserID: user.ID, RequestID: "move"}
	if _, err := svc.MoveReservation(ctx, actor, original.ID, reading.PageRange{Start: 35, End: 45}, now, now.Add(time.Hour)); err == nil {
		t.Fatal("conflicting move unexpectedly succeeded")
	}
	persisted, err := store.GetAssignment(ctx, "tenant", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != reading.AssignmentReserved || persisted.Pages != original.Pages {
		t.Fatalf("failed move changed original reservation: %+v", persisted)
	}
}
