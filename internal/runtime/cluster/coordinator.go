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

// Coordinator maintains worker membership and assignment leader election.
type Coordinator struct {
	store  port.Store
	logger *slog.Logger
	clock  clock.Clock
	config Config

	startedAt time.Time

	mu             sync.RWMutex
	members        map[string]model.WorkerMember
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
		electionSignal: make(chan struct{}, 1),
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
		c.wg.Add(5)
		go c.memberKeepAliveLoop()
		go c.leaderKeepAliveLoop()
		go c.memberWatchLoop()
		go c.leaderWatchLoop()
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
			break
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
}

func (c *Coordinator) signalElection() {
	select {
	case c.electionSignal <- struct{}{}:
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

func memberIDFromKey(key string) string {
	return strings.TrimPrefix(key, keyspace.WorkerMemberPrefix())
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
