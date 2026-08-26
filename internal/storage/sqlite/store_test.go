package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/storage/sqlite"
)

var storeNow = time.Date(2026, 8, 22, 14, 0, 0, 0, time.UTC)

type fixture struct {
	store      *sqlite.Store
	path       string
	user       identity.User
	edition    catalog.Edition
	program    program.Program
	member     program.Member
	assignment reading.Assignment
}

func openFixture(t *testing.T) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "studio.db")
	store, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixture := &fixture{store: store, path: path}
	if err := fixture.seed(context.Background()); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	return fixture
}

func (f *fixture) seed(ctx context.Context) error {
	if err := f.store.EnsureTenant(ctx, "tenant", "Embodied Reading Lab", storeNow); err != nil {
		return err
	}
	user, err := identity.NewUser("user", "tenant", "reader@example.test", "Reader", []identity.Role{identity.Coordinator, identity.Researcher, identity.Reviewer}, []byte("password-hash"), storeNow)
	if err != nil {
		return err
	}
	f.user = user
	if err := f.store.InsertUser(ctx, user); err != nil {
		return err
	}
	work, err := catalog.NewWork("work", "tenant", "Dream of the Red Chamber", "Cao Xueqin", "Physical-edition close reading", storeNow)
	if err != nil {
		return err
	}
	if err := f.store.InsertWork(ctx, work); err != nil {
		return err
	}
	edition, err := catalog.NewEdition("edition", "tenant", work.ID, "Annotated 2026", "Reader Press", storeNow.Add(-365*24*time.Hour), 1200, "sha256:physical-edition-one", storeNow)
	if err != nil {
		return err
	}
	if err := edition.Activate(storeNow); err != nil {
		return err
	}
	f.edition = edition
	if err := f.store.InsertEdition(ctx, edition); err != nil {
		return err
	}
	value, err := program.New("program", "tenant", edition.ID, user.ID, "Chapter 77 Seminar", "Asia/Shanghai", 8,
		storeNow.Add(-time.Hour), storeNow.Add(24*time.Hour), storeNow.Add(-48*time.Hour))
	if err != nil {
		return err
	}
	if err := value.OpenEnrollment(storeNow.Add(-47 * time.Hour)); err != nil {
		return err
	}
	if err := value.Start(storeNow); err != nil {
		return err
	}
	f.program = value
	if err := f.store.InsertProgram(ctx, value); err != nil {
		return err
	}
	member, err := program.NewMember("member", "tenant", value.ID, user.ID, storeNow.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	f.member = member
	if err := f.store.InsertMember(ctx, member); err != nil {
		return err
	}
	assignment, err := reading.NewAssignment("assignment", "tenant", value.ID, edition.ID, member.ID, reading.PageRange{Start: 760, End: 780}, storeNow, storeNow.Add(2*time.Hour), storeNow)
	if err != nil {
		return err
	}
	f.assignment = assignment
	return f.store.InsertAssignment(ctx, assignment)
}

func TestMigrationCreatesRelationalSchemaAndCanRestart(t *testing.T) {
	fixture := openFixture(t)
	if err := fixture.store.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := sqlite.Open(context.Background(), fixture.path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer reopened.Close()
	user, err := reopened.GetUser(context.Background(), "tenant", fixture.user.ID)
	if err != nil {
		t.Fatalf("GetUser() after restart = %v", err)
	}
	if user.Email != fixture.user.Email || user.Version != fixture.user.Version {
		t.Fatalf("recovered user = %+v", user)
	}
	programValue, err := reopened.GetProgram(context.Background(), "tenant", fixture.program.ID)
	if err != nil {
		t.Fatalf("GetProgram() after restart = %v", err)
	}
	if programValue.State != program.Active || programValue.EditionID != fixture.edition.ID {
		t.Fatalf("recovered program = %+v", programValue)
	}
}

func TestTransactionRollbackDoesNotLeavePartialState(t *testing.T) {
	fixture := openFixture(t)
	errSentinel := errors.New("audit storage unavailable")
	newMember, err := program.NewMember("member-rollback", "tenant", fixture.program.ID, fixture.user.ID, storeNow)
	if err != nil {
		t.Fatal(err)
	}
	err = fixture.store.WithinTx(context.Background(), func(ctx context.Context, tx repository.Tx) error {
		if err := tx.InsertMember(ctx, newMember); err == nil {
			t.Fatal("duplicate program membership unexpectedly inserted")
		}
		return errSentinel
	})
	if !errors.Is(err, errSentinel) {
		t.Fatalf("transaction error = %v", err)
	}
	count, err := fixture.store.CountActiveMembers(context.Background(), "tenant", fixture.program.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("active member count = %d", count)
	}
}

func TestTransactionRollsBackSuccessfulWritesAfterLaterFailure(t *testing.T) {
	fixture := openFixture(t)
	other, err := identity.NewUser("other", "tenant", "other@example.test", "Other Reader", []identity.Role{identity.Researcher}, []byte("hash"), storeNow)
	if err != nil {
		t.Fatal(err)
	}
	err = fixture.store.WithinTx(context.Background(), func(ctx context.Context, tx repository.Tx) error {
		if err := tx.InsertUser(ctx, other); err != nil {
			return err
		}
		return errors.New("forced downstream failure")
	})
	if err == nil {
		t.Fatal("transaction unexpectedly committed")
	}
	if _, err := fixture.store.GetUser(context.Background(), "tenant", other.ID); !fault.IsKind(err, fault.NotFound) {
		t.Fatalf("rolled back user lookup error = %v", err)
	}
}

func TestOptimisticVersionPreventsLostUpdate(t *testing.T) {
	fixture := openFixture(t)
	first, err := fixture.store.GetProgram(context.Background(), "tenant", fixture.program.ID)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	first.Name = "First coordinator edit"
	first.Version++
	first.UpdatedAt = storeNow.Add(time.Minute)
	if err := fixture.store.UpdateProgram(context.Background(), first, fixture.program.Version); err != nil {
		t.Fatalf("first update = %v", err)
	}
	second.Name = "Stale coordinator edit"
	second.Version++
	second.UpdatedAt = storeNow.Add(2 * time.Minute)
	if err := fixture.store.UpdateProgram(context.Background(), second, fixture.program.Version); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("stale update error = %v", err)
	}
	persisted, err := fixture.store.GetProgram(context.Background(), "tenant", fixture.program.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != "First coordinator edit" {
		t.Fatalf("persisted name = %q", persisted.Name)
	}
}

func TestAssignmentConflictQueryUsesTenantProgramEditionPageAndTime(t *testing.T) {
	fixture := openFixture(t)
	conflicts, err := fixture.store.FindAssignmentConflicts(context.Background(), "tenant", fixture.program.ID, fixture.edition.ID,
		reading.PageRange{Start: 770, End: 790}, storeNow.Add(time.Hour), storeNow.Add(3*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || conflicts[0].ID != fixture.assignment.ID {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	none, err := fixture.store.FindAssignmentConflicts(context.Background(), "tenant", fixture.program.ID, fixture.edition.ID,
		reading.PageRange{Start: 781, End: 790}, storeNow.Add(time.Hour), storeNow.Add(3*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("non-overlapping pages returned conflicts: %+v", none)
	}
	excluded, err := fixture.store.FindAssignmentConflicts(context.Background(), "tenant", fixture.program.ID, fixture.edition.ID,
		fixture.assignment.Pages, fixture.assignment.WindowStart, fixture.assignment.WindowEnd, fixture.assignment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(excluded) != 0 {
		t.Fatalf("excluded assignment returned: %+v", excluded)
	}
}

func TestIdempotencyRecordPersistsResponseCopyAndRouteIdentity(t *testing.T) {
	fixture := openFixture(t)
	response := []byte(`{"member_id":"member"}`)
	record, err := repository.NewIdempotencyRecord("tenant", "join-key", "post", "/v1/programs/program/join", "user",
		repository.HashRequest("POST", "/v1/programs/program/join", nil), 201, response, storeNow, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.InsertIdempotency(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	response[0] = 'X'
	persisted, err := fixture.store.GetIdempotency(context.Background(), "tenant", "join-key", "POST", "/v1/programs/program/join", "user")
	if err != nil {
		t.Fatal(err)
	}
	if string(persisted.Response) != `{"member_id":"member"}` {
		t.Fatalf("persisted response = %q", persisted.Response)
	}
	if !persisted.Matches("post", persisted.Path, persisted.ActorID, persisted.RequestHash) {
		t.Fatal("persisted record did not match normalized method")
	}
	if persisted.Matches("PUT", persisted.Path, persisted.ActorID, persisted.RequestHash) {
		t.Fatal("different method matched")
	}
}

func TestOutboxClaimAllowsOnlyOneConcurrentOwner(t *testing.T) {
	fixture := openFixture(t)
	event, err := repository.NewOutboxEvent("event", "tenant", "claim.submitted", "claim", map[string]string{"claim_id": "claim"}, storeNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.InsertOutbox(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan repository.OutboxEvent, 2)
	errorsChannel := make(chan error, 2)
	var wait sync.WaitGroup
	for _, owner := range []string{"worker-a", "worker-b"} {
		wait.Add(1)
		go func(owner string) {
			defer wait.Done()
			<-start
			claimed, err := fixture.store.ClaimOutbox(context.Background(), owner, storeNow, time.Minute)
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- claimed
		}(owner)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsChannel)
	if len(results) != 1 || len(errorsChannel) != 1 {
		t.Fatalf("claims=%d errors=%d", len(results), len(errorsChannel))
	}
	claimed := <-results
	if claimed.LeaseToken != 1 || claimed.State != repository.OutboxLeased {
		t.Fatalf("claimed event = %+v", claimed)
	}
	if err := fixture.store.CompleteOutbox(context.Background(), claimed.ID, claimed.LeaseOwner, claimed.LeaseToken, storeNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestOutboxFailureTransitionsToRetryThenPermanentFailure(t *testing.T) {
	fixture := openFixture(t)
	event, err := repository.NewOutboxEvent("event", "tenant", "claim.submitted", "claim", map[string]string{"claim_id": "claim"}, storeNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.InsertOutbox(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	claimed, err := fixture.store.ClaimOutbox(context.Background(), "worker", storeNow, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.FailOutbox(context.Background(), claimed.ID, "worker", claimed.LeaseToken, errors.New("temporary"), storeNow.Add(time.Minute), 2, storeNow); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ClaimOutbox(context.Background(), "worker", storeNow.Add(30*time.Second), time.Minute); !fault.IsKind(err, fault.NotFound) {
		t.Fatalf("early retry claim = %v", err)
	}
	retried, err := fixture.store.ClaimOutbox(context.Background(), "worker", storeNow.Add(time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Attempt != 2 {
		t.Fatalf("retry attempt = %d", retried.Attempt)
	}
	if err := fixture.store.FailOutbox(context.Background(), retried.ID, "worker", retried.LeaseToken, errors.New("permanent"), storeNow.Add(2*time.Minute), 2, storeNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ClaimOutbox(context.Background(), "worker", storeNow.Add(3*time.Minute), time.Minute); !fault.IsKind(err, fault.NotFound) {
		t.Fatalf("permanently failed event reclaimed: %v", err)
	}
}

func TestCancelledContextDoesNotOpenTransaction(t *testing.T) {
	fixture := openFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := fixture.store.WithinTx(ctx, func(context.Context, repository.Tx) error {
		called = true
		return nil
	})
	if called {
		t.Fatal("transaction callback ran after cancellation")
	}
	if !fault.IsKind(err, fault.Cancelled) {
		t.Fatalf("cancelled transaction error = %v", err)
	}
}

func TestTenantIsolationReturnsNotFound(t *testing.T) {
	fixture := openFixture(t)
	if _, err := fixture.store.GetProgram(context.Background(), "another-tenant", fixture.program.ID); !fault.IsKind(err, fault.NotFound) {
		t.Fatalf("cross-tenant program lookup = %v", err)
	}
	if _, err := fixture.store.GetAssignment(context.Background(), "another-tenant", fixture.assignment.ID); !fault.IsKind(err, fault.NotFound) {
		t.Fatalf("cross-tenant assignment lookup = %v", err)
	}
}
