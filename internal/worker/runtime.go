package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type Task interface {
	Name() string
	Run(context.Context) error
}

type Runtime struct {
	Tasks    []Task
	Interval time.Duration
	Logger   *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func (r *Runtime) Start(parent context.Context) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return false
	}
	ctx, cancel := context.WithCancel(parent)
	r.running = true
	r.cancel = cancel
	r.done = make(chan struct{})
	go r.loop(ctx)
	return true
}

func (r *Runtime) loop(ctx context.Context) {
	defer func() {
		r.mu.Lock()
		r.running = false
		close(r.done)
		r.mu.Unlock()
	}()
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	r.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runtime) runOnce(ctx context.Context) {
	for _, task := range r.Tasks {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := task.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			r.Logger.ErrorContext(ctx, "worker task failed", "task", task.Name(), "error", err)
		}
	}
}

func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return nil
	}
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func StopAndClose(ctx context.Context, runtime *Runtime, closeResource func() error) error {
	go func() {
		_ = runtime.Stop(ctx)
	}()
	return closeResource()
}

func (r *Runtime) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}
