// response_test.go verifies JSON decoding, error mapping, and SSE response helpers.
package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	apperrors "etcdisc/internal/core/errors"
)

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	var payload struct {
		Name string `json:"name"`
	}
	err := DecodeJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"prod"}`)), &payload)
	require.NoError(t, err)
	require.Equal(t, "prod", payload.Name)
}

func TestWriteError(t *testing.T) {
	t.Parallel()

	resp := httptest.NewRecorder()
	WriteError(resp, apperrors.New(apperrors.CodeForbidden, "blocked"))
	require.Equal(t, http.StatusForbidden, resp.Code)
	require.Contains(t, resp.Body.String(), "blocked")
}

func TestWriteSSE(t *testing.T) {
	t.Parallel()

	resp := httptest.NewRecorder()
	require.NoError(t, BeginSSE(resp))
	require.NoError(t, WriteSSE(resp, "put", map[string]string{"id": "1"}))
	require.Contains(t, resp.Body.String(), "event: put")
	require.Contains(t, resp.Body.String(), `"id":"1"`)
}
