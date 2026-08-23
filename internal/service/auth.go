package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	Dependencies
	SessionTTL    time.Duration
	TouchInterval time.Duration
}

type RegisterUserInput struct {
	TenantID    string
	Email       string
	DisplayName string
	Password    string
	Roles       []identity.Role
}

func (s AuthService) BootstrapCoordinator(ctx context.Context, tenantID, tenantName, email, password string) (identity.User, error) {
	if err := s.Validate(); err != nil {
		return identity.User{}, err
	}
	if err := s.Store.EnsureTenant(ctx, tenantID, tenantName, s.Clock.Now()); err != nil {
		return identity.User{}, err
	}
	existing, err := s.Store.GetUserByEmail(ctx, tenantID, email)
	if err == nil {
		return existing, nil
	}
	if !fault.IsKind(err, fault.NotFound) {
		return identity.User{}, err
	}
	if len(password) < 12 {
		return identity.User{}, fault.Invalid("bootstrap_password", "must contain at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return identity.User{}, fault.Wrap(fault.Internal, "password_hash_failed", "hash bootstrap password", err)
	}
	now := s.Clock.Now()
	user, err := identity.NewUser(s.IDs.New("user"), tenantID, email, "Studio Coordinator", []identity.Role{identity.Coordinator, identity.Researcher}, hash, now)
	if err != nil {
		return identity.User{}, err
	}
	if err := s.Store.InsertUser(ctx, user); err != nil {
		if fault.IsKind(err, fault.Conflict) {
			return s.Store.GetUserByEmail(ctx, tenantID, email)
		}
		return identity.User{}, err
	}
	return user.Clone(), nil
}

func (s AuthService) Register(ctx context.Context, actor Actor, input RegisterUserInput) (identity.User, error) {
	if err := s.Validate(); err != nil {
		return identity.User{}, err
	}
	if err := actor.Validate(); err != nil {
		return identity.User{}, err
	}
	if actor.TenantID != input.TenantID {
		return identity.User{}, fault.New(fault.Forbidden, "cross_tenant_registration", "cannot create a user in another tenant")
	}
	coordinator, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return identity.User{}, err
	}
	if !coordinator.CanCoordinate() {
		return identity.User{}, fault.New(fault.Forbidden, "coordinator_required", "a coordinator is required")
	}
	if len(input.Password) < 12 {
		return identity.User{}, fault.Invalid("password", "must contain at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return identity.User{}, fault.Wrap(fault.Internal, "password_hash_failed", "hash password", err)
	}
	now := s.Clock.Now()
	user, err := identity.NewUser(s.IDs.New("user"), input.TenantID, input.Email, input.DisplayName, input.Roles, hash, now)
	if err != nil {
		return identity.User{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.InsertUser(ctx, user); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "user.register", "user", user.ID, audit.Succeeded, map[string]any{"roles": user.Roles}, now)
	})
	if err != nil {
		return identity.User{}, err
	}
	return user.Clone(), nil
}

type LoginInput struct {
	TenantID string
	Email    string
	Password string
}

type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	User      identity.User
}

func (s AuthService) Login(ctx context.Context, requestID string, input LoginInput) (LoginResult, error) {
	if err := s.Validate(); err != nil {
		return LoginResult{}, err
	}
	if requestID == "" {
		return LoginResult{}, fault.Invalid("request_id", "must not be empty")
	}
	user, err := s.Store.GetUserByEmail(ctx, input.TenantID, strings.ToLower(strings.TrimSpace(input.Email)))
	if err != nil {
		if fault.IsKind(err, fault.NotFound) {
			return LoginResult{}, fault.New(fault.Unauthorized, "invalid_credentials", "email or password is incorrect")
		}
		return LoginResult{}, err
	}
	if !user.Active || bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(input.Password)) != nil {
		return LoginResult{}, fault.New(fault.Unauthorized, "invalid_credentials", "email or password is incorrect")
	}
	token, err := randomToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.Clock.Now()
	session, err := identity.NewSession(s.IDs.New("session"), user.TenantID, user.ID, token, now, s.SessionTTL)
	if err != nil {
		return LoginResult{}, err
	}
	actor := Actor{TenantID: user.TenantID, UserID: user.ID, RequestID: requestID}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.InsertSession(ctx, session); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "session.login", "session", session.ID, audit.Succeeded, map[string]any{"expires_at": session.ExpiresAt}, now)
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, ExpiresAt: session.ExpiresAt, User: user.Clone()}, nil
}

func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fault.Wrap(fault.Internal, "token_generation_failed", "generate session token", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func (s AuthService) Authenticate(ctx context.Context, token string) (identity.User, identity.Session, error) {
	if err := s.Validate(); err != nil {
		return identity.User{}, identity.Session{}, err
	}
	if strings.TrimSpace(token) == "" {
		return identity.User{}, identity.Session{}, fault.New(fault.Unauthorized, "session_required", "authentication is required")
	}
	session, err := s.Store.GetSessionByTokenHash(ctx, identity.HashToken(token))
	if err != nil {
		if fault.IsKind(err, fault.NotFound) {
			return identity.User{}, identity.Session{}, fault.New(fault.Unauthorized, "invalid_session", "session is not recognized")
		}
		return identity.User{}, identity.Session{}, err
	}
	now := s.Clock.Now()
	original := session.Version
	if err := session.Validate(now); err != nil {
		if session.State == identity.SessionExpired {
			_ = s.Store.UpdateSession(ctx, session, original)
		}
		return identity.User{}, identity.Session{}, err
	}
	user, err := s.Store.GetUser(ctx, session.TenantID, session.UserID)
	if err != nil {
		return identity.User{}, identity.Session{}, err
	}
	if !user.Active {
		return identity.User{}, identity.Session{}, fault.New(fault.Unauthorized, "user_inactive", "user account is inactive")
	}
	if session.Touch(now, s.TouchInterval) {
		if err := s.Store.UpdateSession(ctx, session, original); err != nil && !fault.IsKind(err, fault.Conflict) {
			return identity.User{}, identity.Session{}, err
		}
	}
	return user.Clone(), session, nil
}

func (s AuthService) Logout(ctx context.Context, actor Actor, token string) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	session, err := s.Store.GetSessionByTokenHash(ctx, identity.HashToken(token))
	if err != nil {
		return err
	}
	if session.TenantID != actor.TenantID || session.UserID != actor.UserID {
		return fault.New(fault.Forbidden, "session_owner_mismatch", "session belongs to another actor")
	}
	now := s.Clock.Now()
	previous := session.Version
	if err := session.Revoke(now); err != nil {
		return err
	}
	return s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.UpdateSession(ctx, session, previous); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "session.logout", "session", session.ID, audit.Succeeded, nil, now)
	})
}

func (s AuthService) DeactivateUser(ctx context.Context, actor Actor, userID string) error {
	coordinator, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return err
	}
	if !coordinator.CanCoordinate() {
		return fault.New(fault.Forbidden, "coordinator_required", "a coordinator is required")
	}
	user, err := s.Store.GetUser(ctx, actor.TenantID, userID)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	previous := user.Version
	if err := user.Deactivate(now); err != nil {
		return err
	}
	return s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.UpdateUser(ctx, user, previous); err != nil {
			return err
		}
		if _, err := tx.RevokeSessionsForUser(ctx, actor.TenantID, userID, now); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "user.deactivate", "user", userID, audit.Succeeded, nil, now)
	})
}

func IsNotFound(err error) bool {
	return errors.Is(err, context.Canceled) || fault.IsKind(err, fault.NotFound)
}
