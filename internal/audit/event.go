package audit

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
)

type Result string

const (
	Succeeded Result = "succeeded"
	Rejected  Result = "rejected"
	Failed    Result = "failed"
)

type Event struct {
	ID         string
	TenantID   string
	ActorID    string
	RequestID  string
	Action     string
	ObjectType string
	ObjectID   string
	Result     Result
	Details    json.RawMessage
	CreatedAt  time.Time
}

func New(id, tenantID, actorID, requestID, action, objectType, objectID string, result Result, details any, now time.Time) (Event, error) {
	if id == "" || tenantID == "" || requestID == "" || action == "" || objectType == "" || objectID == "" {
		return Event{}, fault.Invalid("audit_event", "requires identity, request, action, and object")
	}
	if result != Succeeded && result != Rejected && result != Failed {
		return Event{}, fault.Invalid("audit_result", "is not supported")
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return Event{}, fault.Wrap(fault.Validation, "invalid_audit_details", "marshal audit details", err)
	}
	if strings.Contains(strings.ToLower(string(payload)), "password") || strings.Contains(strings.ToLower(string(payload)), "token") {
		return Event{}, fault.New(fault.Validation, "sensitive_audit_details", "audit details cannot contain credentials")
	}
	return Event{ID: id, TenantID: tenantID, ActorID: actorID, RequestID: requestID, Action: action, ObjectType: objectType, ObjectID: objectID, Result: result, Details: payload, CreatedAt: now.UTC()}, nil
}
