package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/archive"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/evidence"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/review"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/seminar"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

const claimColumns = `id, tenant_id, session_id, edition_id, author_id, page_start, page_end, quoted_text, interpretation, state, current_review_id, version, created_at, updated_at`

func (s *Store) InsertClaim(ctx context.Context, value evidence.Claim) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO quotation_claims
        (`+claimColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.SessionID,
		value.EditionID, value.AuthorID, value.PageStart, value.PageEnd, value.QuotedText, value.Interpretation, value.State,
		value.CurrentReviewID, value.Version, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	return mapSQLError("insert quotation claim", err)
}

func scanClaim(scanner interface{ Scan(...any) error }) (evidence.Claim, error) {
	var value evidence.Claim
	var created, updated string
	err := scanner.Scan(&value.ID, &value.TenantID, &value.SessionID, &value.EditionID, &value.AuthorID, &value.PageStart, &value.PageEnd,
		&value.QuotedText, &value.Interpretation, &value.State, &value.CurrentReviewID, &value.Version, &created, &updated)
	if err != nil {
		return evidence.Claim{}, mapSQLError("scan quotation claim", err)
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return evidence.Claim{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err
}

func (s *Store) GetClaim(ctx context.Context, tenantID, id string) (evidence.Claim, error) {
	return scanClaim(s.q.QueryRowContext(ctx, `SELECT `+claimColumns+` FROM quotation_claims WHERE tenant_id=? AND id=?`, tenantID, id))
}

func (s *Store) ListClaimsBySession(ctx context.Context, tenantID, sessionID string) ([]evidence.Claim, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+claimColumns+` FROM quotation_claims WHERE tenant_id=? AND session_id=? ORDER BY created_at, id`, tenantID, sessionID)
	if err != nil {
		return nil, mapSQLError("list quotation claims", err)
	}
	defer rows.Close()
	var values []evidence.Claim
	for rows.Next() {
		value, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, mapSQLError("iterate quotation claims", rows.Err())
}

func (s *Store) UpdateClaim(ctx context.Context, value evidence.Claim, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE quotation_claims SET page_start=?, page_end=?, quoted_text=?, interpretation=?, state=?, current_review_id=?, version=?, updated_at=?
        WHERE tenant_id=? AND id=? AND version=?`, value.PageStart, value.PageEnd, value.QuotedText, value.Interpretation, value.State,
		value.CurrentReviewID, value.Version, formatTime(value.UpdatedAt), value.TenantID, value.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update quotation claim", err)
	}
	return requireUpdated(result, "update quotation claim")
}

func (s *Store) InsertDispute(ctx context.Context, value evidence.Dispute) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO claim_disputes
        (id, tenant_id, claim_id, opened_by, reason, state, resolver_id, resolution, version, opened_at, resolved_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.ClaimID, value.OpenedBy, value.Reason,
		value.State, value.ResolverID, value.Resolution, value.Version, formatTime(value.OpenedAt), optionalTimePointer(value.ResolvedAt))
	return mapSQLError("insert claim dispute", err)
}

func (s *Store) GetDispute(ctx context.Context, tenantID, id string) (evidence.Dispute, error) {
	var value evidence.Dispute
	var opened string
	var resolved sql.NullString
	err := s.q.QueryRowContext(ctx, `SELECT id, tenant_id, claim_id, opened_by, reason, state, resolver_id, resolution, version, opened_at, resolved_at
        FROM claim_disputes WHERE tenant_id=? AND id=?`, tenantID, id).Scan(&value.ID, &value.TenantID, &value.ClaimID, &value.OpenedBy,
		&value.Reason, &value.State, &value.ResolverID, &value.Resolution, &value.Version, &opened, &resolved)
	if err != nil {
		return evidence.Dispute{}, mapSQLError("get claim dispute", err)
	}
	if value.OpenedAt, err = parseTime(opened); err != nil {
		return evidence.Dispute{}, err
	}
	value.ResolvedAt, err = scanOptionalTime(resolved)
	return value, err
}

