package program_test

import (
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
)

var programNow = time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)

func validProgram(t *testing.T) program.Program {
	t.Helper()
	value, err := program.New("program", "tenant", "edition", "coordinator", "Close Reading of Chapter 77", "Asia/Shanghai", 12,
		programNow.Add(24*time.Hour), programNow.Add(14*24*time.Hour), programNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestProgramHappyPath(t *testing.T) {
	value := validProgram(t)
	if err := value.OpenEnrollment(programNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if value.State != program.Enrolling {
		t.Fatalf("state = %s", value.State)
	}
	if err := value.Start(programNow.Add(25 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := value.RequestReview(programNow.Add(13 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := value.Archive(programNow.Add(15 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if value.State != program.Archived || value.Version != 5 {
		t.Fatalf("terminal program = %+v", value)
	}
}

func TestProgramRejectsIllegalTransitions(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*program.Program) error
	}{
		{"start draft", func(value *program.Program) error { return value.Start(programNow.Add(25 * time.Hour)) }},
		{"review draft", func(value *program.Program) error { return value.RequestReview(programNow) }},
		{"archive draft", func(value *program.Program) error { return value.Archive(programNow) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validProgram(t)
			if err := test.apply(&value); !fault.IsKind(err, fault.Conflict) {
				t.Fatalf("error = %v", err)
			}
			if value.State != program.Draft || value.Version != 1 {
				t.Fatalf("illegal transition mutated value: %+v", value)
			}
		})
	}
}

func TestProgramWindowRules(t *testing.T) {
	value := validProgram(t)
	if err := value.OpenEnrollment(value.StartsAt); !fault.IsKind(err, fault.Precondition) {
		t.Fatalf("late enrollment error = %v", err)
	}
	value = validProgram(t)
	if err := value.OpenEnrollment(programNow); err != nil {
		t.Fatal(err)
	}
	if err := value.Start(value.StartsAt.Add(-time.Nanosecond)); !fault.IsKind(err, fault.Precondition) {
		t.Fatalf("early start error = %v", err)
	}
	if err := value.Start(value.EndsAt); !fault.IsKind(err, fault.Precondition) {
		t.Fatalf("end boundary error = %v", err)
	}
}

func TestProgramCancellationOnlyBeforeActivation(t *testing.T) {
	value := validProgram(t)
	if err := value.Cancel(programNow); err != nil {
		t.Fatal(err)
	}
	if value.State != program.Cancelled {
		t.Fatalf("state = %s", value.State)
	}
	value = validProgram(t)
	if err := value.OpenEnrollment(programNow); err != nil {
		t.Fatal(err)
	}
	if err := value.Start(value.StartsAt); err != nil {
		t.Fatal(err)
	}
	if err := value.Cancel(value.StartsAt); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("active cancellation error = %v", err)
	}
}

func TestProgramCreationValidation(t *testing.T) {
	tests := []struct {
		name, id, tenant, edition, coordinator, title, zone string
		capacity                                            int
		start, end                                          time.Time
	}{
		{"missing ids", "", "tenant", "edition", "coordinator", "Title", "UTC", 2, programNow, programNow.Add(time.Hour)},
		{"blank title", "id", "tenant", "edition", "coordinator", " ", "UTC", 2, programNow, programNow.Add(time.Hour)},
		{"bad timezone", "id", "tenant", "edition", "coordinator", "Title", "Mars/Olympus", 2, programNow, programNow.Add(time.Hour)},
		{"capacity one", "id", "tenant", "edition", "coordinator", "Title", "UTC", 1, programNow, programNow.Add(time.Hour)},
		{"reversed window", "id", "tenant", "edition", "coordinator", "Title", "UTC", 2, programNow.Add(time.Hour), programNow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := program.New(test.id, test.tenant, test.edition, test.coordinator, test.title, test.zone, test.capacity, test.start, test.end, programNow)
			if !fault.IsKind(err, fault.Validation) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestMemberLifecycle(t *testing.T) {
	member, err := program.NewMember("member", "tenant", "program", "user", programNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := member.Complete(programNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if member.State != program.MemberCompleted || member.Version != 2 {
		t.Fatalf("member = %+v", member)
	}
	if err := member.Withdraw(programNow.Add(2 * time.Hour)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("completed withdrawal error = %v", err)
	}
	active, err := program.NewMember("member-2", "tenant", "program", "user-2", programNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := active.Withdraw(programNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if active.State != program.MemberWithdrawn {
		t.Fatalf("withdrawn state = %s", active.State)
	}
}
