package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
)

func (s *Store) EnsureTenant(ctx context.Context, tenantID, name string, now time.Time) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO tenants(id, name, created_at) VALUES (?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET name=excluded.name`, tenantID, name, formatTime(now))
	return mapSQLError("ensure tenant", err)
}

func (s *Store) InsertUser(ctx context.Context, user identity.User) error {
	roles, err := json.Marshal(user.Roles)
	if err != nil {
		return fault.Wrap(fault.Internal, "role_encoding_failed", "encode user roles", err)
	}
	_, err = s.q.ExecContext(ctx, `INSERT INTO users
        (id, tenant_id, email, display_name, password_hash, roles_json, active, version, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.TenantID, strings.ToLower(user.Email), user.DisplayName, user.PasswordHash, string(roles), boolInt(user.Active), user.Version, formatTime(user.CreatedAt), formatTime(user.UpdatedAt))
	return mapSQLError("insert user", err)
}

func (s *Store) GetUser(ctx context.Context, tenantID, id string) (identity.User, error) {
	return scanUser(s.q.QueryRowContext(ctx, `SELECT id, tenant_id, email, display_name, password_hash, roles_json, active, version, created_at, updated_at
        FROM users WHERE tenant_id = ? AND id = ?`, tenantID, id))
}

func (s *Store) GetUserByEmail(ctx context.Context, tenantID, email string) (identity.User, error) {
	return scanUser(s.q.QueryRowContext(ctx, `SELECT id, tenant_id, email, display_name, password_hash, roles_json, active, version, created_at, updated_at
        FROM users WHERE tenant_id = ? AND email = ?`, tenantID, strings.ToLower(strings.TrimSpace(email))))
}

func scanUser(row *sql.Row) (identity.User, error) {
	var user identity.User
	var rolesJSON, created, updated string
	var active int
	err := row.Scan(&user.ID, &user.TenantID, &user.Email, &user.DisplayName, &user.PasswordHash, &rolesJSON, &active, &user.Version, &created, &updated)
	if err != nil {
		return identity.User{}, mapSQLError("get user", err)
	}
	if err := json.Unmarshal([]byte(rolesJSON), &user.Roles); err != nil {
		return identity.User{}, fault.Wrap(fault.Dependency, "invalid_persisted_roles", "decode user roles", err)
	}
	user.Active = active == 1
	user.CreatedAt, err = parseTime(created)
	if err != nil {
		return identity.User{}, err
	}
	user.UpdatedAt, err = parseTime(updated)
	return user, err
}

func (s *Store) UpdateUser(ctx context.Context, user identity.User, expectedVersion int64) error {
	roles, err := json.Marshal(user.Roles)
	if err != nil {
		return fault.Wrap(fault.Internal, "role_encoding_failed", "encode user roles", err)
	}
	result, err := s.q.ExecContext(ctx, `UPDATE users SET email=?, display_name=?, password_hash=?, roles_json=?, active=?, version=?, updated_at=?
        WHERE tenant_id=? AND id=? AND version=?`, user.Email, user.DisplayName, user.PasswordHash, string(roles), boolInt(user.Active), user.Version, formatTime(user.UpdatedAt), user.TenantID, user.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update user", err)
	}
	return requireUpdated(result, "update user")
}

func (s *Store) InsertSession(ctx context.Context, session identity.Session) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO auth_sessions
        (id, tenant_id, user_id, token_hash, state, expires_at, last_seen_at, created_at, revoked_at, version)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, session.ID, session.TenantID, session.UserID, session.TokenHash, session.State,
		formatTime(session.ExpiresAt), formatTime(session.LastSeenAt), formatTime(session.CreatedAt), optionalTimePointer(session.RevokedAt), session.Version)
	return mapSQLError("insert auth session", err)
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (identity.Session, error) {
	var value identity.Session
	var expires, lastSeen, created string
	var revoked sql.NullString
	err := s.q.QueryRowContext(ctx, `SELECT id, tenant_id, user_id, token_hash, state, expires_at, last_seen_at, created_at, revoked_at, version
        FROM auth_sessions WHERE token_hash = ?`, tokenHash).Scan(&value.ID, &value.TenantID, &value.UserID, &value.TokenHash, &value.State, &expires, &lastSeen, &created, &revoked, &value.Version)
	if err != nil {
		return identity.Session{}, mapSQLError("get auth session", err)
	}
	if value.ExpiresAt, err = parseTime(expires); err != nil {
		return identity.Session{}, err
	}
	if value.LastSeenAt, err = parseTime(lastSeen); err != nil {
		return identity.Session{}, err
	}
	if value.CreatedAt, err = parseTime(created); err != nil {
		return identity.Session{}, err
	}
	value.RevokedAt, err = scanOptionalTime(revoked)
	return value, err
}

