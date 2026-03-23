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

func TestCoordinatorAutomaticallyReelectsAfterLeaderLeaseRevoked(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup2() }()

	require.Eventually(t, func() bool { return coord1.IsLeader() != coord2.IsLeader() }, 10*time.Second, 100*time.Millisecond)
	leader1, ok := coord1.CurrentLeader()
	require.True(t, ok)
	if coord1.IsLeader() {
		coord1.mu.RLock()
		leaseID := coord1.leaderLeaseID
		coord1.mu.RUnlock()
		require.NotZero(t, leaseID)
		require.NoError(t, coord1.store.RevokeLease(context.Background(), leaseID))
	} else {
		coord2.mu.RLock()
		leaseID := coord2.leaderLeaseID
		coord2.mu.RUnlock()
		require.NotZero(t, leaseID)
		require.NoError(t, coord2.store.RevokeLease(context.Background(), leaseID))
	}

	require.Eventually(t, func() bool {
		leaderA, okA := coord1.CurrentLeader()
		leaderB, okB := coord2.CurrentLeader()
		if !okA || !okB {
			return false
		}
		return leaderA.NodeID == leaderB.NodeID && leaderA.Epoch != leader1.Epoch && (coord1.IsLeader() || coord2.IsLeader())
	}, 10*time.Second, 100*time.Millisecond)
}

func TestCoordinatorRemovesExpiredMemberLease(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup2() }()

	require.Eventually(t, func() bool { return len(coord1.Members()) == 2 && len(coord2.Members()) == 2 }, 10*time.Second, 100*time.Millisecond)
	coord2.mu.RLock()
	memberLeaseID := coord2.memberLeaseID
	coord2.mu.RUnlock()
	require.NotZero(t, memberLeaseID)
	require.NoError(t, coord2.store.RevokeLease(context.Background(), memberLeaseID))
	require.Eventually(t, func() bool {
		return len(coord1.Members()) == 1 || len(coord2.Members()) == 1
	}, 10*time.Second, 100*time.Millisecond)
}

func TestCoordinatorLeaderFailoverSupportsAssignmentRebuild(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()
	client := mustEtcdClient(t, endpoint)
	defer client.Close()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup2() }()
	require.Eventually(t, func() bool { return coord1.IsLeader() != coord2.IsLeader() }, 10*time.Second, 100*time.Millisecond)

	var survivor *Coordinator
	var oldLeaderCleanup func() error
	if coord1.IsLeader() {
		survivor = coord2
		oldLeaderCleanup = cleanup1
	} else {
		survivor = coord1
		oldLeaderCleanup = cleanup2
	}
	require.NoError(t, oldLeaderCleanup())
	_, err := client.Delete(context.Background(), keyspace.AssignmentLeaderKey())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		if survivor.IsLeader() {
			return true
		}
		acquired, acquireErr := survivor.tryAcquireLeadership(context.Background())
		require.NoError(t, acquireErr)
		return acquired || survivor.IsLeader()
	}, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, survivor.RecordServiceSeed(context.Background(), "prod", "after-failover-api", 1))
	require.Eventually(t, func() bool {
		owner, ok := mustReadOwner(t, client, "prod", "after-failover-api")
		return ok && owner.OwnerNodeID == survivor.config.NodeID
	}, 10*time.Second, 100*time.Millisecond)
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

	require.NoError(t, coord2.RecordServiceSeed(context.Background(), "prod", "payment-api", 1))
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

	require.NoError(t, coord1.RecordServiceSeed(context.Background(), "prod", "order-api", 1))
	require.NoError(t, coord2.RecordServiceSeed(context.Background(), "prod", "payment-api", 2))
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
	require.NoError(t, coord1.RecordServiceSeed(context.Background(), "prod", "payment-api", 1))
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

func TestCoordinatorActivatesOwnedServicesFromOwnerWatch(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup2() }()

	require.Eventually(t, func() bool {
		return coord1.IsLeader() != coord2.IsLeader()
	}, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, coord2.RecordServiceSeed(context.Background(), "prod", "payment-api", 1))
	require.Eventually(t, func() bool {
		owned := coord2.OwnedServices()
		return len(owned) == 1 && owned[0].Namespace == "prod" && owned[0].Service == "payment-api" && owned[0].OwnerEpoch == 1
	}, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, cleanup2())
	require.Eventually(t, func() bool {
		owned := coord1.OwnedServices()
		return len(owned) == 1 && owned[0].Namespace == "prod" && owned[0].Service == "payment-api" && owned[0].OwnerEpoch > 1
	}, 10*time.Second, 100*time.Millisecond)
}

