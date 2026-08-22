package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/clock"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

type Dependencies struct {
	Store repository.Store
	Clock clock.Clock
	IDs   idgen.Generator
}

func (d Dependencies) Validate() error {
	if d.Store == nil || d.Clock == nil || d.IDs == nil {
		return fault.New(fault.Internal, "service_dependencies_missing", "service dependencies are not configured")
	}
	return nil
}

type Actor struct {
	TenantID  string
	UserID    string
	RequestID string
}

func (a Actor) Validate() error {
	if a.TenantID == "" || a.UserID == "" || a.RequestID == "" {
		return fault.New(fault.Unauthorized, "actor_context_missing", "authenticated actor context is required")
	}
	return nil
}

func addAudit(ctx context.Context, tx repository.Writer, ids idgen.Generator, actor Actor, action, objectType, objectID string, result audit.Result, details any, now time.Time) error {
	event, err := audit.New(ids.New("audit"), actor.TenantID, actor.UserID, actor.RequestID, action, objectType, objectID, result, details, now)
	if err != nil {
		return err
	}
	return tx.InsertAudit(ctx, event)
}

func addOutbox(ctx context.Context, tx repository.Writer, ids idgen.Generator, tenantID, topic, aggregateID string, payload any, now time.Time) error {
	event, err := repository.NewOutboxEvent(ids.New("event"), tenantID, topic, aggregateID, payload, now)
	if err != nil {
		return err
	}
	return tx.InsertOutbox(ctx, event)
}

func contentHash(value any) (string, []byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", nil, fault.Wrap(fault.Internal, "content_encoding_failed", "encode content", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), payload, nil
}
