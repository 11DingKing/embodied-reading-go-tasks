package seminar

import (
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type AgendaState string

const (
	Assembling AgendaState = "assembling"
	Ready      AgendaState = "ready"
	InSession  AgendaState = "in_session"
	Closed     AgendaState = "closed"
)

type ItemState string

const (
	ItemPending   ItemState = "pending"
	ItemDiscussed ItemState = "discussed"
	ItemDeferred  ItemState = "deferred"
)

type Agenda struct {
	ID        string
	TenantID  string
	ProgramID string
	State     AgendaState
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Item struct {
	ID        string
	TenantID  string
	AgendaID  string
	ClaimID   string
	Position  int
	Prompt    string
	State     ItemState
	Outcome   string
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewAgenda(id, tenantID, programID string, now time.Time) (Agenda, error) {
	if id == "" || tenantID == "" || programID == "" {
		return Agenda{}, fault.Invalid("agenda", "requires id, tenant, and program")
	}
	now = now.UTC()
	return Agenda{ID: id, TenantID: tenantID, ProgramID: programID, State: Assembling, Version: 1, CreatedAt: now, UpdatedAt: now}, nil
}

func NewItem(id, tenantID, agendaID, claimID string, position int, prompt string, now time.Time) (Item, error) {
	if id == "" || tenantID == "" || agendaID == "" || claimID == "" {
		return Item{}, fault.Invalid("agenda_item", "requires all identifiers")
	}
	if position < 1 {
		return Item{}, fault.Invalid("position", "must be positive")
	}
	prompt = strings.TrimSpace(prompt)
	if len(prompt) < 8 {
		return Item{}, fault.Invalid("prompt", "must identify a discussion question")
	}
	now = now.UTC()
	return Item{ID: id, TenantID: tenantID, AgendaID: agendaID, ClaimID: claimID, Position: position, Prompt: prompt, State: ItemPending, Version: 1, CreatedAt: now, UpdatedAt: now}, nil
}

func (a *Agenda) MarkReady(itemCount int, now time.Time) error {
	if a.State != Assembling || itemCount < 1 {
		return fault.New(fault.Precondition, "agenda_not_ready", "agenda requires at least one item before it is ready")
	}
	a.State = Ready
	a.Version++
	a.UpdatedAt = now.UTC()
	return nil
}

func (a *Agenda) Start(now time.Time) error {
	if a.State != Ready {
		return fault.StateConflict("agenda", string(a.State), string(InSession))
	}
	a.State = InSession
	a.Version++
	a.UpdatedAt = now.UTC()
	return nil
}

func (a *Agenda) Close(unresolved int, now time.Time) error {
	if a.State != InSession {
		return fault.StateConflict("agenda", string(a.State), string(Closed))
	}
	if unresolved > 0 {
		return fault.New(fault.Precondition, "agenda_items_unresolved", "all agenda items need an outcome before closing")
	}
	a.State = Closed
	a.Version++
	a.UpdatedAt = now.UTC()
	return nil
}

func (i *Item) Resolve(state ItemState, outcome string, now time.Time) error {
	if i.State != ItemPending {
		return fault.StateConflict("agenda_item", string(i.State), string(state))
	}
	if state != ItemDiscussed && state != ItemDeferred {
		return fault.Invalid("item_state", "must be discussed or deferred")
	}
	outcome = strings.TrimSpace(outcome)
	if len(outcome) < 8 {
		return fault.Invalid("item_outcome", "must record the seminar conclusion")
	}
	i.State = state
	i.Outcome = outcome
	i.Version++
	i.UpdatedAt = now.UTC()
	return nil
}
