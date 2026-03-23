// namespace_page_test.go verifies that the namespace console page renders server-side HTML.
package console

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	namespacesvc "etcdisc/internal/core/service/namespace"
	"etcdisc/test/testkit"
)

func TestNamespacePage(t *testing.T) {
	t.Parallel()

	service := namespacesvc.NewService(testkit.NewMemoryStore(), namespacesvc.NewFixedClock(time.Now()))
	_, err := service.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	NamespacePage{Service: service}.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/console/namespaces?namespace=prod-core", nil))

	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "Namespace Control")
	require.Contains(t, resp.Body.String(), "prod-core")
	require.Contains(t, resp.Body.String(), "read_write")
}
