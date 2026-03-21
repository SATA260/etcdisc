// admin_auth_test.go verifies the static admin token middleware for management routes.
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithAdminToken(t *testing.T) {
	t.Parallel()

	handler := WithAdminToken("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("rejects missing token", func(t *testing.T) {
		t.Parallel()
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/admin", nil))
		require.Equal(t, http.StatusUnauthorized, resp.Code)
	})

	t.Run("accepts bearer token", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("Authorization", "Bearer secret")
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		require.Equal(t, http.StatusNoContent, resp.Code)
	})
}
