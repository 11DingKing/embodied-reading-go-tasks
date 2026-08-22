package idgen

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
)

type Generator interface {
	New(prefix string) string
}

type UUID struct{}

func (UUID) New(prefix string) string {
	return prefix + "_" + uuid.NewString()
}

type Sequence struct {
	mu   sync.Mutex
	next int
}

func NewSequence(start int) *Sequence { return &Sequence{next: start} }

func (s *Sequence) New(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("%s_%06d", prefix, s.next)
	s.next++
	return id
}
