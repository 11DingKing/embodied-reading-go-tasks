package repository_test

import (
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

func TestPageNormalization(t *testing.T) {
	tests := []struct {
		name  string
		input repository.Page
		want  repository.Page
	}{
		{"defaults", repository.Page{}, repository.Page{Limit: 25, Offset: 0}},
		{"negative offset", repository.Page{Limit: 10, Offset: -5}, repository.Page{Limit: 10, Offset: 0}},
		{"maximum", repository.Page{Limit: 101, Offset: 3}, repository.Page{Limit: 100, Offset: 3}},
		{"unchanged", repository.Page{Limit: 40, Offset: 80}, repository.Page{Limit: 40, Offset: 80}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.input.Normalize(); got != test.want {
				t.Fatalf("Normalize() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestProgramReadinessSeparatesReviewAndArchiveBoundaries(t *testing.T) {
	ready := repository.ProgramReadiness{ActiveMembers: 2}
	if !ready.ReadyForReview() || !ready.ReadyForArchive() {
		t.Fatalf("clean readiness rejected: %+v", ready)
	}
	checks := []struct {
		name         string
		mutate       func(*repository.ProgramReadiness)
		reviewReady  bool
		archiveReady bool
	}{
		{"incomplete member", func(value *repository.ProgramReadiness) { value.IncompleteMembers = 1 }, false, false},
		{"active assignment", func(value *repository.ProgramReadiness) { value.ActiveAssignments = 1 }, false, false},
		{"open session", func(value *repository.ProgramReadiness) { value.OpenReadingSessions = 1 }, false, false},
		{"unresolved claim", func(value *repository.ProgramReadiness) { value.UnresolvedClaims = 1 }, true, false},
		{"open dispute", func(value *repository.ProgramReadiness) { value.OpenDisputes = 1 }, true, false},
		{"unresolved agenda", func(value *repository.ProgramReadiness) { value.UnresolvedAgenda = 1 }, true, false},
		{"active lease", func(value *repository.ProgramReadiness) { value.ActiveWorkerLeases = 1 }, true, false},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			value := ready
			check.mutate(&value)
			if value.ReadyForReview() != check.reviewReady || value.ReadyForArchive() != check.archiveReady {
				t.Fatalf("readiness = %+v, review=%v archive=%v", value, value.ReadyForReview(), value.ReadyForArchive())
			}
		})
	}
}

func TestRequestHashBindsMethodPathAndBody(t *testing.T) {
	base := repository.HashRequest("POST", "/v1/programs/p/join", []byte(`{"role":"reader"}`))
	if len(base) != 64 {
		t.Fatalf("hash length = %d", len(base))
	}
	if base != repository.HashRequest("post", "/v1/programs/p/join", []byte(`{"role":"reader"}`)) {
		t.Fatal("method normalization changed hash")
	}
	if base == repository.HashRequest("PUT", "/v1/programs/p/join", []byte(`{"role":"reader"}`)) {
		t.Fatal("different method had same hash")
	}
	if base == repository.HashRequest("POST", "/v1/programs/other/join", []byte(`{"role":"reader"}`)) {
		t.Fatal("different path had same hash")
	}
	if base == repository.HashRequest("POST", "/v1/programs/p/join", []byte(`{"role":"reviewer"}`)) {
		t.Fatal("different body had same hash")
	}
}

func TestIdempotencyRecordValidationAndCopy(t *testing.T) {
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	response := []byte("member-id")
	record, err := repository.NewIdempotencyRecord("tenant", "key", "post", "/join", "actor", repository.HashRequest("POST", "/join", nil), 201, response, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	response[0] = 'X'
	if string(record.Response) != "member-id" || record.Method != "POST" {
		t.Fatalf("record = %+v", record)
	}
	clone := record.Clone()
	clone.Response[0] = 'Y'
	if string(record.Response) != "member-id" {
		t.Fatal("clone response shared storage")
	}
	if _, err := repository.NewIdempotencyRecord("tenant", "key", "POST", "/join", "actor", "short", 201, nil, now, time.Hour); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("short hash error = %v", err)
	}
	if _, err := repository.NewIdempotencyRecord("tenant", "key", "POST", "/join", "actor", record.RequestHash, 201, nil, now, 0); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("zero ttl error = %v", err)
	}
}

func TestOutboxEventValidationAndClone(t *testing.T) {
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	event, err := repository.NewOutboxEvent("event", "tenant", "claim.submitted", "claim", map[string]string{"claim": "c1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if event.State != repository.OutboxPending || !event.NextTryAt.Equal(now) {
		t.Fatalf("event = %+v", event)
	}
	clone := event.Clone()
	clone.Payload[0] = 'X'
	if event.Payload[0] == 'X' {
		t.Fatal("outbox clone shared payload")
	}
	if _, err := repository.NewOutboxEvent("", "tenant", "topic", "aggregate", nil, now); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("missing id error = %v", err)
	}
	if _, err := repository.NewOutboxEvent("event", "tenant", "topic", "aggregate", func() {}, now); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("unencodable payload error = %v", err)
	}
}