func TestCoordinatorRestoresOwnedServicesAfterOwnerWatchResetReload(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	require.Eventually(t, func() bool { return coord1.IsLeader() }, 10*time.Second, 100*time.Millisecond)
	require.NoError(t, coord1.RecordServiceSeed(context.Background(), "prod", "watch-reset-api", 1))
	require.Eventually(t, func() bool {
		owned := coord1.OwnedServices()
		return len(owned) == 1 && owned[0].Service == "watch-reset-api"
	}, 10*time.Second, 100*time.Millisecond)

	coord1.mu.Lock()
	coord1.serviceOwners = map[string]model.ServiceOwner{}
	coord1.ownedServices = map[string]OwnedService{}
	coord1.mu.Unlock()
	require.NoError(t, coord1.reloadServiceOwners(context.Background()))
	coord1.syncOwnedServices(context.Background())
	require.Eventually(t, func() bool {
		owned := coord1.OwnedServices()
		return len(owned) == 1 && owned[0].Service == "watch-reset-api"
	}, 10*time.Second, 100*time.Millisecond)
}

func TestCoordinatorReconcileRemovesExpiredEmptyOwner(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()
	client := mustEtcdClient(t, endpoint)
	defer client.Close()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	require.Eventually(t, func() bool { return coord1.IsLeader() }, 10*time.Second, 100*time.Millisecond)

	payload, err := json.Marshal(model.ServiceOwner{Namespace: "prod", Service: "stale", OwnerNodeID: "node-1", Epoch: 1, ExpiresAt: time.Now().Add(-time.Minute)})
	require.NoError(t, err)
	_, err = client.Put(context.Background(), keyspace.ServiceOwnerKey("prod", "stale"), string(payload))
	require.NoError(t, err)
	require.NoError(t, coord1.reloadServiceOwners(context.Background()))
	require.NoError(t, coord1.ReconcileNow(context.Background()))
	owner, ok := mustReadOwner(t, client, "prod", "stale")
	require.False(t, ok)
	require.Equal(t, model.ServiceOwner{}, owner)
}

func TestCoordinatorPrefersLowerFirstRevisionForSeed(t *testing.T) {
	endpoint, stopEtcd := startEmbeddedEtcd(t)
	defer stopEtcd()
	client := mustEtcdClient(t, endpoint)
	defer client.Close()

	coord1, cleanup1 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-1", HTTPAddr: "127.0.0.1:18080", GRPCAddr: "127.0.0.1:19090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup1() }()
	coord2, cleanup2 := mustCoordinator(t, endpoint, Config{Enabled: true, NodeID: "node-2", HTTPAddr: "127.0.0.1:28080", GRPCAddr: "127.0.0.1:29090", MemberTTL: 3 * time.Second, MemberKeepAliveInterval: 300 * time.Millisecond, LeaderTTL: 3 * time.Second, LeaderKeepAliveInterval: 300 * time.Millisecond})
	defer func() { _ = cleanup2() }()
	require.Eventually(t, func() bool { return coord1.IsLeader() != coord2.IsLeader() }, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, coord2.RecordServiceSeed(context.Background(), "prod", "billing-api", 20))
	require.NoError(t, coord1.RecordServiceSeed(context.Background(), "prod", "billing-api", 10))
	require.Eventually(t, func() bool {
		seedRecord, err := client.Get(context.Background(), keyspace.ServiceSeedKey("prod", "billing-api"))
		require.NoError(t, err)
		if len(seedRecord.Kvs) == 0 {
			return false
		}
		var seed model.ServiceSeed
		require.NoError(t, json.Unmarshal(seedRecord.Kvs[0].Value, &seed))
		return seed.IngressNodeID == "node-1" && seed.FirstRevision == 10
	}, 10*time.Second, 100*time.Millisecond)
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
