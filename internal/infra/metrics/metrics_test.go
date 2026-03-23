// metrics_test.go verifies that the metrics endpoint exposes Prometheus text output.
package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandler(t *testing.T) {
	t.Parallel()

	resp := httptest.NewRecorder()
	Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	require.Equal(t, http.StatusOK, resp.Code)
	require.True(t, strings.Contains(resp.Body.String(), "# HELP") || strings.Contains(resp.Body.String(), "# TYPE"))
}
