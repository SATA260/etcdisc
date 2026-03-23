// coordinator_test.go verifies worker membership, single-leader election, and automatic leader failover with real etcd.
package cluster

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
	client := mustEtcdClient(t, endpoint)
	defer client.Close()

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
	_, err := client.Delete(context.Background(), keyspace.AssignmentLeaderKey())
	require.NoError(t, err)
	follower.signalElection()
	require.Eventually(t, func() bool {
		return follower.IsLeader()
	}, 10*time.Second, 100*time.Millisecond)

	leaderRecord, ok := follower.CurrentLeader()
	require.True(t, ok)
	require.Equal(t, follower.config.NodeID, leaderRecord.NodeID)
}

func TestCoordinatorAssignsInitialOwnerToFirstIngressNode(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()
	client := mustEtcdClient(t, endpoint)
	defer client.Close()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup2() }()

	require.Eventually(t, func() bool {
		return coord1.IsLeader() != coord2.IsLeader()
	}, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, coord2.RecordServiceSeed(context.Background(), "prod", "payment-api"))
	require.Eventually(t, func() bool {
		owner, ok := mustReadOwner(t, client, "prod", "payment-api")
		return ok && owner.OwnerNodeID == "node-2" && owner.Epoch == 1
	}, 10*time.Second, 100*time.Millisecond)
}

func TestCoordinatorReassignsOnlyAffectedServiceOnMemberLeave(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()
	client := mustEtcdClient(t, endpoint)
	defer client.Close()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})

	require.Eventually(t, func() bool {
		return coord1.IsLeader() != coord2.IsLeader()
	}, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, coord1.RecordServiceSeed(context.Background(), "prod", "order-api"))
	require.NoError(t, coord2.RecordServiceSeed(context.Background(), "prod", "payment-api"))
	require.Eventually(t, func() bool {
		orderOwner, okOrder := mustReadOwner(t, client, "prod", "order-api")
		paymentOwner, okPayment := mustReadOwner(t, client, "prod", "payment-api")
		return okOrder && okPayment && orderOwner.OwnerNodeID == "node-1" && paymentOwner.OwnerNodeID == "node-2"
	}, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, cleanup2())
	require.Eventually(t, func() bool {
		paymentOwner, okPayment := mustReadOwner(t, client, "prod", "payment-api")
		orderOwner, okOrder := mustReadOwner(t, client, "prod", "order-api")
		return okPayment && okOrder && paymentOwner.OwnerNodeID == "node-1" && paymentOwner.Epoch > 1 && orderOwner.OwnerNodeID == "node-1" && orderOwner.Epoch == 1
	}, 10*time.Second, 100*time.Millisecond)
}

func TestCoordinatorDoesNotRebalanceAllServicesOnMemberJoin(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()
	client := mustEtcdClient(t, endpoint)
	defer client.Close()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	require.Eventually(t, func() bool { return coord1.IsLeader() }, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, coord1.RecordServiceSeed(context.Background(), "prod", "payment-api"))
	require.Eventually(t, func() bool {
		owner, ok := mustReadOwner(t, client, "prod", "payment-api")
		return ok && owner.OwnerNodeID == "node-1" && owner.Epoch == 1
	}, 10*time.Second, 100*time.Millisecond)

	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup2() }()
	require.NotNil(t, coord2)
	time.Sleep(2 * time.Second)
	owner, ok := mustReadOwner(t, client, "prod", "payment-api")
	require.True(t, ok)
	require.Equal(t, "node-1", owner.OwnerNodeID)
	require.Equal(t, int64(1), owner.Epoch)
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

func mustReadOwner(t *testing.T, client *clientv3.Client, namespaceName, serviceName string) (model.ServiceOwner, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := client.Get(ctx, keyspace.ServiceOwnerKey(namespaceName, serviceName))
	require.NoError(t, err)
	if len(resp.Kvs) == 0 {
		return model.ServiceOwner{}, false
	}
	var owner model.ServiceOwner
	require.NoError(t, json.Unmarshal(resp.Kvs[0].Value, &owner))
	owner.Revision = resp.Kvs[0].ModRevision
	return owner, true
}
