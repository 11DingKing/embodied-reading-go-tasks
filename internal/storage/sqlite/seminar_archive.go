package sqlite

import (
	"context"
	"database/sql"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/archive"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/seminar"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

func (s *Store) InsertAgenda(ctx context.Context, value seminar.Agenda) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO seminar_agendas(id, tenant_id, program_id, state, version, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.ProgramID, value.State, value.Version, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	return mapSQLError("insert seminar agenda", err)
}

func (s *Store) GetAgenda(ctx context.Context, tenantID, id string) (seminar.Agenda, error) {
	var value seminar.Agenda
	var created, updated string
	err := s.q.QueryRowContext(ctx, `SELECT id, tenant_id, program_id, state, version, created_at, updated_at
        FROM seminar_agendas WHERE tenant_id=? AND id=?`, tenantID, id).Scan(&value.ID, &value.TenantID, &value.ProgramID, &value.State, &value.Version, &created, &updated)
	if err != nil {
		return seminar.Agenda{}, mapSQLError("get seminar agenda", err)
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return seminar.Agenda{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err
}

func (s *Store) UpdateAgenda(ctx context.Context, value seminar.Agenda, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE seminar_agendas SET state=?, version=?, updated_at=? WHERE tenant_id=? AND id=? AND version=?`,
		value.State, value.Version, formatTime(value.UpdatedAt), value.TenantID, value.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update seminar agenda", err)
	}
	return requireUpdated(result, "update seminar agenda")
}

func (s *Store) InsertAgendaItem(ctx context.Context, value seminar.Item) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO agenda_items
        (id, tenant_id, agenda_id, claim_id, position, prompt, state, outcome, version, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.AgendaID, value.ClaimID, value.Position,
		value.Prompt, value.State, value.Outcome, value.Version, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	return mapSQLError("insert agenda item", err)
}

func (s *Store) ListAgendaItems(ctx context.Context, tenantID, agendaID string) ([]seminar.Item, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id, tenant_id, agenda_id, claim_id, position, prompt, state, outcome, version, created_at, updated_at
        FROM agenda_items WHERE tenant_id=? AND agenda_id=? ORDER BY position`, tenantID, agendaID)
	if err != nil {
		return nil, mapSQLError("list agenda items", err)
	}
	defer rows.Close()
	var values []seminar.Item
	for rows.Next() {
		var value seminar.Item
		var created, updated string
		if err := rows.Scan(&value.ID, &value.TenantID, &value.AgendaID, &value.ClaimID, &value.Position, &value.Prompt,
			&value.State, &value.Outcome, &value.Version, &created, &updated); err != nil {
			return nil, mapSQLError("scan agenda item", err)
		}
		if value.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		if value.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, mapSQLError("iterate agenda items", rows.Err())
}

func (s *Store) UpdateAgendaItem(ctx context.Context, value seminar.Item, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE agenda_items SET position=?, prompt=?, state=?, outcome=?, version=?, updated_at=?
        WHERE tenant_id=? AND id=? AND version=?`, value.Position, value.Prompt, value.State, value.Outcome, value.Version,
		formatTime(value.UpdatedAt), value.TenantID, value.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update agenda item", err)
	}
	return requireUpdated(result, "update agenda item")
}

func (s *Store) InsertArchiveJob(ctx context.Context, value archive.Job) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO archive_jobs
        (id, tenant_id, program_id, state, lease_owner, lease_token, lease_until, attempt, max_attempts, next_try_at, last_error, snapshot_hash, version, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.ProgramID, value.State,
		value.LeaseOwner, value.LeaseToken, optionalTime(value.LeaseUntil), value.Attempt, value.MaxAttempts, formatTime(value.NextTryAt), value.LastError,
		value.SnapshotHash, value.Version, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	return mapSQLError("insert archive job", err)
}

func (s *Store) GetArchiveJob(ctx context.Context, tenantID, id string) (archive.Job, error) {
	var value archive.Job
	var leaseUntil sql.NullString
	var nextTry, created, updated string
	err := s.q.QueryRowContext(ctx, `SELECT id, tenant_id, program_id, state, lease_owner, lease_token, lease_until, attempt, max_attempts, next_try_at, last_error, snapshot_hash, version, created_at, updated_at
        FROM archive_jobs WHERE tenant_id=? AND id=?`, tenantID, id).Scan(&value.ID, &value.TenantID, &value.ProgramID, &value.State,
		&value.LeaseOwner, &value.LeaseToken, &leaseUntil, &value.Attempt, &value.MaxAttempts, &nextTry, &value.LastError,
		&value.SnapshotHash, &value.Version, &created, &updated)
	if err != nil {
		return archive.Job{}, mapSQLError("get archive job", err)
	}
	if leaseUntil.Valid {
		if value.LeaseUntil, err = parseTime(leaseUntil.String); err != nil {
			return archive.Job{}, err
		}
	}
	if value.NextTryAt, err = parseTime(nextTry); err != nil {
		return archive.Job{}, err
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return archive.Job{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err
}

func (s *Store) UpdateArchiveJob(ctx context.Context, value archive.Job, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE archive_jobs SET state=?, lease_owner=?, lease_token=?, lease_until=?, attempt=?, max_attempts=?, next_try_at=?, last_error=?, snapshot_hash=?, version=?, updated_at=?
        WHERE tenant_id=? AND id=? AND version=?`, value.State, value.LeaseOwner, value.LeaseToken, optionalTime(value.LeaseUntil), value.Attempt,
		value.MaxAttempts, formatTime(value.NextTryAt), value.LastError, value.SnapshotHash, value.Version, formatTime(value.UpdatedAt), value.TenantID, value.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update archive job", err)
	}
	return requireUpdated(result, "update archive job")
}

func (s *Store) InsertArchiveSnapshot(ctx context.Context, value repository.ArchiveSnapshot) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO archive_snapshots(id, tenant_id, program_id, hash, payload, created_at)
        VALUES (?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.ProgramID, value.Hash, value.Payload, formatTime(value.CreatedAt))
	return mapSQLError("insert archive snapshot", err)
}

func (s *Store) GetArchiveSnapshot(ctx context.Context, tenantID, programID string) (repository.ArchiveSnapshot, error) {
	var value repository.ArchiveSnapshot
	var created string
	err := s.q.QueryRowContext(ctx, `SELECT id, tenant_id, program_id, hash, payload, created_at FROM archive_snapshots
        WHERE tenant_id=? AND program_id=?`, tenantID, programID).Scan(&value.ID, &value.TenantID, &value.ProgramID, &value.Hash, &value.Payload, &created)
	if err != nil {
		return repository.ArchiveSnapshot{}, mapSQLError("get archive snapshot", err)
	}
	value.CreatedAt, err = parseTime(created)
	if err != nil {
		return repository.ArchiveSnapshot{}, err
	}
	value.Payload = append([]byte(nil), value.Payload...)
	return value, nil
}
