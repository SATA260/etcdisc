// cluster_runtime_e2e_test.go exercises clustered runtime ownership, failover, and stale owner fencing paths.
package e2e

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"

	appconfig "etcdisc/internal/app/config"
	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	probesvc "etcdisc/internal/core/service/probe"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/internal/infra/clock"
	infraetcd "etcdisc/internal/infra/etcd"
	"etcdisc/internal/infra/logging"
	cluster "etcdisc/internal/runtime/cluster"
	healthscheduler "etcdisc/internal/runtime/healthscheduler"
)

func TestClusterRuntimeAnyNodeIngressButOwnerExclusiveMaintenance(t *testing.T) {
	endpoint, stopEtcd := startClusterRuntimeEtcd(t)
	defer stopEtcd()

	client1 := mustClusterRuntimeClient(t, endpoint)
	defer client1.Close()
	client2 := mustClusterRuntimeClient(t, endpoint)
	defer client2.Close()
	store1 := infraetcd.NewStore(client1)
	store2 := infraetcd.NewStore(client2)
	clk := namespacesvc.NewFixedClock(time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC))
	nsService := namespacesvc.NewService(store1, clk)
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod"})
	require.NoError(t, err)
	registry1 := registrysvc.NewService(store1, nsService, healthsvc.NewManager(), clk)
	registry2 := registrysvc.NewService(store2, nsService, healthsvc.NewManager(), clk)

	coord1, hb1, probe1, cleanup1 := mustRuntimeNode(t, store1, registry1, "node-1")
	defer cleanup1()
	coord2, hb2, probe2, cleanup2 := mustRuntimeNode(t, store2, registry2, "node-2")
	defer cleanup2()
	require.Eventually(t, func() bool { return coord1.IsLeader() != coord2.IsLeader() }, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, coord2.RecordServiceSeed(context.Background(), "prod", "payment-api", 1))
	registered, err := registry1.Register(context.Background(), registrysvc.RegisterInput{Instance: model.Instance{Namespace: "prod", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		leftOwns := len(coord1.OwnedServices()) == 1 && len(hb1.OwnedEntries()) == 1 && len(probe1.OwnedEntries()) == 0
		rightOwns := len(coord2.OwnedServices()) == 1 && len(hb2.OwnedEntries()) == 1 && len(probe2.OwnedEntries()) == 0
		return leftOwns || rightOwns
	}, 10*time.Second, 100*time.Millisecond)

	require.Eventually(t, func() bool {
		owned := coord2.OwnedServices()
		return len(owned) == 1 && owned[0].Service == "payment-api"
	}, 10*time.Second, 100*time.Millisecond)
	oldEpoch := coord2.OwnedServices()[0].OwnerEpoch
	require.NoError(t, cleanup2())
	require.Eventually(t, func() bool {
		owned := coord1.OwnedServices()
		return len(owned) == 1 && owned[0].Service == "payment-api" && owned[0].OwnerEpoch > oldEpoch
	}, 10*time.Second, 100*time.Millisecond)

	_, _, err = registry1.ApplyHeartbeatTimeout(context.Background(), registrysvc.HeartbeatTimeoutInput{Namespace: "prod", Service: "payment-api", InstanceID: registered.InstanceID, ExpectedRevision: registered.Revision, ExpectedOwnerEpoch: oldEpoch})
	require.Error(t, err)
	require.Contains(t, err.Error(), "epoch")
}

