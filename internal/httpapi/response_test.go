package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/fault"
	appmiddleware "github.com/11DingKing/embodied-reading-studio/internal/middleware"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
)

func requestWithID(method, path string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, path, body)
	request = request.WithContext(appmiddleware.WithRequestID(request.Context(), "request-42"))
	return request
}

func TestWriteErrorMapsStableKindsAndRequestID(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"validation", fault.New(fault.Validation, "invalid_page", "page is invalid"), 400, "invalid_page"},
		{"unauthorized", fault.New(fault.Unauthorized, "login_required", "login required"), 401, "login_required"},
		{"forbidden", fault.New(fault.Forbidden, "role_required", "role required"), 403, "role_required"},
		{"not found", fault.New(fault.NotFound, "program_not_found", "program not found"), 404, "program_not_found"},
		{"conflict", fault.New(fault.Conflict, "lease_changed", "lease changed"), 409, "lease_changed"},
		{"precondition", fault.New(fault.Precondition, "state_invalid", "state invalid"), 412, "state_invalid"},
		{"deadline", fault.New(fault.Deadline, "deadline", "deadline exceeded"), 504, "deadline"},
		{"cancelled", fault.New(fault.Cancelled, "cancelled", "request cancelled"), 499, "cancelled"},
		{"dependency", fault.New(fault.Dependency, "database_down", "database unavailable"), 503, "database_down"},
		{"unknown", errors.New("secret implementation detail"), 500, "internal_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeError(response, requestWithID("GET", "/v1/test", nil), test.err)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			var payload errorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Code != test.code || payload.RequestID != "request-42" {
				t.Fatalf("payload = %+v", payload)
			}
			if test.name == "unknown" && strings.Contains(payload.Message, "secret") {
				t.Fatalf("internal detail leaked: %+v", payload)
			}
		})
	}
}

func TestDecodeRejectsUnknownAndMultipleJSONValues(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name string
		body string
		code string
	}{
		{"unknown field", `{"name":"reader","unexpected":true}`, "invalid_json"},
		{"multiple values", `{"name":"reader"} {"name":"other"}`, "multiple_json_values"},
		{"malformed", `{"name":`, "invalid_json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			_, ok := decode[input](response, requestWithID("POST", "/v1/test", bytes.NewBufferString(test.body)))
			if ok {
				t.Fatal("invalid payload decoded successfully")
			}
			var payload errorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Code != test.code {
				t.Fatalf("code = %q, want %q", payload.Code, test.code)
			}
		})
	}
}

func TestDecodeAcceptsSingleKnownObject(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	response := httptest.NewRecorder()
	value, ok := decode[input](response, requestWithID("POST", "/v1/test", bytes.NewBufferString(`{"name":"reader"}`)))
	if !ok || value.Name != "reader" {
		t.Fatalf("decoded = %+v, ok=%v, response=%s", value, ok, response.Body.String())
	}
}

type healthStore struct{ err error }

func (s healthStore) Ping(context.Context) error { return s.err }

func testMiddleware() appmiddleware.HTTP {
	return appmiddleware.HTTP{
		IDs:         idgen.NewSequence(1),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnAuthError: WriteAuthenticationError,
	}
}

func TestHealthEndpointsAndRequestID(t *testing.T) {
	api := API{Store: healthStore{}, Middleware: testMiddleware(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := api.Handler()
	for _, path := range []string{"/health/live", "/health/ready"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", path, nil)
		request.Header.Set("X-Request-ID", "client-request")
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, response.Code, response.Body.String())
		}
		if response.Header().Get("X-Request-ID") != "client-request" {
			t.Fatalf("%s request id = %q", path, response.Header().Get("X-Request-ID"))
		}
	}
}

func TestReadinessReportsDependencyFailure(t *testing.T) {
	api := API{Store: healthStore{err: fault.New(fault.Dependency, "database_unavailable", "database unavailable")}, Middleware: testMiddleware(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/health/ready", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var payload errorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "database_unavailable" || payload.RequestID == "" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestProtectedRouteRejectsMissingBearerToken(t *testing.T) {
	middleware := testMiddleware()
	middleware.Auth = nil
	api := API{Store: healthStore{}, Middleware: middleware, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, httptest.NewRequest("POST", "/v1/logout", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var payload errorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "authentication_required" || payload.RequestID == "" {
		t.Fatalf("payload = %+v", payload)
	}
}
