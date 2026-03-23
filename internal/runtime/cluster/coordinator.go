// coordinator.go registers worker members, elects the assignment leader, and maintains local cluster runtime state.
package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	"etcdisc/internal/core/port"
	"etcdisc/internal/infra/clock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	assignmentLeaderActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "etcdisc_assignment_leader_is_active",
		Help: "Whether the local node currently holds the assignment leader role.",
	})
	clusterMembersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "etcdisc_cluster_members_total",
		Help: "Count of cluster members visible to the local node.",
	})
	assignmentLeaderElections = promauto.NewCounter(prometheus.CounterOpts{
		Name: "etcdisc_assignment_leader_elections_total",
		Help: "Count of successful assignment leader elections on the local node.",
	})
	clusterMemberEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "etcdisc_cluster_member_events_total",
		Help: "Count of observed cluster member join and leave events.",
	}, []string{"type"})
	serviceOwnerReassignments = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "etcdisc_service_owner_reassign_total",
		Help: "Count of service owner assignment and reassignment events.",
	}, []string{"reason"})
	serviceOwnerEpochConflicts = promauto.NewCounter(prometheus.CounterOpts{
		Name: "etcdisc_service_owner_epoch_conflict_total",
		Help: "Count of service owner CAS conflicts while updating authoritative owner records.",
	})
	reconciliationRepairs = promauto.NewCounter(prometheus.CounterOpts{
		Name: "etcdisc_reconciliation_repairs_total",
		Help: "Count of ownership reconciliation repairs executed by the assignment leader.",
	})
)

// Config defines cluster runtime settings for one worker.
type Config struct {
	Enabled                 bool
	NodeID                  string
	HTTPAddr                string
	GRPCAddr                string
	MemberTTL               time.Duration
	MemberKeepAliveInterval time.Duration
	LeaderTTL               time.Duration
	LeaderKeepAliveInterval time.Duration
}

// OwnedService represents one locally active service runtime assignment on the current worker.
type OwnedService struct {
	Namespace     string
	Service       string
	OwnerEpoch    int64
	OwnerRevision int64
	InstanceCount int
	Token         uint64
	ActivatedAt   time.Time
}

// Coordinator maintains worker membership and assignment leader election.
type Coordinator struct {
	store  port.Store
	logger *slog.Logger
	clock  clock.Clock
	config Config

	startedAt time.Time

	mu             sync.RWMutex
	members        map[string]model.WorkerMember
	serviceSeeds   map[string]model.ServiceSeed
	serviceOwners  map[string]model.ServiceOwner
	ownedServices  map[string]OwnedService
	currentLeader  model.AssignmentLeader
	hasLeader      bool
	memberLeaseID  port.LeaseID
	leaderLeaseID  port.LeaseID
	isLeader       bool
	startOnce      sync.Once
	stopOnce       sync.Once
	runCtx         context.Context
	runCancel      context.CancelFunc
	wg             sync.WaitGroup
	electionSignal chan struct{}
	memberChanged  chan string
}

// NewCoordinator creates a cluster coordinator from the shared store and logger.
func NewCoordinator(store port.Store, logger *slog.Logger, clk clock.Clock, cfg Config) (*Coordinator, error) {
	if clk == nil {
		clk = clock.RealClock{}
	}
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return &Coordinator{
		store:          store,
		logger:         logger,
		clock:          clk,
		config:         normalized,
		startedAt:      clk.Now(),
		members:        map[string]model.WorkerMember{},
		serviceSeeds:   map[string]model.ServiceSeed{},
		serviceOwners:  map[string]model.ServiceOwner{},
		ownedServices:  map[string]OwnedService{},
		electionSignal: make(chan struct{}, 1),
		memberChanged:  make(chan string, 32),
	}, nil
}

// Start registers the worker member and launches watch and keepalive loops.
func (c *Coordinator) Start(ctx context.Context) error {
	var startErr error
	c.startOnce.Do(func() {
		if !c.config.Enabled {
			return
		}
		if err := c.ensureMemberRegistration(ctx); err != nil {
			startErr = err
			return
		}
		c.runCtx, c.runCancel = context.WithCancel(context.Background())
		if err := c.reloadMembers(c.runCtx); err != nil {
			startErr = err
			return
		}
		if err := c.reloadLeader(c.runCtx); err != nil {
			startErr = err
			return
		}
		c.wg.Add(9)
		go c.memberKeepAliveLoop()
		go c.leaderKeepAliveLoop()
		go c.memberWatchLoop()
		go c.leaderWatchLoop()
		go c.serviceSeedWatchLoop()
		go c.ownerWatchLoop()
		go c.assignmentLoop()
		go c.reconciliationLoop()
		go c.electionLoop()
		c.signalElection()
	})
	return startErr
}

