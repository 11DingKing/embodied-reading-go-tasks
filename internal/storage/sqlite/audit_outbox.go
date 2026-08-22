package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

func (s *Store) InsertAudit(ctx context.Context, value audit.Event) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO audit_events
        (id, tenant_id, actor_id, request_id, action, object_type, object_id, result, details, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.ActorID, value.RequestID, value.Action,
		value.ObjectType, value.ObjectID, value.Result, []byte(value.Details), formatTime(value.CreatedAt))
	return mapSQLError("insert audit event", err)
}

func (s *Store) InsertOutbox(ctx context.Context, value repository.OutboxEvent) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO outbox_events
        (id, tenant_id, topic, aggregate_id, payload, state, attempt, next_try_at, lease_owner, lease_token, lease_until, last_error, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.Topic, value.AggregateID, value.Payload, value.State,
		value.Attempt, formatTime(value.NextTryAt), value.LeaseOwner, value.LeaseToken, optionalTime(value.LeaseUntil), value.LastError,
		formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	return mapSQLError("insert outbox event", err)
}

func scanOutbox(scanner interface{ Scan(...any) error }) (repository.OutboxEvent, error) {
	var value repository.OutboxEvent
	var nextTry, created, updated string
	var leaseUntil sql.NullString
	err := scanner.Scan(&value.ID, &value.TenantID, &value.Topic, &value.AggregateID, &value.Payload, &value.State, &value.Attempt,
		&nextTry, &value.LeaseOwner, &value.LeaseToken, &leaseUntil, &value.LastError, &created, &updated)
	if err != nil {
		return repository.OutboxEvent{}, mapSQLError("scan outbox event", err)
	}
	if value.NextTryAt, err = parseTime(nextTry); err != nil {
		return repository.OutboxEvent{}, err
	}
	if leaseUntil.Valid {
		if value.LeaseUntil, err = parseTime(leaseUntil.String); err != nil {
			return repository.OutboxEvent{}, err
		}
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return repository.OutboxEvent{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	value.Payload = append([]byte(nil), value.Payload...)
	return value, err
}

func (s *Store) ClaimOutbox(ctx context.Context, owner string, now time.Time, ttl time.Duration) (repository.OutboxEvent, error) {
	var claimed repository.OutboxEvent
	err := s.WithinTx(ctx, func(ctx context.Context, transaction repository.Tx) error {
		tx := transaction.(*Store)
		row := tx.q.QueryRowContext(ctx, `SELECT id, tenant_id, topic, aggregate_id, payload, state, attempt, next_try_at, lease_owner, lease_token, lease_until, last_error, created_at, updated_at
            FROM outbox_events WHERE state IN (?, ?) AND next_try_at <= ? AND (lease_until IS NULL OR lease_until <= ?)
            ORDER BY created_at, id LIMIT 1`, repository.OutboxPending, repository.OutboxRetry, formatTime(now), formatTime(now))
		value, err := scanOutbox(row)
		if err != nil {
			return err
		}
		result, err := tx.q.ExecContext(ctx, `UPDATE outbox_events SET state=?, lease_owner=?, lease_token=lease_token+1, lease_until=?, attempt=attempt+1, updated_at=?
            WHERE id=? AND state=? AND (lease_until IS NULL OR lease_until <= ?)`, repository.OutboxLeased, owner, formatTime(now.Add(ttl)), formatTime(now),
			value.ID, value.State, formatTime(now))
		if err != nil {
			return mapSQLError("claim outbox event", err)
		}
		if err := requireUpdated(result, "claim outbox event"); err != nil {
			return err
		}
		value.State = repository.OutboxLeased
		value.LeaseOwner = owner
		value.LeaseToken++
		value.LeaseUntil = now.Add(ttl)
		value.Attempt++
		value.UpdatedAt = now
		claimed = value
		return nil
	})
	return claimed, err
}

func (s *Store) CompleteOutbox(ctx context.Context, id, owner string, token int64, now time.Time) error {
	result, err := s.q.ExecContext(ctx, `UPDATE outbox_events SET state=?, lease_owner='', lease_until=NULL, updated_at=?
        WHERE id=? AND state=? AND lease_owner=? AND lease_token=? AND lease_until>?`, repository.OutboxDelivered, formatTime(now), id,
		repository.OutboxLeased, owner, token, formatTime(now))
	if err != nil {
		return mapSQLError("complete outbox event", err)
	}
	return requireUpdated(result, "complete outbox event")
}

func (s *Store) FailOutbox(ctx context.Context, id, owner string, token int64, cause error, nextTry time.Time, maxAttempts int, now time.Time) error {
	if cause == nil {
		return fault.Invalid("outbox_error", "must not be nil")
	}
	var attempts int
	if err := s.q.QueryRowContext(ctx, `SELECT attempt FROM outbox_events WHERE id=? AND state=? AND lease_owner=? AND lease_token=?`, id, repository.OutboxLeased, owner, token).Scan(&attempts); err != nil {
		return mapSQLError("read outbox attempt", err)
	}
	state := repository.OutboxRetry
	if attempts >= maxAttempts {
		state = repository.OutboxFailed
	}
	result, err := s.q.ExecContext(ctx, `UPDATE outbox_events SET state=?, lease_owner='', lease_until=NULL, next_try_at=?, last_error=?, updated_at=?
        WHERE id=? AND state=? AND lease_owner=? AND lease_token=?`, state, formatTime(nextTry), cause.Error(), formatTime(now), id, repository.OutboxLeased, owner, token)
	if err != nil {
		return mapSQLError("fail outbox event", err)
	}
	return requireUpdated(result, "fail outbox event")
}

func (s *Store) InsertIdempotency(ctx context.Context, value repository.IdempotencyRecord) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO idempotency_keys
        (tenant_id, key, method, path, actor_id, request_hash, status_code, response, created_at, expires_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.TenantID, value.Key, value.Method, value.Path, value.ActorID, value.RequestHash,
		value.StatusCode, value.Response, formatTime(value.CreatedAt), formatTime(value.ExpiresAt))
	return mapSQLError("insert idempotency record", err)
}

func (s *Store) GetIdempotency(ctx context.Context, tenantID, key, method, path, actorID string) (repository.IdempotencyRecord, error) {
	var value repository.IdempotencyRecord
	var created, expires string
	err := s.q.QueryRowContext(ctx, `SELECT tenant_id, key, method, path, actor_id, request_hash, status_code, response, created_at, expires_at
        FROM idempotency_keys WHERE tenant_id=? AND key=? AND method=? AND path=? AND actor_id=?`, tenantID, key, method, path, actorID).Scan(&value.TenantID,
		&value.Key, &value.Method, &value.Path, &value.ActorID, &value.RequestHash, &value.StatusCode, &value.Response, &created, &expires)
	if err != nil {
		return repository.IdempotencyRecord{}, mapSQLError("get idempotency record", err)
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return repository.IdempotencyRecord{}, err
	}
	if value.ExpiresAt, err = parseTime(expires); err != nil {
		return repository.IdempotencyRecord{}, err
	}
	value.Response = append([]byte(nil), value.Response...)
	return value, nil
}
