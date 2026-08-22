package evidence_test

import (
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/evidence"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

var evidenceNow = time.Date(2026, 8, 22, 11, 0, 0, 0, time.UTC)

func newClaim(t *testing.T) evidence.Claim {
	t.Helper()
	value, err := evidence.NewClaim("claim", "tenant", "session", "edition", "author", 76, 78,
		"俏丫鬟抱屈夭风流", "The farewell is embodied through interrupted speech and physical proximity.", evidenceNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestClaimVerifiedLifecycle(t *testing.T) {
	claim := newClaim(t)
	if err := claim.Assign("review", evidenceNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if claim.State != evidence.ClaimAssigned || claim.CurrentReviewID != "review" {
		t.Fatalf("assigned claim = %+v", claim)
	}
	if err := claim.Decide(true, "review", evidenceNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if claim.State != evidence.ClaimVerified || !claim.Final() {
		t.Fatalf("verified claim = %+v", claim)
	}
}

func TestClaimRejectedDisputedResolvedLifecycle(t *testing.T) {
	claim := newClaim(t)
	if err := claim.Assign("review-1", evidenceNow); err != nil {
		t.Fatal(err)
	}
	if err := claim.Decide(false, "review-1", evidenceNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if claim.State != evidence.ClaimRejected || claim.Final() {
		t.Fatalf("rejected claim = %+v", claim)
	}
	if err := claim.Dispute(evidenceNow.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if claim.State != evidence.ClaimDisputed || claim.CurrentReviewID != "" {
		t.Fatalf("disputed claim = %+v", claim)
	}
	if err := claim.Assign("review-2", evidenceNow.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := claim.Resolve(true, "review-2", evidenceNow.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if claim.State != evidence.ClaimResolved || !claim.Final() {
		t.Fatalf("resolved claim = %+v", claim)
	}
}

func TestClaimFencesReviewOwnership(t *testing.T) {
	claim := newClaim(t)
	if err := claim.Assign("review-1", evidenceNow); err != nil {
		t.Fatal(err)
	}
	if err := claim.Decide(true, "review-2", evidenceNow); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("wrong review decision error = %v", err)
	}
	if claim.State != evidence.ClaimAssigned {
		t.Fatalf("wrong review mutated claim: %+v", claim)
	}
	if err := claim.Assign("review-3", evidenceNow); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("duplicate assignment error = %v", err)
	}
}

func TestClaimValidation(t *testing.T) {
	tests := []struct {
		name, id, tenant, session, edition, author, quote, interpretation string
		start, end                                                        int
	}{
		{"missing id", "", "tenant", "session", "edition", "author", "quote", "long interpretation", 1, 2},
		{"zero start", "id", "tenant", "session", "edition", "author", "quote", "long interpretation", 0, 2},
		{"reversed pages", "id", "tenant", "session", "edition", "author", "quote", "long interpretation", 3, 2},
		{"short quote", "id", "tenant", "session", "edition", "author", "x", "long interpretation", 1, 2},
		{"short interpretation", "id", "tenant", "session", "edition", "author", "quotation", "short", 1, 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := evidence.NewClaim(test.id, test.tenant, test.session, test.edition, test.author, test.start, test.end, test.quote, test.interpretation, evidenceNow)
			if !fault.IsKind(err, fault.Validation) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDisputeRequiresIndependentResolver(t *testing.T) {
	dispute, err := evidence.NewDispute("dispute", "tenant", "claim", "author", "The cited physical edition supports the omitted punctuation.", evidenceNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := dispute.Assign("author"); !fault.IsKind(err, fault.Precondition) {
		t.Fatalf("self assignment error = %v", err)
	}
	if err := dispute.Assign("reviewer"); err != nil {
		t.Fatal(err)
	}
	if err := dispute.Resolve(true, "other", "The edition was inspected and the punctuation confirmed.", evidenceNow); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("wrong resolver error = %v", err)
	}
	if err := dispute.Resolve(true, "reviewer", "The edition was inspected and the punctuation confirmed.", evidenceNow); err != nil {
		t.Fatal(err)
	}
	if dispute.State != evidence.DisputeUpheld || dispute.ResolvedAt == nil {
		t.Fatalf("resolved dispute = %+v", dispute)
	}
}

func TestDisputeRejectsShortEvidence(t *testing.T) {
	if _, err := evidence.NewDispute("dispute", "tenant", "claim", "author", "short", evidenceNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("short reason error = %v", err)
	}
	dispute, err := evidence.NewDispute("dispute", "tenant", "claim", "author", "A sufficiently detailed disagreement reason.", evidenceNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := dispute.Assign("reviewer"); err != nil {
		t.Fatal(err)
	}
	if err := dispute.Resolve(false, "reviewer", "short", evidenceNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("short resolution error = %v", err)
	}
}
