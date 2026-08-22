package catalog

import (
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type Work struct {
	ID          string
	TenantID    string
	Title       string
	Author      string
	Description string
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewWork(id, tenantID, title, author, description string, now time.Time) (Work, error) {
	if id == "" || tenantID == "" {
		return Work{}, fault.Invalid("work", "requires id and tenant")
	}
	title = strings.TrimSpace(title)
	author = strings.TrimSpace(author)
	if title == "" || author == "" {
		return Work{}, fault.Invalid("work", "requires title and author")
	}
	now = now.UTC()
	return Work{ID: id, TenantID: tenantID, Title: title, Author: author, Description: strings.TrimSpace(description), Version: 1, CreatedAt: now, UpdatedAt: now}, nil
}

func (w *Work) Rename(title string, now time.Time) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fault.Invalid("title", "must not be empty")
	}
	if title == w.Title {
		return nil
	}
	w.Title = title
	w.Version++
	w.UpdatedAt = now.UTC()
	return nil
}
