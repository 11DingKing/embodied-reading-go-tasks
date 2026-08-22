package identity_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
)

var identityNow = time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

func TestNewUserNormalizesIdentityAndCopiesInputs(t *testing.T) {
	roles := []identity.Role{identity.Coordinator, identity.Researcher}
	hash := []byte("already-hashed-password")
	user, err := identity.NewUser("user-1", "tenant-1", " Reader@Example.Test ", " Reader One ", roles, hash, identityNow)
	if err != nil {
		t.Fatalf("NewUser() error = %v", err)
	}
	if user.Email != "reader@example.test" {
		t.Fatalf("Email = %q", user.Email)
	}
	if user.DisplayName != "Reader One" {
		t.Fatalf("DisplayName = %q", user.DisplayName)
	}
	roles[0] = identity.Reviewer
	hash[0] = 'X'
	if user.Roles[0] != identity.Coordinator {
		t.Fatalf("roles were aliased: %v", user.Roles)
	}
	if string(user.PasswordHash) != "already-hashed-password" {
		t.Fatalf("password hash was aliased: %q", user.PasswordHash)
	}
}

func TestNewUserRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		tenant  string
		email   string
		display string
		roles   []identity.Role
		hash    []byte
	}{
		{"missing id", "", "tenant", "a@example.test", "A", []identity.Role{identity.Researcher}, []byte("hash")},
		{"missing tenant", "id", "", "a@example.test", "A", []identity.Role{identity.Researcher}, []byte("hash")},
		{"invalid email", "id", "tenant", "invalid", "A", []identity.Role{identity.Researcher}, []byte("hash")},
		{"blank display", "id", "tenant", "a@example.test", " ", []identity.Role{identity.Researcher}, []byte("hash")},
		{"no roles", "id", "tenant", "a@example.test", "A", nil, []byte("hash")},
		{"unknown role", "id", "tenant", "a@example.test", "A", []identity.Role{"owner"}, []byte("hash")},
		{"duplicate role", "id", "tenant", "a@example.test", "A", []identity.Role{identity.Reviewer, identity.Reviewer}, []byte("hash")},
		{"missing hash", "id", "tenant", "a@example.test", "A", []identity.Role{identity.Researcher}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := identity.NewUser(test.id, test.tenant, test.email, test.display, test.roles, test.hash, identityNow)
			if !fault.IsKind(err, fault.Validation) {
				t.Fatalf("error kind = %v, error = %v", fault.KindOf(err), err)
			}
		})
	}
}

func TestUserCapabilitiesRespectActiveStateAndRoles(t *testing.T) {
	user, err := identity.NewUser("user", "tenant", "user@example.test", "Reader", []identity.Role{identity.Researcher, identity.Reviewer}, []byte("hash"), identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if user.CanCoordinate() {
		t.Fatal("researcher unexpectedly coordinates")
	}
	if !user.CanResearch() || !user.CanReview() {
		t.Fatalf("capabilities missing: %+v", user)
	}
	if err := user.Deactivate(identityNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if user.CanResearch() || user.CanReview() {
		t.Fatal("inactive user retained capabilities")
	}
	if user.Version != 2 || !user.UpdatedAt.Equal(identityNow.Add(time.Hour)) {
		t.Fatalf("deactivation metadata = version %d, updated %s", user.Version, user.UpdatedAt)
	}
}

func TestUserCloneIsDeep(t *testing.T) {
	user, err := identity.NewUser("user", "tenant", "user@example.test", "Reader", []identity.Role{identity.Researcher}, []byte("secret hash"), identityNow)
	if err != nil {
		t.Fatal(err)
	}
	clone := user.Clone()
	clone.Roles[0] = identity.Reviewer
	clone.PasswordHash[0] = 'X'
	if user.Roles[0] != identity.Researcher || string(user.PasswordHash) != "secret hash" {
		t.Fatalf("clone mutated source: %+v", user)
	}
}

func TestUserJSONNeverExposesPasswordHash(t *testing.T) {
	user, err := identity.NewUser("user", "tenant", "user@example.test", "Reader", []identity.Role{identity.Researcher}, []byte("secret hash"), identityNow)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "PasswordHash") || strings.Contains(string(payload), "secret") {
		t.Fatalf("password material leaked in JSON: %s", payload)
	}
}

func TestSessionTokenHashAndConstantTimeComparison(t *testing.T) {
	hash := identity.HashToken("opaque-token")
	if hash == "opaque-token" || len(hash) != 64 {
		t.Fatalf("unexpected hash %q", hash)
	}
	if !identity.TokenMatches(hash, "opaque-token") {
		t.Fatal("matching token rejected")
	}
	if identity.TokenMatches(hash, "another-token") {
		t.Fatal("different token accepted")
	}
	if identity.TokenMatches("not-hex", "opaque-token") {
		t.Fatal("invalid hash accepted")
	}
}

func TestSessionLifecycle(t *testing.T) {
	session, err := identity.NewSession("session", "tenant", "user", "opaque", identityNow, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Validate(identityNow.Add(time.Hour)); err != nil {
		t.Fatalf("active session rejected: %v", err)
	}
	if session.Touch(identityNow.Add(2*time.Minute), 5*time.Minute) {
		t.Fatal("session touched before minimum interval")
	}
	if !session.Touch(identityNow.Add(10*time.Minute), 5*time.Minute) {
		t.Fatal("session did not touch after interval")
	}
	if session.Version != 2 {
		t.Fatalf("version after touch = %d", session.Version)
	}
	if err := session.Revoke(identityNow.Add(20 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := session.Validate(identityNow.Add(21 * time.Minute)); !fault.IsKind(err, fault.Unauthorized) {
		t.Fatalf("revoked session validation = %v", err)
	}
	if err := session.Revoke(identityNow.Add(30 * time.Minute)); err != nil {
		t.Fatalf("repeat revoke should be idempotent: %v", err)
	}
}

func TestSessionExpiresAtBoundaryAndCannotBeRevokedAfterExpiry(t *testing.T) {
	session, err := identity.NewSession("session", "tenant", "user", "opaque", identityNow, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Validate(identityNow.Add(time.Hour)); !fault.IsKind(err, fault.Unauthorized) {
		t.Fatalf("expiry boundary error = %v", err)
	}
	if session.State != identity.SessionExpired {
		t.Fatalf("state = %s", session.State)
	}
	if err := session.Revoke(identityNow.Add(2 * time.Hour)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("revoke expired error = %v", err)
	}
}

func TestNewSessionRejectsInvalidOwnershipAndTTL(t *testing.T) {
	tests := []struct {
		name, id, tenant, user, token string
		ttl                           time.Duration
	}{
		{"missing id", "", "tenant", "user", "token", time.Hour},
		{"missing tenant", "id", "", "user", "token", time.Hour},
		{"missing user", "id", "tenant", "", "token", time.Hour},
		{"missing token", "id", "tenant", "user", "", time.Hour},
		{"zero ttl", "id", "tenant", "user", "token", 0},
		{"negative ttl", "id", "tenant", "user", "token", -time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := identity.NewSession(test.id, test.tenant, test.user, test.token, identityNow, test.ttl)
			if !fault.IsKind(err, fault.Validation) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
