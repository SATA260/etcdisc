// coordinator_test.go verifies worker membership, single-leader election, and automatic leader failover with real etcd.
package cluster

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
	"etcdisc/internal/infra/clock"
	infraetcd "etcdisc/internal/infra/etcd"
	"etcdisc/internal/infra/logging"
)

func TestCoordinatorRegistersMembersAndElectsSingleLeader(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup2() }()

	require.Eventually(t, func() bool {
		return len(coord1.Members()) == 2 && len(coord2.Members()) == 2
	}, 10*time.Second, 100*time.Millisecond)

	require.Eventually(t, func() bool {
		return coord1.IsLeader() != coord2.IsLeader()
	}, 10*time.Second, 100*time.Millisecond)

	leader1, ok1 := coord1.CurrentLeader()
	leader2, ok2 := coord2.CurrentLeader()
	require.True(t, ok1)
	require.True(t, ok2)
	require.Equal(t, leader1.NodeID, leader2.NodeID)
}

func TestCoordinatorFailoverAfterLeaderStops(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	defer func() { _ = cleanup2() }()

	require.Eventually(t, func() bool {
		return coord1.IsLeader() != coord2.IsLeader()
	}, 10*time.Second, 100*time.Millisecond)

	var follower *Coordinator
	var leaderCleanup func() error
	if coord1.IsLeader() {
		follower = coord2
		leaderCleanup = cleanup1
	} else {
		follower = coord1
		leaderCleanup = cleanup2
	}

	require.NoError(t, leaderCleanup())
	require.Eventually(t, func() bool {
		return follower.IsLeader()
	}, 10*time.Second, 100*time.Millisecond)

	leaderRecord, ok := follower.CurrentLeader()
	require.True(t, ok)
	require.Equal(t, follower.config.NodeID, leaderRecord.NodeID)
}

func mustCoordinator(t *testing.T, endpoint string, cfg Config) (*Coordinator, func() error) {
	t.Helper()
	client := mustEtcdClient(t, endpoint)
	store := infraetcd.NewStore(client)
	coordinator, err := NewCoordinator(store, logging.New(), clock.RealClock{}, cfg)
	require.NoError(t, err)
	require.NoError(t, coordinator.Start(context.Background()))
	cleanup := func() error {
		err := coordinator.Close()
		_ = client.Close()
		return err
	}
	return coordinator, cleanup
}

func mustEtcdClient(t *testing.T, endpoint string) *clientv3.Client {
	t.Helper()
	cfg := appconfig.Default()
	cfg.Etcd.Endpoints = []string{endpoint}
	client, err := infraetcd.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func startEmbeddedEtcd(t *testing.T) (string, func()) {
	t.Helper()
	cfg := embed.NewConfig()
	cfg.Dir = filepath.Join(t.TempDir(), "etcd")
	cfg.Logger = "zap"
	cfg.LogLevel = "error"
	clientURL := mustURL(t, reserveAddress(t))
	peerURL := mustURL(t, reserveAddress(t))
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

func reserveAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().String()
}

func mustURL(t *testing.T, address string) url.URL {
	t.Helper()
	parsed, err := url.Parse("http://" + address)
	require.NoError(t, err)
	return *parsed
}
