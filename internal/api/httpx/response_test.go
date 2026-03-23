// response_test.go verifies JSON decoding, error mapping, and SSE response helpers.
package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	apperrors "etcdisc/internal/core/errors"
)

type plainWriter struct{ header http.Header }

func (w *plainWriter) Header() http.Header       { return w.header }
func (w *plainWriter) Write([]byte) (int, error) { return 0, nil }
func (w *plainWriter) WriteHeader(int)           {}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	var payload struct {
		Name string `json:"name"`
	}
	err := DecodeJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"prod"}`)), &payload)
	require.NoError(t, err)
	require.Equal(t, "prod", payload.Name)
	err = DecodeJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"extra":1}`)), &payload)
	require.Equal(t, apperrors.CodeInvalidArgument, apperrors.CodeOf(err))
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
	require.NoError(t, json.Unmarshal([]byte(strings.Split(strings.Split(resp.Body.String(), "data: ")[1], "\n\n")[0]), &map[string]string{}))
}

func TestSSEErrors(t *testing.T) {
	t.Parallel()

	w := &plainWriter{header: http.Header{}}
	require.Equal(t, apperrors.CodeInternal, apperrors.CodeOf(BeginSSE(w)))
	require.Equal(t, apperrors.CodeInternal, apperrors.CodeOf(WriteSSE(w, "put", make(chan int))))
}
