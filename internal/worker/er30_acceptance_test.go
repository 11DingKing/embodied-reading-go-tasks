package worker_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/worker"
)

type cleanupTask struct {
	started     chan struct{}
	observed    chan bool
	storeClosed *atomic.Bool
}

func (*cleanupTask) Name() string { return "cleanup" }

func (t *cleanupTask) Run(ctx context.Context) error {
	close(t.started)
	<-ctx.Done()
	t.observed <- t.storeClosed.Load()
	return ctx.Err()
}

func TestShutdownWaitsForTaskCleanupBeforeDatabaseClose(t *testing.T) {
	var storeClosed atomic.Bool
	task := &cleanupTask{started: make(chan struct{}), observed: make(chan bool, 1), storeClosed: &storeClosed}
	runtime := worker.Runtime{Tasks: []worker.Task{task}, Interval: time.Hour, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if !runtime.Start(context.Background()) {
		t.Fatal("runtime did not start")
	}
	<-task.started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.StopAndClose(ctx, &runtime, func() error {
		storeClosed.Store(true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if <-task.observed {
		t.Fatal("database closed before task cleanup")
	}
}
