package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/identity"
	"github.com/11DingKing/embodied-reading-studio/internal/middleware"
)

type cancelledAuthenticator struct{ sawCancellation bool }

func (a *cancelledAuthenticator) Authenticate(ctx context.Context, _ string) (identity.User, identity.Session, error) {
	a.sawCancellation = ctx.Err() != nil
	if err := ctx.Err(); err != nil {
		return identity.User{}, identity.Session{}, err
	}
	return identity.User{ID: "user", TenantID: "tenant", Active: true}, identity.Session{ID: "session", TenantID: "tenant", UserID: "user"}, nil
}

func TestCancelledAuthenticationDoesNotOutliveRequest(t *testing.T) {
	auth := &cancelledAuthenticator{}
	called := false
	m := middleware.HTTP{Auth: auth, OnAuthError: func(http.ResponseWriter, *http.Request, error) {}}
	handler := m.Authenticate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/v1/programs", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer token")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !auth.sawCancellation || called {
		t.Fatal("authentication continued after request cancellation")
	}
}
