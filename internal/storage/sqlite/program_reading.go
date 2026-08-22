package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

func (s *Store) InsertProgram(ctx context.Context, value program.Program) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO study_programs
        (id, tenant_id, edition_id, coordinator_id, name, timezone, capacity, starts_at, ends_at, state, version, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.EditionID, value.CoordinatorID,
		value.Name, value.TimeZone, value.Capacity, formatTime(value.StartsAt), formatTime(value.EndsAt), value.State, value.Version,
		formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	return mapSQLError("insert study program", err)
}

func (s *Store) GetProgram(ctx context.Context, tenantID, id string) (program.Program, error) {
	var value program.Program
	var starts, ends, created, updated string
	err := s.q.QueryRowContext(ctx, `SELECT id, tenant_id, edition_id, coordinator_id, name, timezone, capacity, starts_at, ends_at, state, version, created_at, updated_at
        FROM study_programs WHERE tenant_id=? AND id=?`, tenantID, id).Scan(&value.ID, &value.TenantID, &value.EditionID, &value.CoordinatorID,
		&value.Name, &value.TimeZone, &value.Capacity, &starts, &ends, &value.State, &value.Version, &created, &updated)
	if err != nil {
		return program.Program{}, mapSQLError("get study program", err)
	}
	if value.StartsAt, err = parseTime(starts); err != nil {
		return program.Program{}, err
	}
	if value.EndsAt, err = parseTime(ends); err != nil {
		return program.Program{}, err
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return program.Program{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err
}

func (s *Store) UpdateProgram(ctx context.Context, value program.Program, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE study_programs SET edition_id=?, coordinator_id=?, name=?, timezone=?, capacity=?, starts_at=?, ends_at=?, state=?, version=?, updated_at=?
        WHERE tenant_id=? AND id=? AND version=?`, value.EditionID, value.CoordinatorID, value.Name, value.TimeZone, value.Capacity,
		formatTime(value.StartsAt), formatTime(value.EndsAt), value.State, value.Version, formatTime(value.UpdatedAt), value.TenantID, value.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update study program", err)
	}
	return requireUpdated(result, "update study program")
}

func (s *Store) InsertMember(ctx context.Context, member program.Member) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO program_members(id, tenant_id, program_id, user_id, state, joined_at, updated_at, version)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, member.ID, member.TenantID, member.ProgramID, member.UserID, member.State, formatTime(member.JoinedAt), formatTime(member.UpdatedAt), member.Version)
	return mapSQLError("insert program member", err)
}

func (s *Store) GetMember(ctx context.Context, tenantID, programID, userID string) (program.Member, error) {
	var value program.Member
	var joined, updated string
	err := s.q.QueryRowContext(ctx, `SELECT id, tenant_id, program_id, user_id, state, joined_at, updated_at, version
        FROM program_members WHERE tenant_id=? AND program_id=? AND user_id=?`, tenantID, programID, userID).Scan(&value.ID, &value.TenantID, &value.ProgramID, &value.UserID, &value.State, &joined, &updated, &value.Version)
	if err != nil {
		return program.Member{}, mapSQLError("get program member", err)
	}
	if value.JoinedAt, err = parseTime(joined); err != nil {
		return program.Member{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err
}

func (s *Store) CountActiveMembers(ctx context.Context, tenantID, programID string) (int, error) {
	var count int
	err := s.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM program_members WHERE tenant_id=? AND program_id=? AND state=?`, tenantID, programID, program.MemberActive).Scan(&count)
	return count, mapSQLError("count active members", err)
}

func (s *Store) UpdateMember(ctx context.Context, member program.Member, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE program_members SET state=?, updated_at=?, version=? WHERE tenant_id=? AND id=? AND version=?`,
		member.State, formatTime(member.UpdatedAt), member.Version, member.TenantID, member.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update program member", err)
	}
	return requireUpdated(result, "update program member")
}

func (s *Store) InsertAssignment(ctx context.Context, value reading.Assignment) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO section_assignments
        (id, tenant_id, program_id, edition_id, member_id, page_start, page_end, window_start, window_end, state, lease_owner, lease_token, lease_until, version, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.ProgramID, value.EditionID, value.MemberID,
		value.Pages.Start, value.Pages.End, formatTime(value.WindowStart), formatTime(value.WindowEnd), value.State, value.LeaseOwner, value.LeaseToken,
		optionalTime(value.LeaseUntil), value.Version, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	return mapSQLError("insert section assignment", err)
}

func scanAssignment(scanner interface{ Scan(...any) error }) (reading.Assignment, error) {
	var value reading.Assignment
	var start, end, created, updated string
	var leaseUntil sql.NullString
	err := scanner.Scan(&value.ID, &value.TenantID, &value.ProgramID, &value.EditionID, &value.MemberID, &value.Pages.Start, &value.Pages.End,
		&start, &end, &value.State, &value.LeaseOwner, &value.LeaseToken, &leaseUntil, &value.Version, &created, &updated)
	if err != nil {
		return reading.Assignment{}, mapSQLError("scan section assignment", err)
	}
	if value.WindowStart, err = parseTime(start); err != nil {
		return reading.Assignment{}, err
	}
	if value.WindowEnd, err = parseTime(end); err != nil {
		return reading.Assignment{}, err
	}
	if leaseUntil.Valid {
		if value.LeaseUntil, err = parseTime(leaseUntil.String); err != nil {
			return reading.Assignment{}, err
		}
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return reading.Assignment{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err
}

const assignmentColumns = `id, tenant_id, program_id, edition_id, member_id, page_start, page_end, window_start, window_end, state, lease_owner, lease_token, lease_until, version, created_at, updated_at`

func (s *Store) GetAssignment(ctx context.Context, tenantID, id string) (reading.Assignment, error) {
	return scanAssignment(s.q.QueryRowContext(ctx, `SELECT `+assignmentColumns+` FROM section_assignments WHERE tenant_id=? AND id=?`, tenantID, id))
}

func (s *Store) FindAssignmentConflicts(ctx context.Context, tenantID, programID, editionID string, pages reading.PageRange, start, end time.Time, excludeID string) ([]reading.Assignment, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+assignmentColumns+` FROM section_assignments
        WHERE tenant_id=? AND program_id=? AND edition_id=? AND id<>?
          AND state NOT IN (?, ?) AND window_start < ? AND window_end > ? AND page_start <= ? AND page_end >= ?
        ORDER BY window_start, page_start`, tenantID, programID, editionID, excludeID, reading.AssignmentReleased, reading.AssignmentExpired,
		formatTime(end), formatTime(start), pages.End, pages.Start)
	if err != nil {
		return nil, mapSQLError("find assignment conflicts", err)
	}
	defer rows.Close()
	var values []reading.Assignment
	for rows.Next() {
		value, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, mapSQLError("iterate assignment conflicts", rows.Err())
}

func (s *Store) ListExpiredAssignments(ctx context.Context, now time.Time, page repository.Page) ([]reading.Assignment, error) {
	page = page.Normalize()
	rows, err := s.q.QueryContext(ctx, `SELECT `+assignmentColumns+` FROM section_assignments
        WHERE state IN (?, ?) AND (window_end <= ? OR (lease_until IS NOT NULL AND lease_until <= ?))
        ORDER BY window_end, id LIMIT ? OFFSET ?`, reading.AssignmentReserved, reading.AssignmentInReading, formatTime(now), formatTime(now), page.Limit, page.Offset)
	if err != nil {
		return nil, mapSQLError("list expired assignments", err)
	}
	defer rows.Close()
	values := make([]reading.Assignment, 0, page.Limit)
	for rows.Next() {
		value, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, mapSQLError("iterate expired assignments", rows.Err())
}

func (s *Store) UpdateAssignment(ctx context.Context, value reading.Assignment, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE section_assignments SET member_id=?, page_start=?, page_end=?, window_start=?, window_end=?, state=?, lease_owner=?, lease_token=?, lease_until=?, version=?, updated_at=?
        WHERE tenant_id=? AND id=? AND version=?`, value.MemberID, value.Pages.Start, value.Pages.End, formatTime(value.WindowStart), formatTime(value.WindowEnd), value.State,
		value.LeaseOwner, value.LeaseToken, optionalTime(value.LeaseUntil), value.Version, formatTime(value.UpdatedAt), value.TenantID, value.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update section assignment", err)
	}
	return requireUpdated(result, "update section assignment")
}

