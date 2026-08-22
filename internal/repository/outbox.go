package repository

import (
	"encoding/json"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

const (
	OutboxPending   = "pending"
	OutboxLeased    = "leased"
	OutboxDelivered = "delivered"
	OutboxRetry     = "retry_wait"
	OutboxFailed    = "permanent_failed"
)

func NewOutboxEvent(id, tenantID, topic, aggregateID string, payload any, now time.Time) (OutboxEvent, error) {
	if id == "" || tenantID == "" || topic == "" || aggregateID == "" {
		return OutboxEvent{}, fault.Invalid("outbox_event", "requires identity, topic, and aggregate")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return OutboxEvent{}, fault.Wrap(fault.Validation, "invalid_outbox_payload", "marshal outbox payload", err)
	}
	now = now.UTC()
	return OutboxEvent{
		ID: id, TenantID: tenantID, Topic: topic, AggregateID: aggregateID, Payload: encoded,
		State: OutboxPending, NextTryAt: now, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (e OutboxEvent) Clone() OutboxEvent {
	clone := e
	clone.Payload = append([]byte(nil), e.Payload...)
	return clone
}
