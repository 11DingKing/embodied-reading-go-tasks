package reading_test

import (
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
)

var readingNow = time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)

func newAssignment(t *testing.T, id, member string, pages reading.PageRange, startOffset time.Duration) reading.Assignment {
	t.Helper()
	value, err := reading.NewAssignment(id, "tenant", "program", "edition", member, pages, readingNow.Add(startOffset), readingNow.Add(startOffset+2*time.Hour), readingNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestPageRangeOverlap(t *testing.T) {
	tests := []struct {
		name        string
		left, right reading.PageRange
		want        bool
	}{
		{"same", reading.PageRange{Start: 10, End: 20}, reading.PageRange{Start: 10, End: 20}, true},
		{"touch start", reading.PageRange{Start: 10, End: 20}, reading.PageRange{Start: 20, End: 25}, true},
		{"inside", reading.PageRange{Start: 10, End: 30}, reading.PageRange{Start: 15, End: 18}, true},
		{"left separated", reading.PageRange{Start: 1, End: 9}, reading.PageRange{Start: 10, End: 20}, false},
		{"right separated", reading.PageRange{Start: 21, End: 30}, reading.PageRange{Start: 10, End: 20}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.left.Overlaps(test.right); got != test.want {
				t.Fatalf("Overlaps() = %v, want %v", got, test.want)
			}
			if got := test.right.Overlaps(test.left); got != test.want {
				t.Fatalf("reverse Overlaps() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAssignmentConflictRequiresAllSharedBoundaries(t *testing.T) {
	base := newAssignment(t, "base", "member-1", reading.PageRange{Start: 10, End: 20}, 0)
	overlap := newAssignment(t, "overlap", "member-2", reading.PageRange{Start: 15, End: 25}, time.Hour)
	if !base.Conflicts(overlap) {
		t.Fatal("overlapping page and time windows did not conflict")
	}
	differentProgram := overlap
	differentProgram.ProgramID = "other-program"
	if base.Conflicts(differentProgram) {
		t.Fatal("different programs conflicted")
	}
	differentEdition := overlap
	differentEdition.EditionID = "other-edition"
	if base.Conflicts(differentEdition) {
		t.Fatal("different editions conflicted")
	}
	differentTenant := overlap
	differentTenant.TenantID = "other-tenant"
	if base.Conflicts(differentTenant) {
		t.Fatal("different tenants conflicted")
	}
	separatePages := overlap
	separatePages.Pages = reading.PageRange{Start: 21, End: 30}
	if base.Conflicts(separatePages) {
		t.Fatal("non-overlapping pages conflicted")
	}
	separateTime := overlap
	separateTime.WindowStart = base.WindowEnd
	separateTime.WindowEnd = base.WindowEnd.Add(time.Hour)
	if base.Conflicts(separateTime) {
		t.Fatal("adjacent time windows conflicted")
	}
}

func TestReleasedAndExpiredAssignmentsDoNotConflict(t *testing.T) {
	base := newAssignment(t, "base", "member-1", reading.PageRange{Start: 10, End: 20}, 0)
	overlap := newAssignment(t, "overlap", "member-2", reading.PageRange{Start: 15, End: 25}, time.Hour)
	if err := base.Release(readingNow); err != nil {
		t.Fatal(err)
	}
	if base.Conflicts(overlap) {
		t.Fatal("released assignment still conflicts")
	}
	base = newAssignment(t, "base-2", "member-1", reading.PageRange{Start: 10, End: 20}, 0)
	if err := base.Expire(base.LeaseToken, base.WindowEnd); err != nil {
		t.Fatal(err)
	}
	if base.Conflicts(overlap) {
		t.Fatal("expired assignment still conflicts")
	}
}

func TestAssignmentLeaseLifecycle(t *testing.T) {
	value := newAssignment(t, "assignment", "member", reading.PageRange{Start: 5, End: 12}, 0)
	if err := value.Start("reading-1", readingNow.Add(30*time.Minute), readingNow); err != nil {
		t.Fatal(err)
	}
	if value.State != reading.AssignmentInReading || value.LeaseOwner != "reading-1" || value.LeaseToken != 1 {
		t.Fatalf("started assignment = %+v", value)
	}
	if err := value.Submit("reading-2", value.LeaseToken, readingNow.Add(time.Minute)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("wrong owner submit error = %v", err)
	}
	if err := value.Submit("reading-1", value.LeaseToken+1, readingNow.Add(time.Minute)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("wrong token submit error = %v", err)
	}
	if err := value.Submit("reading-1", value.LeaseToken, readingNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if value.State != reading.AssignmentSubmitted || value.LeaseOwner != "" || !value.LeaseUntil.IsZero() {
		t.Fatalf("submitted assignment retained lease: %+v", value)
	}
}

func TestAssignmentExpiryUsesFencingTokenAndTime(t *testing.T) {
	value := newAssignment(t, "assignment", "member", reading.PageRange{Start: 5, End: 12}, 0)
	if err := value.Start("reading", readingNow.Add(30*time.Minute), readingNow); err != nil {
		t.Fatal(err)
	}
	if err := value.Expire(value.LeaseToken-1, readingNow.Add(time.Hour)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("stale token error = %v", err)
	}
	if err := value.Expire(value.LeaseToken, readingNow.Add(10*time.Minute)); !fault.IsKind(err, fault.Precondition) {
		t.Fatalf("early expiry error = %v", err)
	}
	if err := value.Expire(value.LeaseToken, readingNow.Add(31*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if value.State != reading.AssignmentExpired || value.LeaseOwner != "" {
		t.Fatalf("expired assignment = %+v", value)
	}
}

func TestAssignmentCreationValidation(t *testing.T) {
	tests := []struct {
		name                                 string
		id, tenant, program, edition, member string
		pages                                reading.PageRange
		start, end                           time.Time
	}{
		{"missing identity", "", "tenant", "program", "edition", "member", reading.PageRange{1, 2}, readingNow, readingNow.Add(time.Hour)},
		{"invalid page start", "id", "tenant", "program", "edition", "member", reading.PageRange{0, 2}, readingNow, readingNow.Add(time.Hour)},
		{"reversed pages", "id", "tenant", "program", "edition", "member", reading.PageRange{3, 2}, readingNow, readingNow.Add(time.Hour)},
		{"reversed window", "id", "tenant", "program", "edition", "member", reading.PageRange{1, 2}, readingNow.Add(time.Hour), readingNow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := reading.NewAssignment(test.id, test.tenant, test.program, test.edition, test.member, test.pages, test.start, test.end, readingNow)
			if !fault.IsKind(err, fault.Validation) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func newSession(t *testing.T) reading.Session {
	t.Helper()
	value, err := reading.NewSession("reading", "tenant", "program", "assignment", "member", "Attend to page, type, texture, and recalled experience", readingNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestReadingSessionHappyPath(t *testing.T) {
	value := newSession(t)
	if err := value.Start(readingNow); err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(readingNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := value.Accept(readingNow.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if value.State != reading.SessionAccepted || value.StartedAt == nil || value.SubmittedAt == nil || value.AcceptedAt == nil {
		t.Fatalf("accepted session = %+v", value)
	}
	if value.Version != 4 {
		t.Fatalf("version = %d", value.Version)
	}
}

func TestReadingSessionReopenFlow(t *testing.T) {
	value := newSession(t)
	if err := value.Start(readingNow); err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(readingNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := value.Reopen(readingNow.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if value.State != reading.SessionReopened || value.SubmittedAt != nil {
		t.Fatalf("reopened session = %+v", value)
	}
	if err := value.Start(readingNow.Add(3 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if value.State != reading.SessionActive {
		t.Fatalf("state = %s", value.State)
	}
}

func TestReadingSessionAbandonment(t *testing.T) {
	value := newSession(t)
	if err := value.Abandon("power interruption in the reading room", readingNow); err != nil {
		t.Fatal(err)
	}
	if value.State != reading.SessionAbandoned || value.AbandonedReason == "" {
		t.Fatalf("abandoned session = %+v", value)
	}
	active := newSession(t)
	if err := active.Start(readingNow); err != nil {
		t.Fatal(err)
	}
	if err := active.Abandon(" ", readingNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("blank reason error = %v", err)
	}
	if active.State != reading.SessionActive {
		t.Fatal("invalid abandonment changed state")
	}
}

func TestObservationValidation(t *testing.T) {
	value, err := reading.NewObservation("note", "tenant", "session", 77, reading.Tactile, "The rough page edge slowed the return to this sentence.", 1, readingNow)
	if err != nil {
		t.Fatal(err)
	}
	if value.Channel != reading.Tactile || value.Page != 77 || value.Sequence != 1 {
		t.Fatalf("observation = %+v", value)
	}
	tests := []struct {
		name     string
		page     int
		channel  reading.SensoryChannel
		body     string
		sequence int
	}{
		{"zero page", 0, reading.Visual, "A sufficiently long observation", 1},
		{"unknown channel", 1, "digital", "A sufficiently long observation", 1},
		{"short body", 1, reading.Visual, "short", 1},
		{"zero sequence", 1, reading.Visual, "A sufficiently long observation", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := reading.NewObservation("note", "tenant", "session", test.page, test.channel, test.body, test.sequence, readingNow)
			if !fault.IsKind(err, fault.Validation) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