func TestClusterRuntimeServiceOwnerTTLCleanup(t *testing.T) {
	endpoint, stopEtcd := startClusterRuntimeEtcd(t)
	defer stopEtcd()
	client := mustClusterRuntimeClient(t, endpoint)
	defer client.Close()
	store := infraetcd.NewStore(client)
	registry := registrysvc.NewService(store, namespacesvc.NewService(store, clock.RealClock{}), healthsvc.NewManager(), clock.RealClock{})
	coord1, _, _, cleanup1 := mustRuntimeNode(t, store, registry, "node-1")
	defer cleanup1()
	client2 := mustClusterRuntimeClient(t, endpoint)
	defer client2.Close()
	store2 := infraetcd.NewStore(client2)
	registry2 := registrysvc.NewService(store2, namespacesvc.NewService(store2, clock.RealClock{}), healthsvc.NewManager(), clock.RealClock{})
	coord2, _, _, cleanup2 := mustRuntimeNode(t, store2, registry2, "node-2")
	defer cleanup2()
	require.Eventually(t, func() bool { return coord1.IsLeader() || coord2.IsLeader() }, 10*time.Second, 100*time.Millisecond)
	leader := coord1
	nonLeaderCleanup := cleanup2
	if coord2.IsLeader() {
		leader = coord2
		nonLeaderCleanup = cleanup1
	}
	require.NoError(t, nonLeaderCleanup())
	require.NoError(t, leader.RecordServiceSeed(context.Background(), "prod", "ttl-api", 1))
	require.Eventually(t, func() bool {
		owner, ok := readClusterRuntimeOwner(t, client, "prod", "ttl-api")
		return ok && owner.OwnerNodeID != ""
	}, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, leader.ReconcileNow(context.Background()))
	owner, ok := readClusterRuntimeOwner(t, client, "prod", "ttl-api")
	require.True(t, ok)
	require.False(t, owner.ExpiresAt.IsZero())
	owner.ExpiresAt = time.Now().Add(-time.Minute)
	payload, err := json.Marshal(owner)
	require.NoError(t, err)
	_, err = client.Put(context.Background(), keyspace.ServiceOwnerKey("prod", "ttl-api"), string(payload))
	require.NoError(t, err)
	require.NoError(t, leader.ReconcileNow(context.Background()))
	_, ok = readClusterRuntimeOwner(t, client, "prod", "ttl-api")
	require.False(t, ok)
}

func mustRuntimeNode(t *testing.T, store *infraetcd.Store, registry *registrysvc.Service, nodeID string) (*cluster.Coordinator, *healthscheduler.HeartbeatSupervisor, *healthscheduler.ProbeScheduler, func() error) {
	t.Helper()
	coord, err := cluster.NewCoordinator(store, logging.New(), clock.RealClock{}, cluster.Config{Enabled: true, NodeID: nodeID, HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	require.NoError(t, err)
	require.NoError(t, coord.Start(context.Background()))
	registry.SetIngressRecorder(coord)
	hb := healthscheduler.NewHeartbeatSupervisor(store, registry, coord, clock.RealClock{}, logging.New())
	hb.Start()
	probe := healthscheduler.NewProbeScheduler(store, registry, coord, probesvc.NewService(), clock.RealClock{}, logging.New())
	probe.Start()
	cleanup := func() error {
		_ = hb.Close()
		_ = probe.Close()
		return coord.Close()
	}
	return coord, hb, probe, cleanup
}

func mustClusterRuntimeClient(t *testing.T, endpoint string) *clientv3.Client {
	t.Helper()
	cfg := appconfig.Default()
	cfg.Etcd.Endpoints = []string{endpoint}
	client, err := infraetcd.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func startClusterRuntimeEtcd(t *testing.T) (string, func()) {
	t.Helper()
	cfg := embed.NewConfig()
	cfg.Dir = filepath.Join(t.TempDir(), "etcd")
	cfg.Logger = "zap"
	cfg.LogLevel = "error"
	clientURL := mustClusterRuntimeURL(t, reserveClusterRuntimeAddress(t))
	peerURL := mustClusterRuntimeURL(t, reserveClusterRuntimeAddress(t))
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

func reserveClusterRuntimeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().String()
}

func mustClusterRuntimeURL(t *testing.T, address string) url.URL {
	t.Helper()
	parsed, err := url.Parse("http://" + address)
	require.NoError(t, err)
	return *parsed
}

func readClusterRuntimeOwner(t *testing.T, client *clientv3.Client, namespaceName, serviceName string) (model.ServiceOwner, bool) {
	t.Helper()
	resp, err := client.Get(context.Background(), keyspace.ServiceOwnerKey(namespaceName, serviceName))
	require.NoError(t, err)
	if len(resp.Kvs) == 0 {
		return model.ServiceOwner{}, false
	}
	var owner model.ServiceOwner
	require.NoError(t, json.Unmarshal(resp.Kvs[0].Value, &owner))
	return owner, true
}
