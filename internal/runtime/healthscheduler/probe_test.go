// probe_test.go verifies owner-driven probe scheduling and result application.
package healthscheduler

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	probesvc "etcdisc/internal/core/service/probe"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/internal/infra/clock"
	cluster "etcdisc/internal/runtime/cluster"
	"etcdisc/test/testkit"
)

type fakeProbeRunner struct{ result probesvc.Result }

func (f fakeProbeRunner) Probe(context.Context, model.Instance) probesvc.Result { return f.result }

type countingProbeRunner struct{ count atomic.Int64 }

func (c *countingProbeRunner) Probe(context.Context, model.Instance) probesvc.Result {
	c.count.Add(1)
	return probesvc.Result{Success: true}
}

func TestProbeSchedulerAppliesProbeResults(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC))
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	registered, err := registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)
	ownerPayload, err := json.Marshal(model.ServiceOwner{Namespace: "prod", Service: "pay", OwnerNodeID: "node-1", Epoch: 1})
	require.NoError(t, err)
	_, err = store.Create(context.Background(), keyspace.ServiceOwnerKey("prod", "pay"), ownerPayload, 0)
	require.NoError(t, err)

	scheduler := NewProbeScheduler(store, registry, stubCoordinator{owned: []cluster.OwnedService{{Namespace: "prod", Service: "pay", OwnerEpoch: 1, InstanceCount: 1}}}, fakeProbeRunner{result: probesvc.Result{Success: false}}, clk, nil)
	record, err := store.Get(context.Background(), keyspace.InstanceKey("prod", "pay", "node-1"))
	require.NoError(t, err)
	entry, ok := decodeProbeEntry(record, cluster.OwnedService{Namespace: "prod", Service: "pay", OwnerEpoch: 1})
	require.True(t, ok)
	entry.Token = 1
	scheduler.entries["prod/pay/node-1"] = entry
	scheduler.runProbeJob(probeJob{Entry: entry})
	updated, err := registry.Get(context.Background(), "prod", "pay", "node-1")
	require.NoError(t, err)
	require.GreaterOrEqual(t, updated.Revision, registered.Revision)
}

func TestProbeSchedulerReconcileRebuildsMissingEntries(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC))
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)
	ownerPayload, err := json.Marshal(model.ServiceOwner{Namespace: "prod", Service: "pay", OwnerNodeID: "node-1", Epoch: 1})
	require.NoError(t, err)
	_, err = store.Create(context.Background(), keyspace.ServiceOwnerKey("prod", "pay"), ownerPayload, 0)
	require.NoError(t, err)

	scheduler := NewProbeScheduler(store, registry, stubCoordinator{owned: []cluster.OwnedService{{Namespace: "prod", Service: "pay", OwnerEpoch: 1, OwnerRevision: 1, InstanceCount: 1}}}, fakeProbeRunner{result: probesvc.Result{Success: true}}, clk, nil)
	scheduler.ReconcileNow(context.Background())
	require.Len(t, scheduler.entries, 1)

	delete(scheduler.entries, "prod/pay/node-1")
	require.Len(t, scheduler.entries, 0)
	scheduler.ReconcileNow(context.Background())
	require.Len(t, scheduler.entries, 1)
}

func TestProbeSchedulerContinuouslySchedulesTasks(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	clk := namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC))
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "pay", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)
	ownerPayload, err := json.Marshal(model.ServiceOwner{Namespace: "prod", Service: "pay", OwnerNodeID: "node-1", Epoch: 1})
	require.NoError(t, err)
	_, err = store.Create(context.Background(), keyspace.ServiceOwnerKey("prod", "pay"), ownerPayload, 0)
	require.NoError(t, err)
	runner := &countingProbeRunner{}
	scheduler := NewProbeScheduler(store, registry, stubCoordinator{owned: []cluster.OwnedService{{Namespace: "prod", Service: "pay", OwnerEpoch: 1, OwnerRevision: 1, InstanceCount: 1}}}, runner, clock.RealClock{}, nil)
	scheduler.interval = 50 * time.Millisecond
	scheduler.timeout = 50 * time.Millisecond
	scheduler.concurrency = 1
	scheduler.jobs = make(chan probeJob, 8)
	scheduler.Start()
	defer func() { _ = scheduler.Close() }()

	require.Eventually(t, func() bool {
		return runner.count.Load() >= 2
	}, 3*time.Second, 50*time.Millisecond)
}
