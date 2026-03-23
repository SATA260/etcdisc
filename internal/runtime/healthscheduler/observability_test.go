// observability_test.go verifies runtime cluster and health metrics are exposed through the metrics endpoint.
package healthscheduler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"etcdisc/internal/infra/metrics"
)

func TestRuntimeMetricsAreExposed(t *testing.T) {
	t.Parallel()

	heartbeatStateTransitions.WithLabelValues("unhealth").Inc()
	runtimeDeleteTotal.Inc()
	runtimeOwnerEpochRejects.Inc()
	runtimeRevisionConflicts.Inc()
	runtimeRebuildTotal.Inc()
	runtimeReconcileTotal.Inc()

	resp := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := resp.Body.String()
	require.Equal(t, http.StatusOK, resp.Code)
	for _, name := range []string{
		"etcdisc_heartbeat_state_transition_total",
		"etcdisc_runtime_delete_total",
		"etcdisc_runtime_owner_epoch_reject_total",
		"etcdisc_runtime_revision_conflict_total",
		"etcdisc_runtime_rebuild_total",
		"etcdisc_runtime_reconcile_total",
	} {
		require.True(t, strings.Contains(body, name), name)
	}
}
