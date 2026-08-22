package seminar_test

import (
	"testing"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/seminar"
)

var seminarNow = time.Date(2026, 8, 22, 15, 0, 0, 0, time.UTC)

func agenda(t *testing.T) seminar.Agenda {
	t.Helper()
	value, err := seminar.NewAgenda("agenda", "tenant", "program", seminarNow)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAgendaLifecycleRequiresItemsAndResolution(t *testing.T) {
	value := agenda(t)
	if err := value.MarkReady(0, seminarNow); !fault.IsKind(err, fault.Precondition) {
		t.Fatalf("empty agenda ready error = %v", err)
	}
	if value.State != seminar.Assembling {
		t.Fatalf("empty ready changed state = %s", value.State)
	}
	if err := value.MarkReady(2, seminarNow); err != nil {
		t.Fatal(err)
	}
	if err := value.Start(seminarNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := value.Close(1, seminarNow.Add(2*time.Hour)); !fault.IsKind(err, fault.Precondition) {
		t.Fatalf("unresolved close error = %v", err)
	}
	if value.State != seminar.InSession {
		t.Fatalf("failed close changed state = %s", value.State)
	}
	if err := value.Close(0, seminarNow.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if value.State != seminar.Closed || value.Version != 4 {
		t.Fatalf("closed agenda = %+v", value)
	}
}

func TestAgendaIllegalTransitionsPreserveState(t *testing.T) {
	value := agenda(t)
	if err := value.Start(seminarNow); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("start assembling error = %v", err)
	}
	if err := value.Close(0, seminarNow); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("close assembling error = %v", err)
	}
	if value.State != seminar.Assembling || value.Version != 1 {
		t.Fatalf("illegal transitions mutated agenda: %+v", value)
	}
}

func TestAgendaItemLifecycle(t *testing.T) {
	item, err := seminar.NewItem("item", "tenant", "agenda", "claim", 1, "How does the page texture alter the return to the farewell?", seminarNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := item.Resolve(seminar.ItemDiscussed, "The group connected tactile interruption with the scene's pacing.", seminarNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if item.State != seminar.ItemDiscussed || item.Outcome == "" || item.Version != 2 {
		t.Fatalf("resolved item = %+v", item)
	}
	if err := item.Resolve(seminar.ItemDeferred, "Another edition is needed for comparison.", seminarNow.Add(2*time.Hour)); !fault.IsKind(err, fault.Conflict) {
		t.Fatalf("repeat resolution error = %v", err)
	}
}

func TestAgendaItemCanBeDeferredWithRecordedOutcome(t *testing.T) {
	item, err := seminar.NewItem("item", "tenant", "agenda", "claim", 1, "Compare punctuation across physical editions.", seminarNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := item.Resolve(seminar.ItemDeferred, "A second physical edition must be inspected before conclusion.", seminarNow); err != nil {
		t.Fatal(err)
	}
	if item.State != seminar.ItemDeferred {
		t.Fatalf("state = %s", item.State)
	}
}

func TestAgendaAndItemValidation(t *testing.T) {
	if _, err := seminar.NewAgenda("", "tenant", "program", seminarNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("missing agenda id error = %v", err)
	}
	tests := []struct {
		name, id, tenant, agendaID, claimID, prompt string
		position                                    int
	}{
		{"missing id", "", "tenant", "agenda", "claim", "A useful prompt", 1},
		{"missing tenant", "item", "", "agenda", "claim", "A useful prompt", 1},
		{"missing agenda", "item", "tenant", "", "claim", "A useful prompt", 1},
		{"missing claim", "item", "tenant", "agenda", "", "A useful prompt", 1},
		{"zero position", "item", "tenant", "agenda", "claim", "A useful prompt", 0},
		{"short prompt", "item", "tenant", "agenda", "claim", "short", 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := seminar.NewItem(test.id, test.tenant, test.agendaID, test.claimID, test.position, test.prompt, seminarNow)
			if !fault.IsKind(err, fault.Validation) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestAgendaItemRejectsUnsupportedResolutionAndShortOutcome(t *testing.T) {
	item, err := seminar.NewItem("item", "tenant", "agenda", "claim", 1, "Compare embodied responses to the scene.", seminarNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := item.Resolve("deleted", "A sufficiently long but unsupported outcome.", seminarNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("unsupported state error = %v", err)
	}
	if err := item.Resolve(seminar.ItemDiscussed, "short", seminarNow); !fault.IsKind(err, fault.Validation) {
		t.Fatalf("short outcome error = %v", err)
	}
	if item.State != seminar.ItemPending || item.Version != 1 {
		t.Fatalf("invalid resolution mutated item: %+v", item)
	}
}
