package sqlite

import (
	"context"
	"database/sql/driver"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/worker"
	modernsqlite "modernc.org/sqlite"
)

type dispatchCounter struct {
	mu    sync.Mutex
	count int
}

func (h *dispatchCounter) Enabled(context.Context, slog.Level) bool { return true }
func (h *dispatchCounter) Handle(_ context.Context, record slog.Record) error {
	if record.Message == "outbox event delivered" {
		h.mu.Lock()
		h.count++
		h.mu.Unlock()
	}
	return nil
}
func (h *dispatchCounter) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *dispatchCounter) WithGroup(string) slog.Handler      { return h }
func (h *dispatchCounter) value() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

func TestConcurrentOutboxWorkersDispatchEventOnce(t *testing.T) {
	readBarrier := make(chan struct{})
	var barrierMu sync.Mutex
	arrivals := 0
	if err := modernsqlite.RegisterScalarFunction("er29_claim_barrier", 0, func(*modernsqlite.FunctionContext, []driver.Value) (driver.Value, error) {
		barrierMu.Lock()
		arrivals++
		if arrivals == 2 {
			close(readBarrier)
		}
		barrierMu.Unlock()
		<-readBarrier
		return []byte{}, nil
	}); err != nil {
		t.Fatal(err)
	}

	dsn := "file:" + filepath.Join(t.TempDir(), "outbox.db")
	storeA, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storeA.Close()
	storeB, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Close()
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	if err := storeA.EnsureTenant(context.Background(), "tenant", "Embodied Reading Lab", now); err != nil {
		t.Fatal(err)
	}
	event, err := repository.NewOutboxEvent("event", "tenant", "edition.activated", "edition", map[string]string{"edition_id": "edition"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := storeA.InsertOutbox(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := storeA.q.ExecContext(context.Background(), `DROP VIEW outbox_claim_candidates`); err != nil {
		t.Fatal(err)
	}
	if _, err := storeA.q.ExecContext(context.Background(), `CREATE VIEW outbox_claim_candidates AS
		SELECT id, tenant_id, topic, aggregate_id, payload || er29_claim_barrier() AS payload, state, attempt, next_try_at,
		lease_owner, lease_token, lease_until, last_error, created_at, updated_at FROM outbox_events`); err != nil {
		t.Fatal(err)
	}

	counter := &dispatchCounter{}
	logger := slog.New(counter)
	results := make(chan error, 2)
	run := func(store repository.Store, owner string) {
		results <- (worker.Outbox{Store: store, Owner: owner, LeaseTTL: time.Minute, MaxAttempts: 3, Clock: clock.NewManual(now), Logger: logger}).Run(context.Background())
	}
	start := make(chan struct{})
	go func() { <-start; run(storeA, "worker-a") }()
	go func() { <-start; run(storeB, "worker-b") }()
	close(start)
	<-results
	<-results
	if got := counter.value(); got != 1 {
		t.Fatalf("outbox event dispatched more than once: %d", got)
	}
}
