// keys_test.go verifies that etcd keys remain stable and namespace-aware.
package keyspace

import (
	"testing"

	"github.com/stretchr/testify/require"

	"etcdisc/internal/core/model"
)

func TestKeyBuilders(t *testing.T) {
	t.Parallel()

	require.Equal(t, "/etcdisc/namespaces/", NamespacePrefix())
	require.Equal(t, "/etcdisc/namespaces/prod", NamespaceKey("prod"))
	require.Equal(t, "/etcdisc/instances/prod/", InstanceNamespacePrefix("prod"))
	require.Equal(t, "/etcdisc/instances/prod/pay/node-1", InstanceKey("prod", "pay", "node-1"))
	require.Equal(t, "/etcdisc/instances/prod/pay/", InstanceServicePrefix("prod", "pay"))
	require.Equal(t, "/etcdisc/instances/", InstanceRootPrefix())
	require.Equal(t, "/etcdisc/config/global/timeout.request", ConfigKey(model.ConfigScopeGlobal, "", "", "timeout.request"))
	require.Equal(t, "/etcdisc/config/namespaces/prod/timeout.request", ConfigKey(model.ConfigScopeNamespace, "prod", "", "timeout.request"))
	require.Equal(t, "/etcdisc/config/services/prod/pay/timeout.request", ConfigKey(model.ConfigScopeService, "prod", "pay", "timeout.request"))
	require.Equal(t, "/etcdisc/config/global/", ConfigPrefix(model.ConfigScopeGlobal, "", ""))
	require.Equal(t, "/etcdisc/config/namespaces/prod/", ConfigPrefix(model.ConfigScopeNamespace, "prod", ""))
	require.Equal(t, "/etcdisc/config/services/prod/pay/", ConfigPrefix(model.ConfigScopeService, "prod", "pay"))
	require.Equal(t, "/etcdisc/config/", ConfigRootPrefix())
	require.Equal(t, "/etcdisc/agentcards/prod/agent-1", AgentCardKey("prod", "agent-1"))
	require.Equal(t, "/etcdisc/agentcards/prod/", AgentCardNamespacePrefix("prod"))
	require.Equal(t, "/etcdisc/capabilities/prod/tool.search/agent-1", CapabilityKey("prod", "tool.search", "agent-1"))
	require.Equal(t, "/etcdisc/capabilities/prod/tool.search/", CapabilityPrefix("prod", "tool.search"))
	require.Equal(t, "/etcdisc/audit/", AuditPrefix())
	require.Equal(t, "/etcdisc/runtime/", RuntimeRootPrefix())
	require.Equal(t, "/etcdisc/runtime/assignment/leader", AssignmentLeaderKey())
	require.Equal(t, "/etcdisc/runtime/members/node-1", WorkerMemberKey("node-1"))
	require.Equal(t, "/etcdisc/runtime/members/", WorkerMemberPrefix())
	require.Equal(t, "/etcdisc/runtime/owners/prod/pay", ServiceOwnerKey("prod", "pay"))
	require.Equal(t, "/etcdisc/runtime/owners/", ServiceOwnerPrefix())
	require.Equal(t, "/etcdisc/runtime/owners/prod/", ServiceOwnerNamespacePrefix("prod"))
	require.Equal(t, "/etcdisc/runtime/service-seeds/prod/pay", ServiceSeedKey("prod", "pay"))
	require.Equal(t, "/etcdisc/runtime/service-seeds/", ServiceSeedPrefix())
}

func TestResourceFromKey(t *testing.T) {
	t.Parallel()

	require.Equal(t, "namespace", ResourceFromKey(NamespaceKey("prod")))
	require.Equal(t, "instance", ResourceFromKey(InstanceKey("prod", "pay", "node-1")))
	require.Equal(t, "config", ResourceFromKey(ConfigKey(model.ConfigScopeGlobal, "", "", "timeout.request")))
	require.Equal(t, "agentcard", ResourceFromKey(AgentCardKey("prod", "agent-1")))
	require.Equal(t, "capability", ResourceFromKey(CapabilityKey("prod", "tool.search", "agent-1")))
	require.Equal(t, "audit", ResourceFromKey(AuditKey("evt-1")))
	require.Equal(t, "runtime", ResourceFromKey(AssignmentLeaderKey()))
	require.Equal(t, "runtime", ResourceFromKey(WorkerMemberKey("node-1")))
	require.Equal(t, "runtime", ResourceFromKey(ServiceOwnerKey("prod", "pay")))
	require.Equal(t, "runtime", ResourceFromKey(ServiceSeedKey("prod", "pay")))
	require.Equal(t, "unknown", ResourceFromKey("/other/path"))
}
