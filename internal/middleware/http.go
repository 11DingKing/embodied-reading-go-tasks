package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/platform/idgen"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (identity.User, identity.Session, error)
}

type HTTP struct {
	Auth        Authenticator
	IDs         idgen.Generator
	Logger      *slog.Logger
	OnAuthError func(http.ResponseWriter, *http.Request, error)
}

func (m HTTP) RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestID := strings.TrimSpace(request.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = m.IDs.New("request")
		}
		response.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(response, request.WithContext(WithRequestID(request.Context(), requestID)))
	})
}

func (m HTTP) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		header := strings.TrimSpace(request.Header.Get("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			m.OnAuthError(response, request, &authHeaderError{})
			return
		}
		user, session, err := m.Auth.Authenticate(request.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			m.OnAuthError(response, request, err)
			return
		}
		ctx := WithPrincipal(request.Context(), Principal{User: user, Session: session})
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func (m HTTP) Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				m.Logger.ErrorContext(request.Context(), "request panic", "request_id", RequestID(request.Context()), "panic", recovered, "stack", string(debug.Stack()))
				http.Error(response, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func (m HTTP) Log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		next.ServeHTTP(response, request)
		m.Logger.InfoContext(request.Context(), "http request", "request_id", RequestID(request.Context()), "method", request.Method,
			"path", request.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

type authHeaderError struct{}

func (*authHeaderError) Error() string { return "bearer authentication is required" }
