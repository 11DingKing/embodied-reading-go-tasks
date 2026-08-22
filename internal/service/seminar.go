package service

import (
	"context"
	"fmt"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/program"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/seminar"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

type SeminarService struct{ Dependencies }

type GenerateAgendaResult struct {
	Agenda seminar.Agenda
	Items  []seminar.Item
}

func (s SeminarService) Generate(ctx context.Context, tenantID, programID string) (GenerateAgendaResult, error) {
	value, err := s.Store.GetProgram(ctx, tenantID, programID)
	if err != nil {
		return GenerateAgendaResult{}, err
	}
	if value.State != program.ReviewPending {
		return GenerateAgendaResult{}, fault.New(fault.Precondition, "program_not_review_pending", "agenda can only be generated after reading review begins")
	}
	claims, err := s.Store.ListFinalClaimsForProgram(ctx, tenantID, programID)
	if err != nil {
		return GenerateAgendaResult{}, err
	}
	if len(claims) == 0 {
		return GenerateAgendaResult{}, fault.New(fault.Precondition, "agenda_claims_missing", "agenda requires at least one verified claim")
	}
	now := s.Clock.Now()
	agenda, err := seminar.NewAgenda(s.IDs.New("agenda"), tenantID, programID, now)
	if err != nil {
		return GenerateAgendaResult{}, err
	}
	items := make([]seminar.Item, len(claims))
	for index, claim := range claims {
		prompt := fmt.Sprintf("Compare the physical reading evidence around pages %d-%d with the claim's interpretation", claim.PageStart, claim.PageEnd)
		items[index], err = seminar.NewItem(s.IDs.New("agenda_item"), tenantID, agenda.ID, claim.ID, index+1, prompt, now)
		if err != nil {
			return GenerateAgendaResult{}, err
		}
	}
	if err := agenda.MarkReady(len(items), now); err != nil {
		return GenerateAgendaResult{}, err
	}
	systemActor := Actor{TenantID: tenantID, UserID: "system", RequestID: s.IDs.New("request")}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		freshClaims, err := tx.ListFinalClaimsForProgram(ctx, tenantID, programID)
		if err != nil {
			return err
		}
		if len(freshClaims) != len(claims) {
			return fault.New(fault.Conflict, "agenda_claim_set_changed", "verified claim set changed during agenda generation")
		}
		if err := tx.InsertAgenda(ctx, agenda); err != nil {
			return err
		}
		for _, item := range items {
			if err := tx.InsertAgendaItem(ctx, item); err != nil {
				return err
			}
		}
		return addAudit(ctx, tx, s.IDs, systemActor, "agenda.generate", "seminar_agenda", agenda.ID, audit.Succeeded, map[string]any{"items": len(items)}, now)
	})
	if err != nil {
		return GenerateAgendaResult{}, err
	}
	return GenerateAgendaResult{Agenda: agenda, Items: items}, nil
}

func (s SeminarService) Start(ctx context.Context, actor Actor, agendaID string) (seminar.Agenda, error) {
	agenda, err := s.Store.GetAgenda(ctx, actor.TenantID, agendaID)
	if err != nil {
		return seminar.Agenda{}, err
	}
	value, err := s.Store.GetProgram(ctx, actor.TenantID, agenda.ProgramID)
	if err != nil {
		return seminar.Agenda{}, err
	}
	if value.CoordinatorID != actor.UserID {
		return seminar.Agenda{}, fault.New(fault.Forbidden, "program_coordinator_mismatch", "only the coordinator can start the seminar")
	}
	now := s.Clock.Now()
	previous := agenda.Version
	if err := agenda.Start(now); err != nil {
		return seminar.Agenda{}, err
	}
	if err := s.Store.UpdateAgenda(ctx, agenda, previous); err != nil {
		return seminar.Agenda{}, err
	}
	return agenda, nil
}

func (s SeminarService) ResolveItem(ctx context.Context, actor Actor, agendaID, itemID string, state seminar.ItemState, outcome string) (seminar.Item, error) {
	agenda, err := s.Store.GetAgenda(ctx, actor.TenantID, agendaID)
	if err != nil {
		return seminar.Item{}, err
	}
	if agenda.State != seminar.InSession {
		return seminar.Item{}, fault.New(fault.Precondition, "seminar_not_active", "agenda items can only be resolved during the seminar")
	}
	value, err := s.Store.GetProgram(ctx, actor.TenantID, agenda.ProgramID)
	if err != nil {
		return seminar.Item{}, err
	}
	if value.CoordinatorID != actor.UserID {
		return seminar.Item{}, fault.New(fault.Forbidden, "program_coordinator_mismatch", "only the coordinator can resolve agenda items")
	}
	items, err := s.Store.ListAgendaItems(ctx, actor.TenantID, agendaID)
	if err != nil {
		return seminar.Item{}, err
	}
	var item *seminar.Item
	for index := range items {
		if items[index].ID == itemID {
			item = &items[index]
			break
		}
	}
	if item == nil {
		return seminar.Item{}, fault.Missing("agenda_item", itemID)
	}
	now := s.Clock.Now()
	previous := item.Version
	if err := item.Resolve(state, outcome, now); err != nil {
		return seminar.Item{}, err
	}
	if err := s.Store.UpdateAgendaItem(ctx, *item, previous); err != nil {
		return seminar.Item{}, err
	}
	return *item, nil
}

func (s SeminarService) Close(ctx context.Context, actor Actor, agendaID string) (seminar.Agenda, error) {
	agenda, err := s.Store.GetAgenda(ctx, actor.TenantID, agendaID)
	if err != nil {
		return seminar.Agenda{}, err
	}
	value, err := s.Store.GetProgram(ctx, actor.TenantID, agenda.ProgramID)
	if err != nil {
		return seminar.Agenda{}, err
	}
	if value.CoordinatorID != actor.UserID {
		return seminar.Agenda{}, fault.New(fault.Forbidden, "program_coordinator_mismatch", "only the coordinator can close the seminar")
	}
	items, err := s.Store.ListAgendaItems(ctx, actor.TenantID, agenda.ID)
	if err != nil {
		return seminar.Agenda{}, err
	}
	unresolved := 0
	for _, item := range items {
		if item.State == seminar.ItemPending {
			unresolved++
		}
	}
	now := s.Clock.Now()
	previous := agenda.Version
	if err := agenda.Close(unresolved, now); err != nil {
		return seminar.Agenda{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		freshItems := seminar.PendingSnapshot(items)
		for _, item := range freshItems {
			if item.State == seminar.ItemPending {
				return fault.New(fault.Conflict, "agenda_resolution_changed", "agenda item became unresolved before closing")
			}
		}
		if err := tx.UpdateAgenda(ctx, agenda, previous); err != nil {
			return err
		}
		if err := addOutbox(ctx, tx, s.IDs, actor.TenantID, "agenda.closed", agenda.ID, map[string]string{"program_id": agenda.ProgramID}, now); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "agenda.close", "seminar_agenda", agenda.ID, audit.Succeeded, nil, now)
	})
	return agenda, err
}
