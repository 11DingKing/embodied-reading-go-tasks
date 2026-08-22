package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
)

type Outbox struct {
	Store       repository.Store
	Seminars    service.SeminarService
	Archives    service.ArchiveService
	Owner       string
	LeaseTTL    time.Duration
	MaxAttempts int
	Clock       interface{ Now() time.Time }
	Logger      *slog.Logger
}

func (w Outbox) Name() string { return "outbox" }

func (w Outbox) Run(ctx context.Context) error {
	now := w.Clock.Now()
	event, err := w.Store.ClaimOutbox(ctx, w.Owner, now, w.LeaseTTL)
	if err != nil {
		if fault.IsKind(err, fault.NotFound) || errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if err := w.dispatch(ctx, event); err != nil {
		next := w.Clock.Now().Add(backoff(event.Attempt))
		persistErr := w.Store.FailOutbox(context.WithoutCancel(ctx), event.ID, w.Owner, event.LeaseToken, err, next, w.MaxAttempts, w.Clock.Now())
		if persistErr != nil {
			return errors.Join(err, persistErr)
		}
		return err
	}
	return w.Store.CompleteOutbox(ctx, event.ID, w.Owner, event.LeaseToken, w.Clock.Now())
}

func (w Outbox) dispatch(ctx context.Context, event repository.OutboxEvent) error {
	switch event.Topic {
	case "program.review_requested":
		var payload struct {
			ProgramID string `json:"program_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fault.Wrap(fault.Validation, "invalid_review_event", "decode review event", err)
		}
		_, err := w.Seminars.Generate(ctx, event.TenantID, payload.ProgramID)
		return err
	case "archive.requested":
		var payload struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fault.Wrap(fault.Validation, "invalid_archive_event", "decode archive event", err)
		}
		return w.Archives.Process(ctx, event.TenantID, payload.JobID, w.Owner)
	case "edition.activated", "claim.submitted", "review.assigned", "review.decided", "claim.disputed", "claim.dispute_resolved", "agenda.closed", "archive.completed":
		w.Logger.InfoContext(ctx, "outbox event delivered", "topic", event.Topic, "aggregate_id", event.AggregateID)
		return nil
	default:
		return fault.New(fault.Validation, "unsupported_outbox_topic", "outbox topic has no handler")
	}
}

func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}
