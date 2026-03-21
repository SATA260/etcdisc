// namespace_handler_test.go verifies namespace management HTTP endpoints.
package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	namespacesvc "etcdisc/internal/core/service/namespace"
	"etcdisc/test/testkit"
)

func TestNamespaceAPI(t *testing.T) {
	t.Parallel()

	service := namespacesvc.NewService(testkit.NewMemoryStore(), namespacesvc.NewFixedClock(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)))
	api := NamespaceAPI{Service: service}

	createReq := httptest.NewRequest(http.MethodPost, "/admin/v1/namespaces", strings.NewReader(`{"name":"prod-core"}`))
	createResp := httptest.NewRecorder()
	api.HandleCollection(createResp, createReq)
	require.Equal(t, http.StatusCreated, createResp.Code)
	require.Contains(t, createResp.Body.String(), `"name":"prod-core"`)

	listResp := httptest.NewRecorder()
	api.HandleCollection(listResp, httptest.NewRequest(http.MethodGet, "/admin/v1/namespaces", nil))
	require.Equal(t, http.StatusOK, listResp.Code)
	require.Contains(t, listResp.Body.String(), `"items"`)

	patchReq := httptest.NewRequest(http.MethodPatch, "/admin/v1/namespaces/prod-core", strings.NewReader(`{"accessMode":"read_only","expectedRevision":1}`))
	patchResp := httptest.NewRecorder()
	api.HandleItem(patchResp, patchReq)
	require.Equal(t, http.StatusOK, patchResp.Code)
	require.Contains(t, patchResp.Body.String(), `"accessMode":"read_only"`)
}
