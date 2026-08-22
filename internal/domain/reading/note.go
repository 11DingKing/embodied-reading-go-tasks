package reading

import (
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type SensoryChannel string

const (
	Visual      SensoryChannel = "visual"
	Tactile     SensoryChannel = "tactile"
	Auditory    SensoryChannel = "auditory"
	Olfactory   SensoryChannel = "olfactory"
	Kinesthetic SensoryChannel = "kinesthetic"
	Reflective  SensoryChannel = "reflective"
)

type Observation struct {
	ID        string
	TenantID  string
	SessionID string
	Page      int
	Channel   SensoryChannel
	Body      string
	Sequence  int
	CreatedAt time.Time
}

func NewObservation(id, tenantID, sessionID string, page int, channel SensoryChannel, body string, sequence int, now time.Time) (Observation, error) {
	if id == "" || tenantID == "" || sessionID == "" {
		return Observation{}, fault.Invalid("observation", "requires id, tenant, and session")
	}
	if page < 1 || sequence < 1 {
		return Observation{}, fault.Invalid("observation_position", "must be positive")
	}
	switch channel {
	case Visual, Tactile, Auditory, Olfactory, Kinesthetic, Reflective:
	default:
		return Observation{}, fault.Invalid("sensory_channel", "is not supported")
	}
	body = strings.TrimSpace(body)
	if len(body) < 12 {
		return Observation{}, fault.Invalid("observation_body", "must contain a substantive note")
	}
	return Observation{ID: id, TenantID: tenantID, SessionID: sessionID, Page: page, Channel: channel, Body: body, Sequence: sequence, CreatedAt: now.UTC()}, nil
}
