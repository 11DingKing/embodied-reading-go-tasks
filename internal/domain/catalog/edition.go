package catalog

import (
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type EditionState string

const (
	EditionDraft   EditionState = "draft"
	EditionActive  EditionState = "active"
	EditionRetired EditionState = "retired"
)

type Edition struct {
	ID          string
	TenantID    string
	WorkID      string
	Label       string
	Publisher   string
	PublishedAt time.Time
	PageCount   int
	Fingerprint string
	State       EditionState
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewEdition(id, tenantID, workID, label, publisher string, publishedAt time.Time, pages int, fingerprint string, now time.Time) (Edition, error) {
	if id == "" || tenantID == "" || workID == "" {
		return Edition{}, fault.Invalid("edition", "requires id, tenant, and work")
	}
	if strings.TrimSpace(label) == "" || strings.TrimSpace(publisher) == "" {
		return Edition{}, fault.Invalid("edition", "requires label and publisher")
	}
	if pages < 1 {
		return Edition{}, fault.Invalid("page_count", "must be positive")
	}
	if len(strings.TrimSpace(fingerprint)) < 12 {
		return Edition{}, fault.Invalid("fingerprint", "must identify the physical edition")
	}
	now = now.UTC()
	return Edition{
		ID: id, TenantID: tenantID, WorkID: workID, Label: strings.TrimSpace(label), Publisher: strings.TrimSpace(publisher),
		PublishedAt: publishedAt.UTC(), PageCount: pages, Fingerprint: strings.TrimSpace(fingerprint), State: EditionDraft,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (e *Edition) Activate(now time.Time) error {
	if e.State != EditionDraft {
		return fault.StateConflict("edition", string(e.State), string(EditionActive))
	}
	e.State = EditionActive
	e.Version++
	e.UpdatedAt = now.UTC()
	return nil
}

func (e *Edition) Retire(now time.Time) error {
	if e.State != EditionActive {
		return fault.StateConflict("edition", string(e.State), string(EditionRetired))
	}
	e.State = EditionRetired
	e.Version++
	e.UpdatedAt = now.UTC()
	return nil
}

func (e Edition) ValidatePageRange(start, end int) error {
	if start < 1 || end < start || end > e.PageCount {
		return fault.Invalid("page_range", fmt.Sprintf("must be within 1..%d", e.PageCount))
	}
	return nil
}