func (s *Store) InsertReadingSession(ctx context.Context, value reading.Session) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO reading_sessions
        (id, tenant_id, program_id, assignment_id, member_id, state, intention, started_at, submitted_at, accepted_at, abandoned_reason, version, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.ProgramID, value.AssignmentID, value.MemberID,
		value.State, value.Intention, optionalTimePointer(value.StartedAt), optionalTimePointer(value.SubmittedAt), optionalTimePointer(value.AcceptedAt), value.AbandonedReason,
		value.Version, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	return mapSQLError("insert reading session", err)
}

func (s *Store) GetReadingSession(ctx context.Context, tenantID, id string) (reading.Session, error) {
	var value reading.Session
	var started, submitted, accepted sql.NullString
	var created, updated string
	err := s.q.QueryRowContext(ctx, `SELECT id, tenant_id, program_id, assignment_id, member_id, state, intention, started_at, submitted_at, accepted_at, abandoned_reason, version, created_at, updated_at
        FROM reading_sessions WHERE tenant_id=? AND id=?`, tenantID, id).Scan(&value.ID, &value.TenantID, &value.ProgramID, &value.AssignmentID, &value.MemberID,
		&value.State, &value.Intention, &started, &submitted, &accepted, &value.AbandonedReason, &value.Version, &created, &updated)
	if err != nil {
		return reading.Session{}, mapSQLError("get reading session", err)
	}
	if value.StartedAt, err = scanOptionalTime(started); err != nil {
		return reading.Session{}, err
	}
	if value.SubmittedAt, err = scanOptionalTime(submitted); err != nil {
		return reading.Session{}, err
	}
	if value.AcceptedAt, err = scanOptionalTime(accepted); err != nil {
		return reading.Session{}, err
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return reading.Session{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err
}

func (s *Store) UpdateReadingSession(ctx context.Context, value reading.Session, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE reading_sessions SET state=?, intention=?, started_at=?, submitted_at=?, accepted_at=?, abandoned_reason=?, version=?, updated_at=?
        WHERE tenant_id=? AND id=? AND version=?`, value.State, value.Intention, optionalTimePointer(value.StartedAt), optionalTimePointer(value.SubmittedAt), optionalTimePointer(value.AcceptedAt),
		value.AbandonedReason, value.Version, formatTime(value.UpdatedAt), value.TenantID, value.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update reading session", err)
	}
	return requireUpdated(result, "update reading session")
}

func (s *Store) InsertObservation(ctx context.Context, value reading.Observation) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO observation_notes(id, tenant_id, session_id, page, channel, body, sequence, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.SessionID, value.Page, value.Channel, value.Body, value.Sequence, formatTime(value.CreatedAt))
	return mapSQLError("insert observation", err)
}

func (s *Store) ListObservations(ctx context.Context, tenantID, sessionID string) ([]reading.Observation, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id, tenant_id, session_id, page, channel, body, sequence, created_at
        FROM observation_notes WHERE tenant_id=? AND session_id=? ORDER BY sequence`, tenantID, sessionID)
	if err != nil {
		return nil, mapSQLError("list observations", err)
	}
	defer rows.Close()
	var values []reading.Observation
	for rows.Next() {
		var value reading.Observation
		var created string
		if err := rows.Scan(&value.ID, &value.TenantID, &value.SessionID, &value.Page, &value.Channel, &value.Body, &value.Sequence, &created); err != nil {
			return nil, mapSQLError("scan observation", err)
		}
		value.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, mapSQLError("iterate observations", rows.Err())
}
