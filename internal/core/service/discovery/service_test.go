// service_test.go verifies discovery snapshots, healthy filtering, and watch event conversion.
package discovery

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
	"etcdisc/test/testkit"
)

func TestSnapshotDefaultsToHealthyOnly(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), namespacesvc.NewFixedClock(time.Now()))
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-2", Address: "127.0.0.2", Port: 8080, Status: model.InstanceStatusUnhealth, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)

	svc := NewService(store, registry)
	items, err := svc.Snapshot(context.Background(), SnapshotInput{Namespace: "prod-core", Service: "payment-api"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "node-1", items[0].InstanceID)

	healthyOnly := false
	items, err = svc.Snapshot(context.Background(), SnapshotInput{Namespace: "prod-core", Service: "payment-api", HealthyOnly: &healthyOnly})
	require.NoError(t, err)
	require.Len(t, items, 2)
}

func TestWatchConvertsStoreEvents(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), namespacesvc.NewFixedClock(time.Now()))
	svc := NewService(store, registry)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchCh := svc.Watch(ctx, WatchInput{Namespace: "prod-core", Service: "payment-api"})
	time.Sleep(20 * time.Millisecond)

	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)

	event := <-watchCh
	require.Equal(t, model.WatchEventPut, event.Type)
	var instance model.Instance
	require.NoError(t, json.Unmarshal(event.Value, &instance))
	require.Equal(t, "node-1", instance.InstanceID)
}

func TestSnapshotAndWatchStayConsistentForRuntimeTransitions(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	baseClock := namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC))
	nsService := namespacesvc.NewService(store, baseClock)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), baseClock)
	registered, err := registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	seedOwnerForDiscovery(t, store, model.ServiceOwner{Namespace: "prod-core", Service: "payment-api", OwnerNodeID: "node-1", Epoch: 1})
	svc := NewService(store, registry)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	includeUnhealthy := false
	watchCh := svc.Watch(ctx, WatchInput{Namespace: "prod-core", Service: "payment-api", HealthyOnly: &includeUnhealthy})

	timeoutSvc := registrysvc.NewService(store, nsService, healthsvc.NewManager(), namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 20, 0, time.UTC)))
	instance, deleted, err := timeoutSvc.ApplyHeartbeatTimeout(context.Background(), registrysvc.HeartbeatTimeoutInput{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", ExpectedRevision: registered.Revision, ExpectedOwnerEpoch: 1})
	require.NoError(t, err)
	require.False(t, deleted)
	event := <-watchCh
	require.Equal(t, model.WatchEventPut, event.Type)
	var watched model.Instance
	require.NoError(t, json.Unmarshal(event.Value, &watched))
	require.Equal(t, model.InstanceStatusUnhealth, watched.Status)

	snapshot, err := svc.Snapshot(context.Background(), SnapshotInput{Namespace: "prod-core", Service: "payment-api"})
	require.NoError(t, err)
	require.Len(t, snapshot, 0)
	includeUnhealthy = false
	snapshot, err = svc.Snapshot(context.Background(), SnapshotInput{Namespace: "prod-core", Service: "payment-api", HealthyOnly: &includeUnhealthy})
	require.NoError(t, err)
	require.Len(t, snapshot, 1)
	require.Equal(t, instance.InstanceID, snapshot[0].InstanceID)

	deleteSvc := registrysvc.NewService(store, nsService, healthsvc.NewManager(), namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 50, 0, time.UTC)))
	_, deleted, err = deleteSvc.ApplyHeartbeatTimeout(context.Background(), registrysvc.HeartbeatTimeoutInput{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", ExpectedRevision: instance.Revision, ExpectedOwnerEpoch: 1})
	require.NoError(t, err)
	require.True(t, deleted)
	event = <-watchCh
	require.Equal(t, model.WatchEventDelete, event.Type)
}

func seedOwnerForDiscovery(t *testing.T, store *testkit.MemoryStore, owner model.ServiceOwner) {
	t.Helper()
	payload, err := json.Marshal(owner)
	require.NoError(t, err)
	_, err = store.Put(context.Background(), keyspace.ServiceOwnerKey(owner.Namespace, owner.Service), payload, 0)
	require.NoError(t, err)
}