func (s *Store) UpdateDispute(ctx context.Context, value evidence.Dispute, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE claim_disputes SET state=?, resolver_id=?, resolution=?, version=?, resolved_at=?
        WHERE tenant_id=? AND id=? AND version=?`, value.State, value.ResolverID, value.Resolution, value.Version,
		optionalTimePointer(value.ResolvedAt), value.TenantID, value.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update claim dispute", err)
	}
	return requireUpdated(result, "update claim dispute")
}

func (s *Store) CountOpenDisputes(ctx context.Context, tenantID, programID string) (int, error) {
	var count int
	err := s.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM claim_disputes d
        JOIN quotation_claims c ON c.id=d.claim_id AND c.tenant_id=d.tenant_id
        JOIN reading_sessions r ON r.id=c.session_id AND r.tenant_id=c.tenant_id
        WHERE d.tenant_id=? AND r.program_id=? AND d.state IN (?, ?)`, tenantID, programID, evidence.DisputeOpen, evidence.DisputeAssigned).Scan(&count)
	return count, mapSQLError("count open disputes", err)
}

const reviewColumns = `id, tenant_id, claim_id, claim_author_id, reviewer_id, state, lease_owner, lease_token, lease_until, attempt, version, created_at, updated_at`

func (s *Store) InsertReviewAssignment(ctx context.Context, value review.Assignment) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO review_assignments (`+reviewColumns+`)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.ClaimID, value.ClaimAuthorID, value.ReviewerID,
		value.State, value.LeaseOwner, value.LeaseToken, optionalTime(value.LeaseUntil), value.Attempt, value.Version, formatTime(value.CreatedAt), formatTime(value.UpdatedAt))
	return mapSQLError("insert review assignment", err)
}

