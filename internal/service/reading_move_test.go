package service_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"github.com/11DingKing/embodied-reading-studio/internal/storage/sqlite"
)

var moveNow = time.Date(2026, 8, 22, 14, 0, 0, 0, time.UTC)

type moveFixture struct {
	readings service.ReadingService
	actor    service.Actor
	store    *sqlite.Store
	owner    reading.Assignment
}

func newMoveFixture(t *testing.T) *moveFixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "studio.db")
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.EnsureTenant(ctx, "tenant", "Embodied Reading Lab", moveNow); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	user, err := identity.NewUser("user", "tenant", "reader@example.test", "Reader",
		[]identity.Role{identity.Coordinator, identity.Researcher, identity.Reviewer}, []byte("password-hash"), moveNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	work, err := catalog.NewWork("work", "tenant", "Dream of the Red Chamber", "Cao Xueqin", "close reading", moveNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertWork(ctx, work); err != nil {
		t.Fatal(err)
	}
	edition, err := catalog.NewEdition("edition", "tenant", work.ID, "Annotated 2026", "Reader Press", moveNow.Add(-365*24*time.Hour), 1200, "sha256:edition", moveNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := edition.Activate(moveNow); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertEdition(ctx, edition); err != nil {
		t.Fatal(err)
	}
	value, err := program.New("program", "tenant", edition.ID, user.ID, "Chapter 77 Seminar", "Asia/Shanghai", 8,
		moveNow.Add(-time.Hour), moveNow.Add(24*time.Hour), moveNow.Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := value.OpenEnrollment(moveNow.Add(-47 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := value.Start(moveNow); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertProgram(ctx, value); err != nil {
		t.Fatal(err)
	}
	member, err := program.NewMember("member", "tenant", value.ID, user.ID, moveNow.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertMember(ctx, member); err != nil {
		t.Fatal(err)
	}
	owner, err := reading.NewAssignment("owner", "tenant", value.ID, edition.ID, member.ID,
		reading.PageRange{Start: 100, End: 120}, moveNow, moveNow.Add(2*time.Hour), moveNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertAssignment(ctx, owner); err != nil {
		t.Fatal(err)
	}
	// A second reservation that the move target will collide with.
	blocker, err := reading.NewAssignment("blocker", "tenant", value.ID, edition.ID, member.ID,
		reading.PageRange{Start: 200, End: 220}, moveNow, moveNow.Add(2*time.Hour), moveNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertAssignment(ctx, blocker); err != nil {
		t.Fatal(err)
	}

	clock := clock.NewManual(moveNow)
	ids := idgen.NewSequence(1)
	readings := service.ReadingService{Dependencies: service.Dependencies{Store: store, Clock: clock, IDs: ids}, SessionLeaseTTL: 30 * time.Minute}
	actor := service.Actor{TenantID: "tenant", UserID: user.ID, RequestID: "request"}
	return &moveFixture{readings: readings, actor: actor, store: store, owner: owner}
}

// TestMoveReservationConflictLeavesOriginalIntact reproduces the failed-migration
// regression: when the target range overlaps another reservation the move is
// rejected, and the caller's original reservation must remain reserved and owned
// rather than being released as a side effect of the failed attempt.
func TestMoveReservationConflictLeavesOriginalIntact(t *testing.T) {
	fixture := newMoveFixture(t)
	ctx := context.Background()

	_, err := fixture.readings.MoveReservation(ctx, fixture.actor, fixture.owner.ID,
		reading.PageRange{Start: 210, End: 215}, fixture.owner.WindowStart, fixture.owner.WindowEnd)
	if !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("MoveReservation() error = %v, want conflict", err)
	}

	persisted, err := fixture.store.GetAssignment(ctx, "tenant", fixture.owner.ID)
	if err != nil {
		t.Fatalf("GetAssignment() error = %v", err)
	}
	if persisted.State != reading.AssignmentReserved {
		t.Fatalf("original reservation state = %s, want reserved", persisted.State)
	}
	if persisted.Pages != fixture.owner.Pages {
		t.Fatalf("original reservation pages = %+v, want %+v", persisted.Pages, fixture.owner.Pages)
	}
	if persisted.Version != fixture.owner.Version {
		t.Fatalf("original reservation version = %d, want %d (failed move must not bump version)", persisted.Version, fixture.owner.Version)
	}
}

// TestMoveReservationRelocatesToFreeRange confirms the happy path still moves
// the reservation to a conflict-free range in a single atomic transition.
func TestMoveReservationRelocatesToFreeRange(t *testing.T) {
	fixture := newMoveFixture(t)
	ctx := context.Background()

	target := reading.PageRange{Start: 300, End: 320}
	moved, err := fixture.readings.MoveReservation(ctx, fixture.actor, fixture.owner.ID,
		target, fixture.owner.WindowStart, fixture.owner.WindowEnd)
	if err != nil {
		t.Fatalf("MoveReservation() error = %v", err)
	}
	if moved.Pages != target || moved.State != reading.AssignmentReserved {
		t.Fatalf("moved reservation = %+v", moved)
	}
	persisted, err := fixture.store.GetAssignment(ctx, "tenant", fixture.owner.ID)
	if err != nil {
		t.Fatalf("GetAssignment() error = %v", err)
	}
	if persisted.Pages != target || persisted.State != reading.AssignmentReserved {
		t.Fatalf("persisted reservation = %+v", persisted)
	}
}
