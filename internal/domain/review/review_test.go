package review_test

import (
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/review"
)

var reviewNow = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

func assignment(t *testing.T) review.Assignment {
	t.Helper()
	value, err := review.NewAssignment("review", "tenant", "claim", "author", "reviewer", reviewNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestReviewAssignmentRejectsSelfReview(t *testing.T) {
	_, err := review.NewAssignment("review", "tenant", "claim", "same-user", "same-user", reviewNow)
	if !fault.IsKind(err, fault.Precondition) {
		t.Fatalf("self review error = %v", err)
	}
}

func TestReviewLeaseClaimRenewAndComplete(t *testing.T) {
	value := assignment(t)
	if err := value.Claim("worker-a", 30*time.Second, reviewNow); err != nil {
		t.Fatal(err)
	}
	token := value.LeaseToken
	if value.State != review.Leased || value.Attempt != 1 {
		t.Fatalf("claimed assignment = %+v", value)
	}
	if err := value.Renew("worker-a", token, 30*time.Second, reviewNow.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !value.LeaseUntil.Equal(reviewNow.Add(40 * time.Second)) {
		t.Fatalf("renewed until %s", value.LeaseUntil)
	}
	if err := value.Complete("worker-a", token, reviewNow.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	if value.State != review.Decided || value.LeaseOwner != "" || !value.LeaseUntil.IsZero() {
		t.Fatalf("completed assignment = %+v", value)
	}
}

func TestReviewLeaseRejectsConcurrentOwner(t *testing.T) {
	value := assignment(t)
	if err := value.Claim("worker-a", time.Minute, reviewNow); err != nil {
		t.Fatal(err)
	}
	if err := value.Claim("worker-b", time.Minute, reviewNow.Add(time.Second)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("concurrent claim error = %v", err)
	}
	if value.LeaseOwner != "worker-a" || value.Attempt != 1 {
		t.Fatalf("failed claim changed lease: %+v", value)
	}
}

func TestReviewLeaseCanBeTakenAfterExpiryWithNewToken(t *testing.T) {
	value := assignment(t)
	if err := value.Claim("worker-a", time.Minute, reviewNow); err != nil {
		t.Fatal(err)
	}
	oldToken := value.LeaseToken
	if err := value.Claim("worker-b", time.Minute, reviewNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if value.LeaseOwner != "worker-b" || value.LeaseToken <= oldToken || value.Attempt != 2 {
		t.Fatalf("reclaimed assignment = %+v", value)
	}
	if err := value.Complete("worker-a", oldToken, reviewNow.Add(61*time.Second)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("stale completion error = %v", err)
	}
}

func TestReviewLeaseRejectsExpiredRenewalAndCompletion(t *testing.T) {
	value := assignment(t)
	if err := value.Claim("worker", time.Minute, reviewNow); err != nil {
		t.Fatal(err)
	}
	if err := value.Renew("worker", value.LeaseToken, time.Minute, reviewNow.Add(time.Minute)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("expired renewal error = %v", err)
	}
	if err := value.Complete("worker", value.LeaseToken, reviewNow.Add(time.Minute)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("expired completion error = %v", err)
	}
}

func TestDecisionValidation(t *testing.T) {
	decision, err := review.NewDecision("decision", "tenant", "review", "claim", "reviewer", review.Verified,
		"The physical edition and page reference match the quoted passage.", reviewNow)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != review.Verified || decision.AssignmentID != "review" {
		t.Fatalf("decision = %+v", decision)
	}
	if _, err := review.NewDecision("decision", "tenant", "review", "claim", "reviewer", "maybe", "A sufficiently detailed rationale.", reviewNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("unknown outcome error = %v", err)
	}
	if _, err := review.NewDecision("decision", "tenant", "review", "claim", "reviewer", review.Rejected, "short", reviewNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("short rationale error = %v", err)
	}
}
