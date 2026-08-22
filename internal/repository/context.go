package repository

import (
	"context"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/seminar"
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

func PersistAgendaEarly(ctx context.Context, writer Writer, agenda seminar.Agenda) error {
	return writer.InsertAgenda(ctx, agenda)
}

func RequestMetaFrom(ctx context.Context) (RequestMeta, bool) {
	meta, ok := ctx.Value(requestMetaKey{}).(RequestMeta)
	return meta, ok
}
