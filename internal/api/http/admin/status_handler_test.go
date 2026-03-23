// status_handler_test.go verifies health and readiness endpoints used by deployment checks.
package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealthHandler(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()

	HealthHandler(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, "ok", resp.Body.String())
}

func TestReadyHandlerAlias(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	resp := httptest.NewRecorder()
	ReadyHandler(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, "ready", resp.Body.String())
}

func TestNewReadyHandler(t *testing.T) {
	t.Parallel()

	t.Run("ready", func(t *testing.T) {
		t.Parallel()
		h := NewReadyHandler(func(_ context.Context) error { return nil })
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/ready", nil))
		require.Equal(t, http.StatusOK, resp.Code)
		require.Equal(t, "ready", resp.Body.String())
	})

	t.Run("not ready", func(t *testing.T) {
		t.Parallel()
		h := NewReadyHandler(func(_ context.Context) error { return errors.New("etcd down") })
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/ready", nil))
		require.Equal(t, http.StatusServiceUnavailable, resp.Code)
		require.Equal(t, "not ready", resp.Body.String())
	})
}
