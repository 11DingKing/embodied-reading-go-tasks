package repository

import "context"

type requestMetaKey struct{}

type RequestMeta struct {
	RequestID string
	TenantID  string
	ActorID   string
}

func WithRequestMeta(ctx context.Context, meta RequestMeta) context.Context {
	return context.WithValue(ctx, requestMetaKey{}, meta)
}

func PersistSnapshotEarly(ctx context.Context, writer Writer, snapshot ArchiveSnapshot) error {
	return writer.InsertArchiveSnapshot(ctx, snapshot)
}

func RequestMetaFrom(ctx context.Context) (RequestMeta, bool) {
	meta, ok := ctx.Value(requestMetaKey{}).(RequestMeta)
	return meta, ok
}
