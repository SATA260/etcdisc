// service_test.go verifies AgentCard CAS updates, capability matching, protocol filtering, and runtime address joins.
package a2a

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/model"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/test/testkit"
)

func TestDiscoverReturnsRuntimeAddressAndProtocolFilter(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), namespacesvc.NewFixedClock(time.Now()))
	a2aService := NewService(store, nsService, registry, namespacesvc.NewFixedClock(time.Now()))

	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "agent-api", AgentID: "agent-1", InstanceID: "inst-1", Address: "10.0.0.9", Port: 9090}})
	require.NoError(t, err)
	card, err := a2aService.Put(context.Background(), PutInput{Card: model.AgentCard{Namespace: "prod-core", AgentID: "agent-1", Service: "agent-api", Capabilities: []string{"tool.search"}, Protocols: []string{"grpc"}, AuthMode: model.AuthModeStaticToken, Metadata: map[string]string{"team": "copilot"}}})
	require.NoError(t, err)

	results, err := a2aService.Discover(context.Background(), DiscoverInput{Namespace: "prod-core", Capability: "tool.search", Protocol: "grpc"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "10.0.0.9", results[0].Address)
	require.Equal(t, 9090, results[0].Port)
	require.Equal(t, model.AuthModeStaticToken, results[0].AuthMode)

	_, err = a2aService.Put(context.Background(), PutInput{Card: model.AgentCard{Namespace: "prod-core", AgentID: "agent-1", Service: "agent-api", Capabilities: []string{"tool.search"}, Protocols: []string{"http"}, AuthMode: model.AuthModeStaticToken}, ExpectedRevision: card.Revision + 1})
	require.Error(t, err)
	require.Equal(t, apperrors.CodeConflict, apperrors.CodeOf(err))
}

func TestDiscoverDefaultsToHealthyOnly(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), namespacesvc.NewFixedClock(time.Now()))
	a2aService := NewService(store, nsService, registry, namespacesvc.NewFixedClock(time.Now()))

	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "agent-api", AgentID: "agent-1", InstanceID: "inst-1", Address: "10.0.0.9", Port: 9090, Status: model.InstanceStatusUnhealth, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)
	_, err = a2aService.Put(context.Background(), PutInput{Card: model.AgentCard{Namespace: "prod-core", AgentID: "agent-1", Service: "agent-api", Capabilities: []string{"tool.search"}, Protocols: []string{"grpc"}, AuthMode: model.AuthModeStaticToken}})
	require.NoError(t, err)

	results, err := a2aService.Discover(context.Background(), DiscoverInput{Namespace: "prod-core", Capability: "tool.search"})
	require.NoError(t, err)
	require.Len(t, results, 0)

	results, err = a2aService.Discover(context.Background(), DiscoverInput{Namespace: "prod-core", Capability: "tool.search", IncludeUnhealthy: true})
	require.NoError(t, err)
	require.Len(t, results, 1)
}