func scanReviewAssignment(scanner interface{ Scan(...any) error }) (review.Assignment, error) {
	var value review.Assignment
	var leaseUntil sql.NullString
	var created, updated string
	err := scanner.Scan(&value.ID, &value.TenantID, &value.ClaimID, &value.ClaimAuthorID, &value.ReviewerID, &value.State,
		&value.LeaseOwner, &value.LeaseToken, &leaseUntil, &value.Attempt, &value.Version, &created, &updated)
	if err != nil {
		return review.Assignment{}, mapSQLError("scan review assignment", err)
	}
	if leaseUntil.Valid {
		if value.LeaseUntil, err = parseTime(leaseUntil.String); err != nil {
			return review.Assignment{}, err
		}
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return review.Assignment{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err
}

func (s *Store) GetReviewAssignment(ctx context.Context, tenantID, id string) (review.Assignment, error) {
	return scanReviewAssignment(s.q.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM review_assignments WHERE tenant_id=? AND id=?`, tenantID, id))
}

func (s *Store) UpdateReviewAssignment(ctx context.Context, value review.Assignment, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE review_assignments SET reviewer_id=?, state=?, lease_owner=?, lease_token=?, lease_until=?, attempt=?, version=?, updated_at=?
        WHERE tenant_id=? AND id=? AND version=?`, value.ReviewerID, value.State, value.LeaseOwner, value.LeaseToken, optionalTime(value.LeaseUntil),
		value.Attempt, value.Version, formatTime(value.UpdatedAt), value.TenantID, value.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update review assignment", err)
	}
	return requireUpdated(result, "update review assignment")
}

func (s *Store) InsertReviewDecision(ctx context.Context, value review.Decision) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO review_decisions(id, tenant_id, assignment_id, claim_id, reviewer_id, outcome, rationale, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, value.ID, value.TenantID, value.AssignmentID, value.ClaimID, value.ReviewerID, value.Outcome, value.Rationale, formatTime(value.CreatedAt))
	return mapSQLError("insert review decision", err)
}

func (s *Store) ListReviewQueue(ctx context.Context, filter repository.ReviewQueueFilter) (repository.ReviewQueuePage, error) {
	page := filter.Page.Normalize()
	states := filter.States
	if len(states) == 0 {
		states = []review.AssignmentState{review.Queued, review.Leased, review.Expired}
	}
	placeholders := make([]string, len(states))
	args := []any{filter.TenantID, filter.ReviewerID}
	for i, state := range states {
		placeholders[i] = "?"
		args = append(args, state)
	}
	where := `tenant_id=? AND reviewer_id=? AND state IN (` + strings.Join(placeholders, ",") + `)`
	if filter.DueBefore != nil {
		where += ` AND (lease_until IS NULL OR lease_until <= ?)`
		args = append(args, formatTime(*filter.DueBefore))
	}
	var total int
	if err := s.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_assignments WHERE `+where, args...).Scan(&total); err != nil {
		return repository.ReviewQueuePage{}, mapSQLError("count review queue", err)
	}
	listArgs := append(append([]any(nil), args...), page.Limit, page.Offset)
	rows, err := s.q.QueryContext(ctx, `SELECT `+reviewColumns+` FROM review_assignments WHERE `+where+` ORDER BY created_at, id LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return repository.ReviewQueuePage{}, mapSQLError("list review queue", err)
	}
	defer rows.Close()
	values := make([]review.Assignment, 0, page.Limit)
	for rows.Next() {
		value, err := scanReviewAssignment(rows)
		if err != nil {
			return repository.ReviewQueuePage{}, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return repository.ReviewQueuePage{}, mapSQLError("iterate review queue", err)
	}
	return repository.ReviewQueuePage{Items: values, Total: total}, nil
}

func (s *Store) ListFinalClaimsForProgram(ctx context.Context, tenantID, programID string) ([]evidence.Claim, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+claimColumns+` FROM quotation_claims c
        JOIN reading_sessions r ON r.id=c.session_id AND r.tenant_id=c.tenant_id
        WHERE c.tenant_id=? AND r.program_id=? AND c.state IN (?, ?)
        ORDER BY c.created_at, c.id`, tenantID, programID, evidence.ClaimVerified, evidence.ClaimResolved)
	if err != nil {
		return nil, mapSQLError("list final claims for program", err)
	}
	defer rows.Close()
	var values []evidence.Claim
	for rows.Next() {
		value, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, mapSQLError("iterate final program claims", rows.Err())
}

func (s *Store) GetProgramReadiness(ctx context.Context, tenantID, programID string, now time.Time) (repository.ProgramReadiness, error) {
	var result repository.ProgramReadiness
	queries := []struct {
		target *int
		query  string
		args   []any
	}{
		{&result.ActiveMembers, `SELECT COUNT(*) FROM program_members WHERE tenant_id=? AND program_id=? AND state=?`, []any{tenantID, programID, program.MemberActive}},
		{&result.IncompleteMembers, `SELECT COUNT(*) FROM program_members WHERE tenant_id=? AND program_id=? AND state NOT IN (?, ?)`, []any{tenantID, programID, program.MemberCompleted, program.MemberWithdrawn}},
		{&result.ActiveAssignments, `SELECT COUNT(*) FROM section_assignments WHERE tenant_id=? AND program_id=? AND state IN (?, ?)`, []any{tenantID, programID, reading.AssignmentReserved, reading.AssignmentInReading}},
		{&result.OpenReadingSessions, `SELECT COUNT(*) FROM reading_sessions WHERE tenant_id=? AND program_id=? AND state NOT IN (?, ?)`, []any{tenantID, programID, reading.SessionAccepted, reading.SessionAbandoned}},
		{&result.UnresolvedClaims, `SELECT COUNT(*) FROM quotation_claims c JOIN reading_sessions r ON r.id=c.session_id AND r.tenant_id=c.tenant_id WHERE c.tenant_id=? AND r.program_id=? AND c.state NOT IN (?, ?)`, []any{tenantID, programID, evidence.ClaimVerified, evidence.ClaimResolved}},
		{&result.OpenDisputes, `SELECT COUNT(*) FROM claim_disputes d JOIN quotation_claims c ON c.id=d.claim_id AND c.tenant_id=d.tenant_id JOIN reading_sessions r ON r.id=c.session_id AND r.tenant_id=c.tenant_id WHERE d.tenant_id=? AND r.program_id=? AND d.state IN (?, ?)`, []any{tenantID, programID, evidence.DisputeOpen, evidence.DisputeAssigned}},
		{&result.UnresolvedAgenda, `SELECT COUNT(*) FROM agenda_items i JOIN seminar_agendas a ON a.id=i.agenda_id AND a.tenant_id=i.tenant_id WHERE i.tenant_id=? AND a.program_id=? AND i.state=?`, []any{tenantID, programID, seminar.ItemPending}},
		{&result.ActiveWorkerLeases, `SELECT (SELECT COUNT(*) FROM archive_jobs WHERE tenant_id=? AND program_id=? AND state IN (?, ?) AND lease_until>?) + (SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND aggregate_id=? AND state=? AND lease_until>?)`, []any{tenantID, programID, archive.Leased, archive.Writing, formatTime(now), tenantID, programID, repository.OutboxLeased, formatTime(now)}},
	}
	for _, item := range queries {
		if err := s.q.QueryRowContext(ctx, item.query, item.args...).Scan(item.target); err != nil {
			return repository.ProgramReadiness{}, mapSQLError("read program readiness", err)
		}
	}
	return result, nil
}
