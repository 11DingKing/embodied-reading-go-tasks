package clock

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

type Manual struct {
	mu  sync.RWMutex
	now time.Time
}

func NewManual(now time.Time) *Manual {
	return &Manual{now: now.UTC()}
}

func (m *Manual) Now() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.now
}

func (m *Manual) Set(now time.Time) {
	m.mu.Lock()
	m.now = now.UTC()
	m.mu.Unlock()
}

func (m *Manual) Advance(duration time.Duration) {
	m.mu.Lock()
	m.now = m.now.Add(duration)
	m.mu.Unlock()
}
