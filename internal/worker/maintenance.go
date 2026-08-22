package worker

import (
	"context"
	"log/slog"

	"github.com/11DingKing/embodied-reading-studio/internal/service"
)

type AssignmentExpiry struct {
	Readings service.ReadingService
	Limit    int
	Logger   *slog.Logger
}

func (w AssignmentExpiry) Name() string { return "assignment-expiry" }

func (w AssignmentExpiry) Run(ctx context.Context) error {
	count, err := w.Readings.ExpireAssignments(ctx, w.Limit)
	if err != nil {
		return err
	}
	if count > 0 {
		w.Logger.InfoContext(ctx, "expired reading assignments", "count", count)
	}
	return nil
}
