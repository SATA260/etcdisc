// observability_test.go verifies cluster metrics are exposed after coordinator activity.
package cluster

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"etcdisc/internal/infra/metrics"
)

func TestClusterMetricsAreExposed(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()
	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup2() }()
	require.Eventually(t, func() bool { return coord1.IsLeader() != coord2.IsLeader() }, 10*time.Second, 100*time.Millisecond)

	resp := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := resp.Body.String()
	require.Equal(t, http.StatusOK, resp.Code)
	for _, name := range []string{
		"etcdisc_assignment_leader_is_active",
		"etcdisc_cluster_members_total",
		"etcdisc_cluster_member_events_total",
		"etcdisc_assignment_leader_elections_total",
	} {
		require.True(t, strings.Contains(body, name), name)
	}
}
