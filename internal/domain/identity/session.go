package identity

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type SessionState string

const (
	SessionActive  SessionState = "active"
	SessionRevoked SessionState = "revoked"
	SessionExpired SessionState = "expired"
)

type Session struct {
	ID         string
	TenantID   string
	UserID     string
	TokenHash  string
	State      SessionState
	ExpiresAt  time.Time
	LastSeenAt time.Time
	CreatedAt  time.Time
	RevokedAt  *time.Time
	Version    int64
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func TokenMatches(hash, token string) bool {
	want, err := hex.DecodeString(hash)
	if err != nil {
		return false
	}
	got := sha256.Sum256([]byte(token))
	return len(want) == len(got) && subtle.ConstantTimeCompare(want, got[:]) == 1
}

func NewSession(id, tenantID, userID, token string, now time.Time, ttl time.Duration) (Session, error) {
	if id == "" || tenantID == "" || userID == "" || token == "" {
		return Session{}, fault.Invalid("session", "requires id, tenant, user, and token")
	}
	if ttl <= 0 {
		return Session{}, fault.Invalid("session_ttl", "must be positive")
	}
	now = now.UTC()
	return Session{
		ID: id, TenantID: tenantID, UserID: userID, TokenHash: HashToken(token),
		State: SessionActive, ExpiresAt: now.Add(ttl), LastSeenAt: now, CreatedAt: now, Version: 1,
	}, nil
}

func (s *Session) Validate(now time.Time) error {
	if s.State == SessionRevoked {
		return fault.New(fault.Unauthorized, "session_revoked", "session has been revoked")
	}
	if s.State == SessionExpired || !now.UTC().Before(s.ExpiresAt) {
		s.State = SessionExpired
		return fault.New(fault.Unauthorized, "session_expired", "session has expired")
	}
	return nil
}

func (s *Session) Touch(now time.Time, minimumInterval time.Duration) bool {
	now = now.UTC()
	if now.Sub(s.LastSeenAt) < minimumInterval {
		return false
	}
	s.LastSeenAt = now
	s.Version++
	return true
}

func (s *Session) Revoke(now time.Time) error {
	if s.State == SessionRevoked {
		return nil
	}
	if s.State == SessionExpired {
		return fault.StateConflict("session", string(s.State), string(SessionRevoked))
	}
	now = now.UTC()
	s.State = SessionRevoked
	s.RevokedAt = &now
	s.Version++
	return nil
}
