// failover_test.go verifies multi-node runtime failover for heartbeat and probe ownership activation.
package healthscheduler

import (
	"context"
	"net"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"

	appconfig "etcdisc/internal/app/config"
	"etcdisc/internal/core/model"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	probesvc "etcdisc/internal/core/service/probe"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/internal/infra/clock"
	infraetcd "etcdisc/internal/infra/etcd"
	"etcdisc/internal/infra/logging"
	cluster "etcdisc/internal/runtime/cluster"
)

func TestRuntimeFailoverRebuildsHeartbeatAndProbeOwnership(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcdForRuntime(t)
	defer stopEtcd()

	client1 := mustRuntimeClient(t, endpoint)
	defer client1.Close()
	client2 := mustRuntimeClient(t, endpoint)
	defer client2.Close()
	store1 := infraetcd.NewStore(client1)
	store2 := infraetcd.NewStore(client2)
	clk := namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC))
	nsService := namespacesvc.NewService(store1, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store1, nsService, healthsvc.NewManager(), clk)

	coord1, err := cluster.NewCoordinator(store1, logging.New(), clock.RealClock{}, cluster.Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, coord1.Start(context.Background()))
	defer func() { _ = coord1.Close() }()
	coord2, err := cluster.NewCoordinator(store2, logging.New(), clock.RealClock{}, cluster.Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, coord2.Start(context.Background()))
	defer func() { _ = coord2.Close() }()

	hb1 := NewHeartbeatSupervisor(store1, registry, coord1, clk, logging.New())
	hb1.Start()
	defer func() { _ = hb1.Close() }()
	hb2 := NewHeartbeatSupervisor(store2, registry, coord2, clk, logging.New())
	hb2.Start()
	defer func() { _ = hb2.Close() }()
	probe1 := NewProbeScheduler(store1, registry, coord1, probesvc.NewService(), clk, logging.New())
	probe1.Start()
	defer func() { _ = probe1.Close() }()
	probe2 := NewProbeScheduler(store2, registry, coord2, probesvc.NewService(), clk, logging.New())
	probe2.Start()
	defer func() { _ = probe2.Close() }()

	require.Eventually(t, func() bool { return coord1.IsLeader() != coord2.IsLeader() }, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, coord2.RecordServiceSeed(context.Background(), "prod", "heartbeat-api", 1))
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "heartbeat-api", InstanceID: "hb-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	require.NoError(t, coord2.RecordServiceSeed(context.Background(), "prod", "probe-api", 2))
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "probe-api", InstanceID: "pb-1", Address: "127.0.0.1", Port: 8081, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		leftOwns := len(coord1.OwnedServices()) >= 2 && len(hb1.entries) == 1 && len(probe1.entries) == 1
		rightOwns := len(coord2.OwnedServices()) >= 2 && len(hb2.entries) == 1 && len(probe2.entries) == 1
		return leftOwns || rightOwns
	}, 10*time.Second, 100*time.Millisecond)

	closeOwnedSide := func() error {
		if len(coord2.OwnedServices()) >= 2 {
			return coord2.Close()
		}
		return coord1.Close()
	}
	require.NoError(t, closeOwnedSide())
	require.Eventually(t, func() bool {
		return len(coord1.OwnedServices()) >= 2 || len(coord2.OwnedServices()) >= 2
	}, 10*time.Second, 200*time.Millisecond)
}

func TestRuntimeRestartRebuildsOwnedEntries(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcdForRuntime(t)
	defer stopEtcd()

	client := mustRuntimeClient(t, endpoint)
	defer client.Close()
	store := infraetcd.NewStore(client)
	clk := namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC))
	nsService := namespacesvc.NewService(store, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry := registrysvc.NewService(store, nsService, healthsvc.NewManager(), clk)
	coord, err := cluster.NewCoordinator(store, logging.New(), clock.RealClock{}, cluster.Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, coord.Start(context.Background()))
	defer func() { _ = coord.Close() }()
	require.Eventually(t, func() bool { return coord.IsLeader() }, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, coord.RecordServiceSeed(context.Background(), "prod", "heartbeat-api", 1))
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "heartbeat-api", InstanceID: "hb-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	require.NoError(t, coord.RecordServiceSeed(context.Background(), "prod", "probe-api", 2))
	_, err = registry.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "probe-api", InstanceID: "pb-1", Address: "127.0.0.1", Port: 8081, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)

	hb := NewHeartbeatSupervisor(store, registry, coord, clk, logging.New())
	hb.Start()
	probe := NewProbeScheduler(store, registry, coord, probesvc.NewService(), clk, logging.New())
	probe.Start()
	require.Eventually(t, func() bool { return len(hb.entries) == 1 && len(probe.entries) == 1 }, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, hb.Close())
	require.NoError(t, probe.Close())

	hb2 := NewHeartbeatSupervisor(store, registry, coord, clk, logging.New())
	hb2.ReconcileNow(context.Background())
	probe2 := NewProbeScheduler(store, registry, coord, probesvc.NewService(), clk, logging.New())
	probe2.ReconcileNow(context.Background())
	require.Len(t, hb2.entries, 1)
	require.Len(t, probe2.entries, 1)
}

func startEmbeddedEtcdForRuntime(t *testing.T) (string, func()) {
	t.Helper()
	cfg := embed.NewConfig()
	cfg.Dir = filepath.Join(t.TempDir(), "etcd")
	cfg.Logger = "zap"
	cfg.LogLevel = "error"
	clientURL := mustRuntimeURL(t, reserveRuntimeAddress(t))
	peerURL := mustRuntimeURL(t, reserveRuntimeAddress(t))
	cfg.ListenClientUrls = []url.URL{clientURL}
	cfg.AdvertiseClientUrls = []url.URL{clientURL}
	cfg.ListenPeerUrls = []url.URL{peerURL}
	cfg.AdvertisePeerUrls = []url.URL{peerURL}
	cfg.InitialCluster = cfg.InitialClusterFromName(cfg.Name)
	e, err := embed.StartEtcd(cfg)
	require.NoError(t, err)
	select {
	case <-e.Server.ReadyNotify():
	case <-time.After(10 * time.Second):
		e.Server.Stop()
		t.Fatal("embedded etcd did not become ready in time")
	}
	return cfg.ListenClientUrls[0].String(), func() { e.Close() }
}

func mustRuntimeClient(t *testing.T, endpoint string) *clientv3.Client {
	t.Helper()
	cfg := appconfig.Default()
	cfg.Etcd.Endpoints = []string{endpoint}
	client, err := infraetcd.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func reserveRuntimeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().String()
}

func mustRuntimeURL(t *testing.T, address string) url.URL {
	t.Helper()
	parsed, err := url.Parse("http://" + address)
	require.NoError(t, err)
	return *parsed
}