// Close stops background loops and revokes owned leases.
func (c *Coordinator) Close() error {
	var closeErr error
	c.stopOnce.Do(func() {
		if c.runCancel != nil {
			c.runCancel()
		}
		c.wg.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c.mu.Lock()
		leaderLeaseID := c.leaderLeaseID
		memberLeaseID := c.memberLeaseID
		c.leaderLeaseID = 0
		c.memberLeaseID = 0
		c.isLeader = false
		assignmentLeaderActive.Set(0)
		c.mu.Unlock()
		if leaderLeaseID != 0 {
			if err := c.store.RevokeLease(ctx, leaderLeaseID); err != nil && !apperrors.IsCode(err, apperrors.CodeNotFound) {
				closeErr = errors.Join(closeErr, err)
			}
		}
		if memberLeaseID != 0 {
			if err := c.store.RevokeLease(ctx, memberLeaseID); err != nil && !apperrors.IsCode(err, apperrors.CodeNotFound) {
				closeErr = errors.Join(closeErr, err)
			}
		}
	})
	return closeErr
}

// IsLeader reports whether the local node currently holds the assignment leader role.
func (c *Coordinator) IsLeader() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isLeader
}

// CurrentLeader returns the latest observed assignment leader.
func (c *Coordinator) CurrentLeader() (model.AssignmentLeader, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentLeader, c.hasLeader
}

// RecordServiceSeed records the first ingress node for a service when the local node accepts its initial traffic.
func (c *Coordinator) RecordServiceSeed(ctx context.Context, namespaceName, serviceName string) error {
	if !c.config.Enabled {
		return nil
	}
	seed := model.ServiceSeed{Namespace: namespaceName, Service: serviceName, IngressNodeID: c.config.NodeID, CreatedAt: c.clock.Now()}
	payload, err := json.Marshal(seed)
	if err != nil {
		return err
	}
	revision, err := c.store.Create(ctx, keyspace.ServiceSeedKey(namespaceName, serviceName), payload, 0)
	if err != nil {
		if apperrors.IsCode(err, apperrors.CodeAlreadyExists) {
			return nil
		}
		return err
	}
	seed.Revision = revision
	c.mu.Lock()
	c.serviceSeeds[serviceKey(namespaceName, serviceName)] = seed
	c.mu.Unlock()
	if c.IsLeader() {
		c.signalMemberChange("")
	}
	return nil
}

// Members returns the current member view observed by the local node.
func (c *Coordinator) Members() []model.WorkerMember {
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := make([]model.WorkerMember, 0, len(c.members))
	for _, member := range c.members {
		items = append(items, member)
	}
	return items
}

// OwnedServices returns the current locally activated service runtime assignments.
func (c *Coordinator) OwnedServices() []OwnedService {
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := make([]OwnedService, 0, len(c.ownedServices))
	for _, owned := range c.ownedServices {
		items = append(items, owned)
	}
	return items
}

func (c *Coordinator) memberKeepAliveLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.config.MemberKeepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.runCtx.Done():
			return
		case <-ticker.C:
			c.mu.RLock()
			leaseID := c.memberLeaseID
			c.mu.RUnlock()
			if leaseID == 0 {
				if err := c.ensureMemberRegistration(c.runCtx); err != nil {
					c.logger.Warn("cluster member re-registration failed", "nodeId", c.config.NodeID, "err", err)
				}
				continue
			}
			if err := c.store.KeepAliveOnce(c.runCtx, leaseID); err != nil {
				c.logger.Warn("cluster member keepalive failed", "nodeId", c.config.NodeID, "err", err)
				if err := c.ensureMemberRegistration(c.runCtx); err != nil {
					c.logger.Warn("cluster member lease refresh failed", "nodeId", c.config.NodeID, "err", err)
				}
			}
		}
	}
}

