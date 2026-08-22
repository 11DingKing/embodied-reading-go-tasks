package fault

import (
	"errors"
	"fmt"
)

type Kind string

const (
	Validation   Kind = "validation"
	Unauthorized Kind = "unauthorized"
	Forbidden    Kind = "forbidden"
	NotFound     Kind = "not_found"
	Conflict     Kind = "conflict"
	Precondition Kind = "precondition"
	Deadline     Kind = "deadline"
	Cancelled    Kind = "cancelled"
	Dependency   Kind = "dependency"
	Internal     Kind = "internal"
)

type Error struct {
	Kind      Kind
	Code      string
	Message   string
	Resource  string
	Operation string
	Cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	base := e.Message
	if base == "" {
		base = string(e.Kind)
	}
	if e.Resource != "" {
		base = e.Resource + ": " + base
	}
	if e.Operation != "" {
		base = e.Operation + ": " + base
	}
	if e.Cause != nil {
		return base + ": " + e.Cause.Error()
	}
	return base
}

func (e *Error) Unwrap() error { return e.Cause }

func New(kind Kind, code, message string) error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func Wrap(kind Kind, code, operation string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: kind, Code: code, Operation: operation, Cause: err}
}

func WithResource(err error, resource string) error {
	var current *Error
	if errors.As(err, &current) {
		clone := *current
		clone.Resource = resource
		return &clone
	}
	return &Error{Kind: Internal, Code: "internal_error", Resource: resource, Cause: err}
}

func KindOf(err error) Kind {
	var target *Error
	if errors.As(err, &target) {
		return target.Kind
	}
	return Internal
}

func CodeOf(err error) string {
	var target *Error
	if errors.As(err, &target) && target.Code != "" {
		return target.Code
	}
	return "internal_error"
}

func PublicMessage(err error) string {
	var target *Error
	if errors.As(err, &target) && target.Message != "" {
		return target.Message
	}
	return "the request could not be completed"
}

func IsKind(err error, kind Kind) bool { return KindOf(err) == kind }

func Invalid(field, reason string) error {
	return &Error{Kind: Validation, Code: "invalid_" + field, Message: fmt.Sprintf("%s %s", field, reason)}
}

func Missing(resource, id string) error {
	return &Error{Kind: NotFound, Code: resource + "_not_found", Message: "requested " + resource + " was not found", Resource: id}
}

func StateConflict(resource, current, requested string) error {
	return &Error{
		Kind:     Conflict,
		Code:     resource + "_state_conflict",
		Message:  fmt.Sprintf("cannot move from %s to %s", current, requested),
		Resource: resource,
	}
}
