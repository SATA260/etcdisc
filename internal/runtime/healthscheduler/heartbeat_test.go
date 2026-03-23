// heartbeat_test.go verifies heartbeat timeout scheduling and state progression for owned services.
package healthscheduler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	registrysvc "etcdisc/internal/core/service/registry"
	cluster "etcdisc/internal/runtime/cluster"
	"etcdisc/test/testkit"
)

type stubCoordinator struct {
	owned []cluster.OwnedService
}

func (s stubCoordinator) OwnedServices() []cluster.OwnedService {
	return append([]cluster.OwnedService(nil), s.owned...)
}

func TestHeartbeatSupervisorDowngradesOwnedInstance(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	registerClock := namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC))
	nsService := namespacesvc.NewService(store, registerClock)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), registerClock)
	registered, err := registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	ownerPayload, err := json.Marshal(model.ServiceOwner{Namespace: "prod", Service: "pay", OwnerNodeID: "node-1", Epoch: 1, AssignedAt: time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	_, err = store.Create(context.Background(), keyspace.ServiceOwnerKey("prod", "pay"), ownerPayload, 0)
	require.NoError(t, err)
	laterClock := namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 20, 0, time.UTC))
	registry = registrysvc.NewService(store, nsService, healthsvc.NewManager(), laterClock)

	supervisor := NewHeartbeatSupervisor(store, registry, stubCoordinator{owned: []cluster.OwnedService{{Namespace: "prod", Service: "pay", OwnerEpoch: 1, OwnerRevision: 1, InstanceCount: 1, Token: 1}}}, laterClock, nil)
	supervisor.syncOwnedServices(context.Background())
	require.NotEmpty(t, supervisor.entries)
	entry := supervisor.entries["prod/pay/node-1"]
	task := &heartbeatTask{ServiceKey: entry.ServiceKey, Namespace: entry.Namespace, Service: entry.Service, InstanceID: entry.InstanceID, Phase: heartbeatPhaseUnhealth, Token: entry.Token, Revision: registered.Revision, OwnerEpoch: 1}

	supervisor.handleTask(context.Background(), task)
	instance, err := registry.Get(context.Background(), "prod", "pay", "node-1")
	require.NoError(t, err)
	require.Equal(t, model.InstanceStatusUnhealth, instance.Status)
}

func TestHeartbeatSupervisorReconcileRebuildsMissingEntries(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC))
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	ownerPayload, err := json.Marshal(model.ServiceOwner{Namespace: "prod", Service: "pay", OwnerNodeID: "node-1", Epoch: 1})
	require.NoError(t, err)
	_, err = store.Create(context.Background(), keyspace.ServiceOwnerKey("prod", "pay"), ownerPayload, 0)
	require.NoError(t, err)

	supervisor := NewHeartbeatSupervisor(store, registry, stubCoordinator{owned: []cluster.OwnedService{{Namespace: "prod", Service: "pay", OwnerEpoch: 1, OwnerRevision: 1, InstanceCount: 1}}}, clk, nil)
	supervisor.ReconcileNow(context.Background())
	require.Len(t, supervisor.entries, 1)

	delete(supervisor.entries, "prod/pay/node-1")
	require.Len(t, supervisor.entries, 0)
	supervisor.ReconcileNow(context.Background())
	require.Len(t, supervisor.entries, 1)
}
