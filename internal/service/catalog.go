package service

import (
	"context"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/audit"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/catalog"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
)

type CatalogService struct{ Dependencies }

type RegisterEditionInput struct {
	WorkID          string
	WorkTitle       string
	WorkAuthor      string
	WorkDescription string
	EditionLabel    string
	Publisher       string
	PublishedAt     time.Time
	PageCount       int
	Fingerprint     string
}

type RegisterEditionResult struct {
	Work    catalog.Work
	Edition catalog.Edition
}

func (s CatalogService) RegisterEdition(ctx context.Context, actor Actor, input RegisterEditionInput) (RegisterEditionResult, error) {
	if err := actor.Validate(); err != nil {
		return RegisterEditionResult{}, err
	}
	user, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return RegisterEditionResult{}, err
	}
	if !user.CanCoordinate() {
		return RegisterEditionResult{}, fault.New(fault.Forbidden, "coordinator_required", "a coordinator is required")
	}
	now := s.Clock.Now()
	work, err := catalog.NewWork(s.IDs.New("work"), actor.TenantID, input.WorkTitle, input.WorkAuthor, input.WorkDescription, now)
	if err != nil {
		return RegisterEditionResult{}, err
	}
	if input.WorkID != "" {
		work, err = s.Store.GetWork(ctx, actor.TenantID, input.WorkID)
		if err != nil {
			return RegisterEditionResult{}, err
		}
	}
	edition, err := catalog.NewEdition(s.IDs.New("edition"), actor.TenantID, work.ID, input.EditionLabel, input.Publisher, input.PublishedAt, input.PageCount, input.Fingerprint, now)
	if err != nil {
		return RegisterEditionResult{}, err
	}
	if err := edition.Activate(now); err != nil {
		return RegisterEditionResult{}, err
	}
	err = s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if input.WorkID == "" {
			if err := tx.InsertWork(ctx, work); err != nil {
				return err
			}
		}
		if err := tx.InsertEdition(ctx, edition); err != nil {
			return err
		}
		if err := addAudit(ctx, tx, s.IDs, actor, "edition.register", "edition", edition.ID, audit.Succeeded, map[string]any{"work_id": work.ID, "fingerprint": edition.Fingerprint}, now); err != nil {
			return err
		}
		return addOutbox(ctx, tx, s.IDs, actor.TenantID, "edition.activated", edition.ID, map[string]any{"work_id": work.ID}, now)
	})
	if err != nil {
		return RegisterEditionResult{}, err
	}
	return RegisterEditionResult{Work: work, Edition: edition}, nil
}

func (s CatalogService) RetireEdition(ctx context.Context, actor Actor, editionID string) error {
	user, err := s.Store.GetUser(ctx, actor.TenantID, actor.UserID)
	if err != nil {
		return err
	}
	if !user.CanCoordinate() {
		return fault.New(fault.Forbidden, "coordinator_required", "a coordinator is required")
	}
	edition, err := s.Store.GetEdition(ctx, actor.TenantID, editionID)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	previous := edition.Version
	if err := edition.Retire(now); err != nil {
		return err
	}
	return s.Store.WithinTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := tx.UpdateEdition(ctx, edition, previous); err != nil {
			return err
		}
		return addAudit(ctx, tx, s.IDs, actor, "edition.retire", "edition", edition.ID, audit.Succeeded, nil, now)
	})
}
