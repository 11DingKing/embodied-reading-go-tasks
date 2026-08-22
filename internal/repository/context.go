package repository

import (
	"context"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
)

type requestMetaKey struct{}

type RequestMeta struct {
	RequestID string
	TenantID  string
	ActorID   string
}

func WithRequestMeta(ctx context.Context, meta RequestMeta) context.Context {
	return context.WithValue(ctx, requestMetaKey{}, meta)
}

func FindReservationSnapshot(ctx context.Context, reader Reader, tenantID, programID, editionID string, pages reading.PageRange, start, end time.Time) ([]reading.Assignment, error) {
	return reader.FindAssignmentConflicts(ctx, tenantID, programID, editionID, pages, start, end, "")
}

func RequestMetaFrom(ctx context.Context) (RequestMeta, bool) {
	meta, ok := ctx.Value(requestMetaKey{}).(RequestMeta)
	return meta, ok
}
