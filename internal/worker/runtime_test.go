package worker_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/worker"
)

type task struct {
	name  string
	runs  atomic.Int64
	err   error
	block chan struct{}
}

func (t *task) Name() string { return t.name }

func (t *task) Run(ctx context.Context) error {
	t.runs.Add(1)
	if t.block != nil {
		select {
		case <-t.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return t.err
}

func logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRuntimeStartsOnceRunsTasksAndStops(t *testing.T) {
	first := &task{name: "first"}
	second := &task{name: "second"}
	runtime := &worker.Runtime{Tasks: []worker.Task{first, second}, Interval: 5 * time.Millisecond, Logger: logger()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !runtime.Start(ctx) {
		t.Fatal("first Start() returned false")
	}
	if runtime.Start(ctx) {
		t.Fatal("second Start() returned true")
	}
	deadline := time.Now().Add(time.Second)
	for (first.runs.Load() < 2 || second.runs.Load() < 2) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if first.runs.Load() < 2 || second.runs.Load() < 2 {
		t.Fatalf("runs first=%d second=%d", first.runs.Load(), second.runs.Load())
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := runtime.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if runtime.Running() {
		t.Fatal("runtime still running after Stop")
	}
}

func TestRuntimeContinuesAfterTaskError(t *testing.T) {
	failing := &task{name: "failing", err: errors.New("temporary failure")}
	healthy := &task{name: "healthy"}
	runtime := &worker.Runtime{Tasks: []worker.Task{failing, healthy}, Interval: time.Hour, Logger: logger()}
	ctx, cancel := context.WithCancel(context.Background())
	if !runtime.Start(ctx) {
		t.Fatal("runtime did not start")
	}
	deadline := time.Now().Add(time.Second)
	for healthy.runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := runtime.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if failing.runs.Load() != 1 || healthy.runs.Load() != 1 {
		t.Fatalf("runs failing=%d healthy=%d", failing.runs.Load(), healthy.runs.Load())
	}
}

func TestRuntimeCancellationInterruptsBlockingTask(t *testing.T) {
	blocking := &task{name: "blocking", block: make(chan struct{})}
	runtime := &worker.Runtime{Tasks: []worker.Task{blocking}, Interval: time.Hour, Logger: logger()}
	ctx, cancel := context.WithCancel(context.Background())
	if !runtime.Start(ctx) {
		t.Fatal("runtime did not start")
	}
	deadline := time.Now().Add(time.Second)
	for blocking.runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := runtime.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeStopHonorsCallerDeadline(t *testing.T) {
	release := make(chan struct{})
	stubborn := &stubbornTask{started: make(chan struct{}), release: release}
	runtime := &worker.Runtime{Tasks: []worker.Task{stubborn}, Interval: time.Hour, Logger: logger()}
	if !runtime.Start(context.Background()) {
		t.Fatal("runtime did not start")
	}
	<-stubborn.started
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := runtime.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v", err)
	}
	close(release)
	finalCtx, finalCancel := context.WithTimeout(context.Background(), time.Second)
	defer finalCancel()
	if err := runtime.Stop(finalCtx); err != nil {
		t.Fatal(err)
	}
}

type stubbornTask struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (t *stubbornTask) Name() string { return "stubborn" }

func (t *stubbornTask) Run(context.Context) error {
	t.once.Do(func() {
		close(t.started)
	})
	<-t.release
	return nil
}