func (c *Coordinator) leaderKeepAliveLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.config.LeaderKeepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.runCtx.Done():
			return
		case <-ticker.C:
			c.mu.RLock()
			leaseID := c.leaderLeaseID
			isLeader := c.isLeader
			c.mu.RUnlock()
			if !isLeader || leaseID == 0 {
				continue
			}
			if err := c.store.KeepAliveOnce(c.runCtx, leaseID); err != nil {
				c.logger.Warn("assignment leader keepalive failed", "nodeId", c.config.NodeID, "err", err)
				c.mu.Lock()
				c.isLeader = false
				c.leaderLeaseID = 0
				assignmentLeaderActive.Set(0)
				c.mu.Unlock()
				c.signalElection()
			}
		}
	}
}

func (c *Coordinator) memberWatchLoop() {
	defer c.wg.Done()
	for {
		if err := c.reloadMembers(c.runCtx); err != nil {
			if c.runCtx.Err() != nil {
				return
			}
			c.logger.Warn("cluster member snapshot reload failed", "err", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		watchCh := c.store.Watch(c.runCtx, keyspace.WorkerMemberPrefix(), 0)
		reset := false
		for event := range watchCh {
			if event.Err != nil {
				if c.runCtx.Err() != nil {
					return
				}
				c.logger.Warn("cluster member watch reset", "err", event.Err)
				reset = true
				break
			}
			if !strings.HasPrefix(event.Key, keyspace.WorkerMemberPrefix()) {
				continue
			}
			switch event.Type {
			case port.EventPut:
				member, err := decodeWorkerMember(port.Record{Key: event.Key, Value: event.Value, Revision: event.Revision})
				if err != nil {
					c.logger.Warn("cluster member decode failed", "key", event.Key, "err", err)
					continue
				}
				c.upsertMember(member)
			case port.EventDelete:
				c.deleteMember(memberIDFromKey(event.Key))
			}
		}
		if c.runCtx.Err() != nil {
			return
		}
		if !reset {
			return
		}
	}
}

func (c *Coordinator) serviceSeedWatchLoop() {
	defer c.wg.Done()
	for {
		if err := c.reloadServiceSeeds(c.runCtx); err != nil {
			if c.runCtx.Err() != nil {
				return
			}
			c.logger.Warn("service seed snapshot reload failed", "err", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		watchCh := c.store.Watch(c.runCtx, keyspace.ServiceSeedPrefix(), 0)
		reset := false
		for event := range watchCh {
			if event.Err != nil {
				if c.runCtx.Err() != nil {
					return
				}
				c.logger.Warn("service seed watch reset", "err", event.Err)
				reset = true
				break
			}
			if !strings.HasPrefix(event.Key, keyspace.ServiceSeedPrefix()) {
				continue
			}
			switch event.Type {
			case port.EventPut:
				seed, err := decodeServiceSeed(port.Record{Key: event.Key, Value: event.Value, Revision: event.Revision})
				if err != nil {
					c.logger.Warn("service seed decode failed", "key", event.Key, "err", err)
					continue
				}
				c.mu.Lock()
				c.serviceSeeds[serviceKey(seed.Namespace, seed.Service)] = seed
				c.mu.Unlock()
				if c.IsLeader() {
					c.signalMemberChange("")
				}
			case port.EventDelete:
				c.mu.Lock()
				delete(c.serviceSeeds, serviceKeyFromSeedKey(event.Key))
				c.mu.Unlock()
			}
		}
		if c.runCtx.Err() != nil {
			return
		}
		if !reset {
			return
		}
	}
}

func (c *Coordinator) ownerWatchLoop() {
	defer c.wg.Done()
	for {
		if err := c.reloadServiceOwners(c.runCtx); err != nil {
			if c.runCtx.Err() != nil {
				return
			}
			c.logger.Warn("service owner snapshot reload failed", "err", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		c.syncOwnedServices(c.runCtx)
		watchCh := c.store.Watch(c.runCtx, keyspace.ServiceOwnerPrefix(), 0)
		reset := false
		for event := range watchCh {
			if event.Err != nil {
				if c.runCtx.Err() != nil {
					return
				}
				c.logger.Warn("service owner watch reset", "err", event.Err)
				reset = true
				break
			}
			if !strings.HasPrefix(event.Key, keyspace.ServiceOwnerPrefix()) {
				continue
			}
			switch event.Type {
			case port.EventPut:
				owner, err := decodeServiceOwner(port.Record{Key: event.Key, Value: event.Value, Revision: event.Revision})
				if err != nil {
					c.logger.Warn("service owner decode failed", "key", event.Key, "err", err)
					continue
				}
				c.mu.Lock()
				c.serviceOwners[serviceKey(owner.Namespace, owner.Service)] = owner
				c.mu.Unlock()
				c.syncOwnedServices(c.runCtx)
			case port.EventDelete:
				c.mu.Lock()
				delete(c.serviceOwners, serviceKeyFromOwnerKey(event.Key))
				c.mu.Unlock()
				c.syncOwnedServices(c.runCtx)
			}
		}
		if c.runCtx.Err() != nil {
			return
		}
		if !reset {
			return
		}
	}
}

func (c *Coordinator) assignmentLoop() {
	defer c.wg.Done()
	for {
		select {
		case <-c.runCtx.Done():
			return
		case nodeID := <-c.memberChanged:
			if !c.IsLeader() {
				continue
			}
			if err := c.reassignAffectedServices(c.runCtx, nodeID); err != nil {
				c.logger.Warn("service owner reassignment failed", "nodeId", nodeID, "err", err)
			}
		}
	}
}

func (c *Coordinator) reconciliationLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.runCtx.Done():
			return
		case <-ticker.C:
			if !c.IsLeader() {
				continue
			}
			if err := c.reconcileOwners(c.runCtx); err != nil {
				c.logger.Warn("service owner reconciliation failed", "err", err)
			}
		}
	}
}

func (c *Coordinator) leaderWatchLoop() {
	defer c.wg.Done()
	for {
		if err := c.reloadLeader(c.runCtx); err != nil {
			if c.runCtx.Err() != nil {
				return
			}
			c.logger.Warn("cluster leader snapshot reload failed", "err", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		watchCh := c.store.Watch(c.runCtx, keyspace.AssignmentLeaderKey(), 0)
		reset := false
		for event := range watchCh {
			if event.Err != nil {
				if c.runCtx.Err() != nil {
					return
				}
				c.logger.Warn("cluster leader watch reset", "err", event.Err)
				c.signalElection()
				reset = true
				break
			}
			if event.Key != keyspace.AssignmentLeaderKey() {
				continue
			}
			switch event.Type {
			case port.EventPut:
				leader, err := decodeAssignmentLeader(port.Record{Key: event.Key, Value: event.Value, Revision: event.Revision})
				if err != nil {
					c.logger.Warn("cluster leader decode failed", "err", err)
					continue
				}
				c.setCurrentLeader(leader, true)
			case port.EventDelete:
				c.setCurrentLeader(model.AssignmentLeader{}, false)
				c.signalElection()
			}
		}
		if c.runCtx.Err() != nil {
			return
		}
		if !reset {
			return
		}
	}
}

func (c *Coordinator) electionLoop() {
	defer c.wg.Done()
	for {
		select {
		case <-c.runCtx.Done():
			return
		case <-c.electionSignal:
		}
		for {
			if c.runCtx.Err() != nil {
				return
			}
			c.mu.RLock()
			hasLeader := c.hasLeader
			currentLeader := c.currentLeader
			localLeader := c.isLeader
			c.mu.RUnlock()
			if localLeader {
				break
			}
			if hasLeader && currentLeader.NodeID != c.config.NodeID {
				break
			}
			acquired, err := c.tryAcquireLeadership(c.runCtx)
			if err != nil {
				c.logger.Warn("assignment leader election attempt failed", "nodeId", c.config.NodeID, "err", err)
				time.Sleep(200 * time.Millisecond)
				continue
			}
			if acquired {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (c *Coordinator) tryAcquireLeadership(ctx context.Context) (bool, error) {
	now := c.clock.Now()
	leaseID, err := c.store.GrantLease(ctx, int64(c.config.LeaderTTL/time.Second))
	if err != nil {
		return false, err
	}
	leader := model.AssignmentLeader{NodeID: c.config.NodeID, Epoch: now.UnixNano(), ElectedAt: now}
	payload, err := json.Marshal(leader)
	if err != nil {
		_ = c.store.RevokeLease(ctx, leaseID)
		return false, err
	}
	revision, err := c.store.Create(ctx, keyspace.AssignmentLeaderKey(), payload, leaseID)
	if err != nil {
		_ = c.store.RevokeLease(ctx, leaseID)
		if apperrors.IsCode(err, apperrors.CodeAlreadyExists) {
			_ = c.reloadLeader(ctx)
			return false, nil
		}
		return false, err
	}
	leader.Revision = revision
	c.mu.Lock()
	c.leaderLeaseID = leaseID
	c.isLeader = true
	c.currentLeader = leader
	c.hasLeader = true
	c.mu.Unlock()
	assignmentLeaderActive.Set(1)
	assignmentLeaderElections.Inc()
	c.logger.Info("assignment leader elected", "nodeId", c.config.NodeID, "epoch", leader.Epoch)
	c.signalMemberChange("")
	return true, nil
}

func (c *Coordinator) ensureMemberRegistration(ctx context.Context) error {
	leaseID, err := c.store.GrantLease(ctx, int64(c.config.MemberTTL/time.Second))
	if err != nil {
		return err
	}
	member := model.WorkerMember{NodeID: c.config.NodeID, HTTPAddr: c.config.HTTPAddr, GRPCAddr: c.config.GRPCAddr, StartedAt: c.startedAt}
	payload, err := json.Marshal(member)
	if err != nil {
		_ = c.store.RevokeLease(ctx, leaseID)
		return err
	}
	revision, err := c.store.Put(ctx, keyspace.WorkerMemberKey(c.config.NodeID), payload, leaseID)
	if err != nil {
		_ = c.store.RevokeLease(ctx, leaseID)
		return err
	}
	member.Revision = revision
	c.mu.Lock()
	oldLeaseID := c.memberLeaseID
	c.memberLeaseID = leaseID
	c.members[c.config.NodeID] = member
	clusterMembersTotal.Set(float64(len(c.members)))
	c.mu.Unlock()
	if oldLeaseID != 0 && oldLeaseID != leaseID {
		_ = c.store.RevokeLease(ctx, oldLeaseID)
	}
	return nil
}

func (c *Coordinator) reloadMembers(ctx context.Context) error {
	records, err := c.store.List(ctx, keyspace.WorkerMemberPrefix())
	if err != nil {
		return err
	}
	items := make(map[string]model.WorkerMember, len(records))
	for _, record := range records {
		member, err := decodeWorkerMember(record)
		if err != nil {
			return err
		}
		items[member.NodeID] = member
	}
	c.mu.Lock()
	c.members = items
	clusterMembersTotal.Set(float64(len(items)))
	c.mu.Unlock()
	return nil
}

func (c *Coordinator) reloadLeader(ctx context.Context) error {
	record, err := c.store.Get(ctx, keyspace.AssignmentLeaderKey())
	if err != nil {
		if apperrors.IsCode(err, apperrors.CodeNotFound) {
			c.setCurrentLeader(model.AssignmentLeader{}, false)
			c.signalElection()
			return nil
		}
		return err
	}
	leader, err := decodeAssignmentLeader(record)
	if err != nil {
		return err
	}
	c.setCurrentLeader(leader, true)
	return nil
}

func (c *Coordinator) setCurrentLeader(leader model.AssignmentLeader, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentLeader = leader
	c.hasLeader = ok
	if !ok || leader.NodeID != c.config.NodeID {
		c.isLeader = false
		c.leaderLeaseID = 0
		assignmentLeaderActive.Set(0)
	}
}

func (c *Coordinator) upsertMember(member model.WorkerMember) {
	c.mu.Lock()
	_, existed := c.members[member.NodeID]
	c.members[member.NodeID] = member
	clusterMembersTotal.Set(float64(len(c.members)))
	c.mu.Unlock()
	if !existed {
		clusterMemberEvents.WithLabelValues("join").Inc()
		c.logger.Info("cluster member joined", "nodeId", member.NodeID)
	}
	if c.IsLeader() {
		c.signalMemberChange("")
	}
}

func (c *Coordinator) deleteMember(nodeID string) {
	c.mu.Lock()
	_, existed := c.members[nodeID]
	delete(c.members, nodeID)
	clusterMembersTotal.Set(float64(len(c.members)))
	c.mu.Unlock()
	if existed {
		clusterMemberEvents.WithLabelValues("leave").Inc()
		c.logger.Info("cluster member left", "nodeId", nodeID)
	}
	if c.IsLeader() {
		c.signalMemberChange(nodeID)
	}
}

func (c *Coordinator) signalElection() {
	select {
	case c.electionSignal <- struct{}{}:
	default:
	}
}

func (c *Coordinator) signalMemberChange(nodeID string) {
	select {
	case c.memberChanged <- nodeID:
	default:
	}
}

func decodeWorkerMember(record port.Record) (model.WorkerMember, error) {
	var member model.WorkerMember
	if err := json.Unmarshal(record.Value, &member); err != nil {
		return model.WorkerMember{}, err
	}
	member.Revision = record.Revision
	return member, nil
}

func decodeAssignmentLeader(record port.Record) (model.AssignmentLeader, error) {
	var leader model.AssignmentLeader
	if err := json.Unmarshal(record.Value, &leader); err != nil {
		return model.AssignmentLeader{}, err
	}
	leader.Revision = record.Revision
	return leader, nil
}

func decodeServiceSeed(record port.Record) (model.ServiceSeed, error) {
	var seed model.ServiceSeed
	if err := json.Unmarshal(record.Value, &seed); err != nil {
		return model.ServiceSeed{}, err
	}
	seed.Revision = record.Revision
	return seed, nil
}

func decodeServiceOwner(record port.Record) (model.ServiceOwner, error) {
	var owner model.ServiceOwner
	if err := json.Unmarshal(record.Value, &owner); err != nil {
		return model.ServiceOwner{}, err
	}
	owner.Revision = record.Revision
	return owner, nil
}

func memberIDFromKey(key string) string {
	return strings.TrimPrefix(key, keyspace.WorkerMemberPrefix())
}

func serviceKey(namespaceName, serviceName string) string {
	return namespaceName + "/" + serviceName
}

func serviceKeyFromSeedKey(key string) string {
	parts := strings.Split(strings.TrimPrefix(key, keyspace.ServiceSeedPrefix()), "/")
	if len(parts) < 2 {
		return ""
	}
	return serviceKey(parts[0], parts[1])
}

func serviceKeyFromOwnerKey(key string) string {
	parts := strings.Split(strings.TrimPrefix(key, keyspace.ServiceOwnerPrefix()), "/")
	if len(parts) < 2 {
		return ""
	}
	return serviceKey(parts[0], parts[1])
}

func (c *Coordinator) reloadServiceSeeds(ctx context.Context) error {
	records, err := c.store.List(ctx, keyspace.ServiceSeedPrefix())
	if err != nil {
		if apperrors.IsCode(err, apperrors.CodeNotFound) {
			return nil
		}
		return err
	}
	items := make(map[string]model.ServiceSeed, len(records))
	for _, record := range records {
		seed, err := decodeServiceSeed(record)
		if err != nil {
			return err
		}
		items[serviceKey(seed.Namespace, seed.Service)] = seed
	}
	c.mu.Lock()
	c.serviceSeeds = items
	c.mu.Unlock()
	return nil
}

func (c *Coordinator) reloadServiceOwners(ctx context.Context) error {
	records, err := c.store.List(ctx, keyspace.ServiceOwnerPrefix())
	if err != nil {
		if apperrors.IsCode(err, apperrors.CodeNotFound) {
			return nil
		}
		return err
	}
	items := make(map[string]model.ServiceOwner, len(records))
	for _, record := range records {
		owner, err := decodeServiceOwner(record)
		if err != nil {
			return err
		}
		items[serviceKey(owner.Namespace, owner.Service)] = owner
	}
	c.mu.Lock()
	c.serviceOwners = items
	c.mu.Unlock()
	return nil
}

func (c *Coordinator) syncOwnedServices(ctx context.Context) {
	owners := c.snapshotOwners()
	for key, owner := range owners {
		if owner.OwnerNodeID != c.config.NodeID {
			c.deactivateOwnedService(key)
			continue
		}
		if err := c.activateOwnedService(ctx, owner); err != nil {
			c.logger.Warn("owned service activation failed", "namespace", owner.Namespace, "service", owner.Service, "err", err)
		}
	}
	for _, owned := range c.OwnedServices() {
		key := serviceKey(owned.Namespace, owned.Service)
		owner, ok := owners[key]
		if !ok || owner.OwnerNodeID != c.config.NodeID {
			c.deactivateOwnedService(key)
		}
	}
}

func (c *Coordinator) activateOwnedService(ctx context.Context, owner model.ServiceOwner) error {
	records, err := c.store.List(ctx, keyspace.InstanceServicePrefix(owner.Namespace, owner.Service))
	if err != nil {
		return err
	}
	key := serviceKey(owner.Namespace, owner.Service)
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.ownedServices[key]
	if ok && current.OwnerEpoch == owner.Epoch && current.OwnerRevision == owner.Revision && current.InstanceCount == len(records) {
		return nil
	}
	token := current.Token + 1
	c.ownedServices[key] = OwnedService{
		Namespace:     owner.Namespace,
		Service:       owner.Service,
		OwnerEpoch:    owner.Epoch,
		OwnerRevision: owner.Revision,
		InstanceCount: len(records),
		Token:         token,
		ActivatedAt:   c.clock.Now(),
	}
	c.logger.Info("owned service activated", "namespace", owner.Namespace, "service", owner.Service, "ownerEpoch", owner.Epoch, "instanceCount", len(records))
	return nil
}

func (c *Coordinator) deactivateOwnedService(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.ownedServices[key]
	if !ok {
		return
	}
	delete(c.ownedServices, key)
	c.logger.Info("owned service deactivated", "namespace", current.Namespace, "service", current.Service, "lastToken", current.Token)
}

func (c *Coordinator) reassignAffectedServices(ctx context.Context, departedNodeID string) error {
	seeds := c.snapshotSeeds()
	owners := c.snapshotOwners()
	alive := c.aliveMemberIDs()
	for key, seed := range seeds {
		owner, ok := owners[key]
		if !ok {
			if err := c.ensureOwnerRecord(ctx, seed, model.ServiceOwner{}, false, alive, "first_register"); err != nil {
				return err
			}
			continue
		}
		if departedNodeID != "" && owner.OwnerNodeID != departedNodeID {
			continue
		}
		if owner.ExpiresAt.IsZero() && contains(alive, owner.OwnerNodeID) {
			continue
		}
		if err := c.ensureOwnerRecord(ctx, seed, owner, true, alive, firstNonEmptyReason(departedNodeID, "member_down", "leader_rebuild")); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) reconcileOwners(ctx context.Context) error {
	alive := c.aliveMemberIDs()
	owners := c.snapshotOwners()
	for _, owner := range owners {
		active, err := c.serviceHasInstances(ctx, owner.Namespace, owner.Service)
		if err != nil {
			return err
		}
		if active {
			if !owner.ExpiresAt.IsZero() {
				updated := owner
				updated.ExpiresAt = time.Time{}
				if err := c.updateOwnerRecord(ctx, owner, updated); err != nil {
					return err
				}
				reconciliationRepairs.Inc()
			}
			if !contains(alive, owner.OwnerNodeID) {
				seed := c.snapshotSeeds()[serviceKey(owner.Namespace, owner.Service)]
				if err := c.ensureOwnerRecord(ctx, seed, owner, true, alive, "member_down"); err != nil {
					return err
				}
				reconciliationRepairs.Inc()
			}
			continue
		}
		if owner.ExpiresAt.IsZero() {
			updated := owner
			updated.ExpiresAt = c.clock.Now().Add(300 * time.Second)
			updated.Reason = "leader_rebuild"
			if err := c.updateOwnerRecord(ctx, owner, updated); err != nil {
				return err
			}
			reconciliationRepairs.Inc()
			continue
		}
		if c.clock.Now().After(owner.ExpiresAt) {
			if _, err := c.store.DeleteCAS(ctx, keyspace.ServiceOwnerKey(owner.Namespace, owner.Service), owner.Revision); err != nil && !apperrors.IsCode(err, apperrors.CodeNotFound) && !apperrors.IsCode(err, apperrors.CodeConflict) {
				return err
			}
			reconciliationRepairs.Inc()
		}
	}
	return nil
}

func (c *Coordinator) ensureOwnerRecord(ctx context.Context, seed model.ServiceSeed, current model.ServiceOwner, exists bool, aliveMemberIDs []string, reason string) error {
	ownerNodeID := seed.IngressNodeID
	if ownerNodeID == "" || !contains(aliveMemberIDs, ownerNodeID) || reason != "first_register" {
		ownerNodeID = ChooseOwner(serviceKey(seed.Namespace, seed.Service), aliveMemberIDs)
	}
	if ownerNodeID == "" {
		return nil
	}
	now := c.clock.Now()
	leader, ok := c.CurrentLeader()
	if !ok || leader.NodeID != c.config.NodeID {
		return nil
	}
	if !exists {
		owner := model.ServiceOwner{Namespace: seed.Namespace, Service: seed.Service, OwnerNodeID: ownerNodeID, Epoch: 1, AssignedByLeaderEpoch: leader.Epoch, Reason: reason, AssignedAt: now}
		payload, err := json.Marshal(owner)
		if err != nil {
			return err
		}
		revision, err := c.store.Create(ctx, keyspace.ServiceOwnerKey(seed.Namespace, seed.Service), payload, 0)
		if err != nil {
			if apperrors.IsCode(err, apperrors.CodeAlreadyExists) {
				serviceOwnerEpochConflicts.Inc()
				return nil
			}
			return err
		}
		owner.Revision = revision
		c.mu.Lock()
		c.serviceOwners[serviceKey(seed.Namespace, seed.Service)] = owner
		c.mu.Unlock()
		serviceOwnerReassignments.WithLabelValues(reason).Inc()
		c.logger.Info("service owner assigned", "namespace", seed.Namespace, "service", seed.Service, "ownerNodeId", ownerNodeID, "epoch", owner.Epoch, "reason", reason)
		return nil
	}
	if current.OwnerNodeID == ownerNodeID && current.ExpiresAt.IsZero() {
		return nil
	}
	updated := current
	updated.OwnerNodeID = ownerNodeID
	updated.Epoch = current.Epoch + 1
	updated.AssignedByLeaderEpoch = leader.Epoch
	updated.Reason = reason
	updated.AssignedAt = now
	updated.ExpiresAt = time.Time{}
	if err := c.updateOwnerRecord(ctx, current, updated); err != nil {
		if apperrors.IsCode(err, apperrors.CodeConflict) {
			serviceOwnerEpochConflicts.Inc()
			return nil
		}
		return err
	}
	serviceOwnerReassignments.WithLabelValues(reason).Inc()
	c.logger.Info("service owner reassigned", "namespace", seed.Namespace, "service", seed.Service, "ownerNodeId", ownerNodeID, "epoch", updated.Epoch, "reason", reason)
	return nil
}

func (c *Coordinator) updateOwnerRecord(ctx context.Context, current, updated model.ServiceOwner) error {
	payload, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	revision, err := c.store.CAS(ctx, keyspace.ServiceOwnerKey(current.Namespace, current.Service), current.Revision, payload, 0)
	if err != nil {
		return err
	}
	updated.Revision = revision
	c.mu.Lock()
	c.serviceOwners[serviceKey(updated.Namespace, updated.Service)] = updated
	c.mu.Unlock()
	return nil
}

func (c *Coordinator) serviceHasInstances(ctx context.Context, namespaceName, serviceName string) (bool, error) {
	records, err := c.store.List(ctx, keyspace.InstanceServicePrefix(namespaceName, serviceName))
	if err != nil {
		return false, err
	}
	return len(records) > 0, nil
}

func (c *Coordinator) aliveMemberIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := make(map[string]struct{}, len(c.members))
	for nodeID := range c.members {
		items[nodeID] = struct{}{}
	}
	return SortedMemberIDs(items)
}

func (c *Coordinator) snapshotSeeds() map[string]model.ServiceSeed {
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := make(map[string]model.ServiceSeed, len(c.serviceSeeds))
	for key, seed := range c.serviceSeeds {
		items[key] = seed
	}
	return items
}

func (c *Coordinator) snapshotOwners() map[string]model.ServiceOwner {
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := make(map[string]model.ServiceOwner, len(c.serviceOwners))
	for key, owner := range c.serviceOwners {
		items[key] = owner
	}
	return items
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func firstNonEmptyReason(trigger, preferred, fallback string) string {
	if trigger != "" {
		return preferred
	}
	return fallback
}

func normalizeConfig(cfg Config) (Config, error) {
	if !cfg.Enabled {
		return cfg, nil
	}
	if cfg.NodeID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return Config{}, err
		}
		cfg.NodeID = fmt.Sprintf("%s-%d", strings.ReplaceAll(hostname, " ", "-"), time.Now().UnixNano())
	}
	if cfg.MemberTTL <= 0 {
		cfg.MemberTTL = 30 * time.Second
	}
	if cfg.MemberKeepAliveInterval <= 0 {
		cfg.MemberKeepAliveInterval = 10 * time.Second
	}
	if cfg.LeaderTTL <= 0 {
		cfg.LeaderTTL = 30 * time.Second
	}
	if cfg.LeaderKeepAliveInterval <= 0 {
		cfg.LeaderKeepAliveInterval = 10 * time.Second
	}
	return cfg, nil
}
