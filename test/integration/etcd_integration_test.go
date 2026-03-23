// etcd_integration_test.go exercises the real-etcd storage, registry, and config paths required by phase 1.
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	appconfig "etcdisc/internal/app/config"
	"etcdisc/internal/core/model"
	configsvc "etcdisc/internal/core/service/config"
	discoverysvc "etcdisc/internal/core/service/discovery"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	probesvc "etcdisc/internal/core/service/probe"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/internal/infra/clock"
	infraetcd "etcdisc/internal/infra/etcd"
	cluster "etcdisc/internal/runtime/cluster"
	healthscheduler "etcdisc/internal/runtime/healthscheduler"
)

func TestRealEtcdPhase1Flows(t *testing.T) {
	if os.Getenv("ETCDISC_RUN_INTEGRATION") != "1" {
		t.Skip("set ETCDISC_RUN_INTEGRATION=1 to run real etcd integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := appconfig.Default()
	client, err := infraetcd.NewClient(cfg)
	require.NoError(t, err)
	defer client.Close()
	ready := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := client.Get(probeCtx, "health")
		probeCancel()
		if err == nil {
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		t.Skip("local etcd did not become ready in time")
	}
	store := infraetcd.NewStore(client)
	clk := clock.RealClock{}
	namespaceService := namespacesvc.NewService(store, clk)
	registryService := registrysvc.NewService(store, namespaceService, healthsvc.NewManager(), clk)
	discoveryService := discoverysvc.NewService(store, registryService)
	configService := configsvc.NewService(store, namespaceService, clk)

	_, _ = client.Delete(ctx, "/etcdisc", clientv3.WithPrefix())
	_, err = namespaceService.Create(ctx, namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)

	instance, err := registryService.Register(ctx, registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	require.NotZero(t, instance.Revision)
	items, err := discoveryService.Snapshot(ctx, discoverysvc.SnapshotInput{Namespace: "prod-core", Service: "payment-api"})
	require.NoError(t, err)
	require.Len(t, items, 1)

	_, err = configService.Put(ctx, configsvc.PutInput{Item: model.ConfigItem{Scope: model.ConfigScopeService, Namespace: "prod-core", Service: "payment-api", Key: "timeout.request", Value: "1000", ValueType: model.ConfigValueDuration}})
	require.NoError(t, err)
	effective, err := configService.Resolve(ctx, configsvc.ResolveInput{Namespace: "prod-core", Service: "payment-api"})
	require.NoError(t, err)
	require.Equal(t, "1000", effective["timeout.request"].Value)
}

func TestRealEtcdClusterRuntimeFlows(t *testing.T) {
	if os.Getenv("ETCDISC_RUN_INTEGRATION") != "1" {
		t.Skip("set ETCDISC_RUN_INTEGRATION=1 to run real etcd integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := appconfig.Default()
	client1, err := infraetcd.NewClient(cfg)
	require.NoError(t, err)
	defer client1.Close()
	client2, err := infraetcd.NewClient(cfg)
	require.NoError(t, err)
	defer client2.Close()
	_, _ = client1.Delete(ctx, "/etcdisc", clientv3.WithPrefix())
	store1 := infraetcd.NewStore(client1)
	store2 := infraetcd.NewStore(client2)
	clk := clock.RealClock{}
	namespaceService := namespacesvc.NewService(store1, clk)
	_, err = namespaceService.Create(ctx, namespacesvc.CreateNamespaceInput{Name: "prod-runtime"})
	require.NoError(t, err)
	registry1 := registrysvc.NewService(store1, namespaceService, healthsvc.NewManager(), clk)
	registry2 := registrysvc.NewService(store2, namespaceService, healthsvc.NewManager(), clk)
	coord1, err := cluster.NewCoordinator(store1, nil, clk, cluster.Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, coord1.Start(ctx))
	defer func() { _ = coord1.Close() }()
	coord2, err := cluster.NewCoordinator(store2, nil, clk, cluster.Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, coord2.Start(ctx))
	defer func() { _ = coord2.Close() }()
	registry1.SetIngressRecorder(coord1)
	registry2.SetIngressRecorder(coord2)
	hb1 := healthscheduler.NewHeartbeatSupervisor(store1, registry1, coord1, clk, nil)
	hb1.Start()
	defer func() { _ = hb1.Close() }()
	hb2 := healthscheduler.NewHeartbeatSupervisor(store2, registry2, coord2, clk, nil)
	hb2.Start()
	defer func() { _ = hb2.Close() }()
	probe1 := healthscheduler.NewProbeScheduler(store1, registry1, coord1, probesvc.NewService(), clk, nil)
	probe1.Start()
	defer func() { _ = probe1.Close() }()
	probe2 := healthscheduler.NewProbeScheduler(store2, registry2, coord2, probesvc.NewService(), clk, nil)
	probe2.Start()
	defer func() { _ = probe2.Close() }()

	require.Eventually(t, func() bool { return coord1.IsLeader() != coord2.IsLeader() }, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, coord2.RecordServiceSeed(ctx, "prod-runtime", "payment-api", 1))
	_, err = registry1.Register(ctx, registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-runtime", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	require.NoError(t, coord2.RecordServiceSeed(ctx, "prod-runtime", "probe-api", 2))
	_, err = registry1.Register(ctx, registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod-runtime", Service: "probe-api", InstanceID: "node-2", Address: "127.0.0.2", Port: 8081, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(coord1.OwnedServices()) >= 1 || len(coord2.OwnedServices()) >= 1
	}, 10*time.Second, 100*time.Millisecond)
}