func (s *Store) UpdateSession(ctx context.Context, session identity.Session, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE auth_sessions SET state=?, expires_at=?, last_seen_at=?, revoked_at=?, version=?
        WHERE tenant_id=? AND id=? AND version=?`, session.State, formatTime(session.ExpiresAt), formatTime(session.LastSeenAt), optionalTimePointer(session.RevokedAt), session.Version, session.TenantID, session.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update auth session", err)
	}
	return requireUpdated(result, "update auth session")
}

func (s *Store) RevokeSessionsForUser(ctx context.Context, tenantID, userID string, now time.Time) (int, error) {
	result, err := s.q.ExecContext(ctx, `UPDATE auth_sessions SET state=?, revoked_at=?, version=version+1
        WHERE tenant_id=? AND user_id=? AND state=?`, identity.SessionRevoked, formatTime(now), tenantID, userID, identity.SessionActive)
	if err != nil {
		return 0, mapSQLError("revoke user sessions", err)
	}
	count, err := result.RowsAffected()
	return int(count), mapSQLError("count revoked sessions", err)
}

func (s *Store) InsertWork(ctx context.Context, work catalog.Work) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO works(id, tenant_id, title, author, description, version, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, work.ID, work.TenantID, work.Title, work.Author, work.Description, work.Version, formatTime(work.CreatedAt), formatTime(work.UpdatedAt))
	return mapSQLError("insert work", err)
}

func (s *Store) GetWork(ctx context.Context, tenantID, id string) (catalog.Work, error) {
	var value catalog.Work
	var created, updated string
	err := s.q.QueryRowContext(ctx, `SELECT id, tenant_id, title, author, description, version, created_at, updated_at
        FROM works WHERE tenant_id=? AND id=?`, tenantID, id).Scan(&value.ID, &value.TenantID, &value.Title, &value.Author, &value.Description, &value.Version, &created, &updated)
	if err != nil {
		return catalog.Work{}, mapSQLError("get work", err)
	}
	value.CreatedAt, err = parseTime(created)
	if err != nil {
		return catalog.Work{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err
}

func (s *Store) UpdateWork(ctx context.Context, work catalog.Work, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE works SET title=?, author=?, description=?, version=?, updated_at=?
        WHERE tenant_id=? AND id=? AND version=?`, work.Title, work.Author, work.Description, work.Version, formatTime(work.UpdatedAt), work.TenantID, work.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update work", err)
	}
	return requireUpdated(result, "update work")
}

func (s *Store) InsertEdition(ctx context.Context, edition catalog.Edition) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO editions
        (id, tenant_id, work_id, label, publisher, published_at, page_count, fingerprint, state, version, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, edition.ID, edition.TenantID, edition.WorkID, edition.Label, edition.Publisher,
		formatTime(edition.PublishedAt), edition.PageCount, edition.Fingerprint, edition.State, edition.Version, formatTime(edition.CreatedAt), formatTime(edition.UpdatedAt))
	return mapSQLError("insert edition", err)
}

func (s *Store) GetEdition(ctx context.Context, tenantID, id string) (catalog.Edition, error) {
	var value catalog.Edition
	var published, created, updated string
	err := s.q.QueryRowContext(ctx, `SELECT id, tenant_id, work_id, label, publisher, published_at, page_count, fingerprint, state, version, created_at, updated_at
        FROM editions WHERE tenant_id=? AND id=?`, tenantID, id).Scan(&value.ID, &value.TenantID, &value.WorkID, &value.Label, &value.Publisher, &published, &value.PageCount, &value.Fingerprint, &value.State, &value.Version, &created, &updated)
	if err != nil {
		return catalog.Edition{}, mapSQLError("get edition", err)
	}
	value.PublishedAt, err = parseTime(published)
	if err != nil {
		return catalog.Edition{}, err
	}
	value.CreatedAt, err = parseTime(created)
	if err != nil {
		return catalog.Edition{}, err
	}
	value.UpdatedAt, err = parseTime(updated)
	return value, err
}

func (s *Store) UpdateEdition(ctx context.Context, edition catalog.Edition, expectedVersion int64) error {
	result, err := s.q.ExecContext(ctx, `UPDATE editions SET label=?, publisher=?, published_at=?, page_count=?, fingerprint=?, state=?, version=?, updated_at=?
        WHERE tenant_id=? AND id=? AND version=?`, edition.Label, edition.Publisher, formatTime(edition.PublishedAt), edition.PageCount, edition.Fingerprint,
		edition.State, edition.Version, formatTime(edition.UpdatedAt), edition.TenantID, edition.ID, expectedVersion)
	if err != nil {
		return mapSQLError("update edition", err)
	}
	return requireUpdated(result, "update edition")
}
