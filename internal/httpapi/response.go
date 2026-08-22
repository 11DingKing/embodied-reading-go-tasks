package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	appmiddleware "github.com/11DingKing/embodied-reading-studio/internal/middleware"
)

type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func decode[T any](response http.ResponseWriter, request *http.Request) (T, bool) {
	var value T
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		writeError(response, request, fault.Wrap(fault.Validation, "invalid_json", "decode request body", err))
		return value, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, request, fault.New(fault.Validation, "multiple_json_values", "request body must contain one JSON value"))
		return value, false
	}
	return value, true
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, request *http.Request, err error) {
	status := http.StatusInternalServerError
	switch fault.KindOf(err) {
	case fault.Validation:
		status = http.StatusBadRequest
	case fault.Unauthorized:
		status = http.StatusUnauthorized
	case fault.Forbidden:
		status = http.StatusForbidden
	case fault.NotFound:
		status = http.StatusNotFound
	case fault.Conflict:
		status = http.StatusConflict
	case fault.Precondition:
		status = http.StatusPreconditionFailed
	case fault.Deadline:
		status = http.StatusGatewayTimeout
	case fault.Cancelled:
		status = 499
	case fault.Dependency:
		status = http.StatusServiceUnavailable
	}
	writeJSON(response, status, errorResponse{Code: fault.CodeOf(err), Message: fault.PublicMessage(err), RequestID: appmiddleware.RequestID(request.Context())})
}

func WriteAuthenticationError(response http.ResponseWriter, request *http.Request, err error) {
	if fault.KindOf(err) == fault.Internal {
		err = fault.New(fault.Unauthorized, "authentication_required", "authentication is required")
	}
	writeError(response, request, err)
}
