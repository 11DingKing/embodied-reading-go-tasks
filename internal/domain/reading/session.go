package reading

import (
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type SessionState string

const (
	SessionPrepared  SessionState = "prepared"
	SessionActive    SessionState = "active"
	SessionSubmitted SessionState = "submitted"
	SessionAccepted  SessionState = "accepted"
	SessionAbandoned SessionState = "abandoned"
	SessionReopened  SessionState = "reopened"
)

type Session struct {
	ID              string
	TenantID        string
	ProgramID       string
	AssignmentID    string
	MemberID        string
	State           SessionState
	Intention       string
	StartedAt       *time.Time
	SubmittedAt     *time.Time
	AcceptedAt      *time.Time
	AbandonedReason string
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func NewSession(id, tenantID, programID, assignmentID, memberID, intention string, now time.Time) (Session, error) {
	if id == "" || tenantID == "" || programID == "" || assignmentID == "" || memberID == "" {
		return Session{}, fault.Invalid("reading_session", "requires all ownership identifiers")
	}
	if len(strings.TrimSpace(intention)) < 8 {
		return Session{}, fault.Invalid("intention", "must describe the reading purpose")
	}
	now = now.UTC()
	return Session{
		ID: id, TenantID: tenantID, ProgramID: programID, AssignmentID: assignmentID, MemberID: memberID,
		State: SessionPrepared, Intention: strings.TrimSpace(intention), Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Session) Start(now time.Time) error {
	if s.State != SessionPrepared && s.State != SessionReopened {
		return fault.StateConflict("reading_session", string(s.State), string(SessionActive))
	}
	now = now.UTC()
	s.State = SessionActive
	s.StartedAt = &now
	s.Version++
	s.UpdatedAt = now
	return nil
}

func (s *Session) Submit(now time.Time) error {
	if s.State != SessionActive {
		return fault.StateConflict("reading_session", string(s.State), string(SessionSubmitted))
	}
	now = now.UTC()
	s.State = SessionSubmitted
	s.SubmittedAt = &now
	s.Version++
	s.UpdatedAt = now
	return nil
}

func (s *Session) Accept(now time.Time) error {
	if s.State != SessionSubmitted {
		return fault.StateConflict("reading_session", string(s.State), string(SessionAccepted))
	}
	now = now.UTC()
	s.State = SessionAccepted
	s.AcceptedAt = &now
	s.Version++
	s.UpdatedAt = now
	return nil
}

func (s *Session) Abandon(reason string, now time.Time) error {
	if s.State != SessionPrepared && s.State != SessionActive && s.State != SessionReopened {
		return fault.StateConflict("reading_session", string(s.State), string(SessionAbandoned))
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 5 {
		return fault.Invalid("abandon_reason", "must explain why the session ended")
	}
	s.State = SessionAbandoned
	s.AbandonedReason = reason
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}

func (s *Session) Reopen(now time.Time) error {
	if s.State != SessionSubmitted {
		return fault.StateConflict("reading_session", string(s.State), string(SessionReopened))
	}
	s.State = SessionReopened
	s.SubmittedAt = nil
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}
