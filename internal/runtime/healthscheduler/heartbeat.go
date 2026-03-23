// heartbeat.go implements owner-driven heartbeat timeout supervision using a min-heap deadline queue.
package healthscheduler

import (
	"container/heap"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	"etcdisc/internal/core/port"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/internal/infra/clock"
	cluster "etcdisc/internal/runtime/cluster"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var heartbeatStateTransitions = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "etcdisc_heartbeat_state_transition_total",
	Help: "Count of heartbeat driven state transitions by target state.",
}, []string{"state"})

type heartbeatPhase string

const (
	heartbeatPhaseUnhealth heartbeatPhase = "unhealth"
	heartbeatPhaseDelete   heartbeatPhase = "delete"
)

type heartbeatEntry struct {
	ServiceKey      string
	Namespace       string
	Service         string
	InstanceID      string
	Revision        int64
	OwnerEpoch      int64
	Status          model.InstanceStatus
	LastHeartbeatAt time.Time
	LeaseID         int64
	Token           uint64
}

type heartbeatTask struct {
	ServiceKey string
	Namespace  string
	Service    string
	InstanceID string
	DueAt      time.Time
	Phase      heartbeatPhase
	Token      uint64
	Revision   int64
	OwnerEpoch int64
	index      int
}

type heartbeatTaskHeap []*heartbeatTask

func (h heartbeatTaskHeap) Len() int           { return len(h) }
func (h heartbeatTaskHeap) Less(i, j int) bool { return h[i].DueAt.Before(h[j].DueAt) }
func (h heartbeatTaskHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *heartbeatTaskHeap) Push(x any) {
	task := x.(*heartbeatTask)
	task.index = len(*h)
	*h = append(*h, task)
}
func (h *heartbeatTaskHeap) Pop() any {
	old := *h
	last := len(old) - 1
	task := old[last]
	old[last] = nil
	task.index = -1
	*h = old[:last]
	return task
}

// HeartbeatSupervisor maintains timeout tasks for owned heartbeat-mode services.
type HeartbeatSupervisor struct {
	store       port.Store
	registry    *registrysvc.Service
	coordinator OwnedServicesProvider
	clock       clock.Clock
	logger      *slog.Logger

	mu         sync.Mutex
	entries    map[string]heartbeatEntry
	ownedState map[string]cluster.OwnedService
	tasks      heartbeatTaskHeap
	counter    atomic.Uint64
	started    bool
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// OwnedServicesProvider exposes the current locally owned services used by runtime schedulers.
type OwnedServicesProvider interface {
	OwnedServices() []cluster.OwnedService
}

// NewHeartbeatSupervisor creates a heartbeat timeout supervisor.
func NewHeartbeatSupervisor(store port.Store, registry *registrysvc.Service, coordinator OwnedServicesProvider, clk clock.Clock, logger *slog.Logger) *HeartbeatSupervisor {
	if clk == nil {
		clk = clock.RealClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &HeartbeatSupervisor{store: store, registry: registry, coordinator: coordinator, clock: clk, logger: logger, entries: map[string]heartbeatEntry{}, ownedState: map[string]cluster.OwnedService{}}
}

// Start launches background sync and timeout loops.
func (s *HeartbeatSupervisor) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.started = true
	heap.Init(&s.tasks)
	s.wg.Add(2)
	go s.syncLoop()
	go s.runLoop()
}

// Close stops background loops.
func (s *HeartbeatSupervisor) Close() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	return nil
}

func (s *HeartbeatSupervisor) syncLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.syncOwnedServices(s.ctx)
		}
	}
}

func (s *HeartbeatSupervisor) runLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		task, waitFor := s.nextDueTask()
		if task == nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if waitFor > 0 {
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(waitFor):
			}
		}
		s.handleTask(s.ctx, task)
	}
}

func (s *HeartbeatSupervisor) nextDueTask() (*heartbeatTask, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tasks) == 0 {
		return nil, 0
	}
	task := s.tasks[0]
	now := s.clock.Now()
	if task.DueAt.After(now) {
		return task, task.DueAt.Sub(now)
	}
	heap.Pop(&s.tasks)
	return task, 0
}

func (s *HeartbeatSupervisor) syncOwnedServices(ctx context.Context) {
	owned := s.coordinator.OwnedServices()
	current := make(map[string]cluster.OwnedService, len(owned))
	for _, service := range owned {
		current[service.Namespace+"/"+service.Service] = service
	}
	s.mu.Lock()
	for key := range s.ownedState {
		if _, ok := current[key]; !ok {
			delete(s.ownedState, key)
			for entryKey, entry := range s.entries {
				if entry.ServiceKey == key {
					delete(s.entries, entryKey)
				}
			}
		}
	}
	s.mu.Unlock()
	for _, ownedService := range owned {
		records, err := s.store.List(ctx, keyspace.InstanceServicePrefix(ownedService.Namespace, ownedService.Service))
		if err != nil {
			continue
		}
		for _, record := range records {
			entry, ok := decodeHeartbeatEntry(record, ownedService)
			if !ok {
				continue
			}
			s.upsertEntry(entry)
		}
		s.mu.Lock()
		s.ownedState[ownedService.Namespace+"/"+ownedService.Service] = ownedService
		s.mu.Unlock()
	}
	for _, ownedService := range owned {
		serviceKey := ownedService.Namespace + "/" + ownedService.Service
		s.pruneMissingEntries(ctx, serviceKey, ownedService.Namespace, ownedService.Service)
	}
}

func (s *HeartbeatSupervisor) upsertEntry(entry heartbeatEntry) {
	entryKey := entry.ServiceKey + "/" + entry.InstanceID
	s.mu.Lock()
	current, exists := s.entries[entryKey]
	if exists && current.Revision == entry.Revision && current.OwnerEpoch == entry.OwnerEpoch && current.LastHeartbeatAt.Equal(entry.LastHeartbeatAt) && current.Status == entry.Status {
		s.mu.Unlock()
		return
	}
	entry.Token = current.Token + 1
	s.entries[entryKey] = entry
	tasks := scheduleHeartbeatTasks(entry)
	for _, task := range tasks {
		heap.Push(&s.tasks, task)
	}
	s.mu.Unlock()
	if !exists {
		s.logger.Info("heartbeat entry activated", "namespace", entry.Namespace, "service", entry.Service, "instanceId", entry.InstanceID, "ownerEpoch", entry.OwnerEpoch)
	}
}

func scheduleHeartbeatTasks(entry heartbeatEntry) []*heartbeatTask {
	policy := registrysvc.DefaultHeartbeatPolicy()
	return []*heartbeatTask{
		{ServiceKey: entry.ServiceKey, Namespace: entry.Namespace, Service: entry.Service, InstanceID: entry.InstanceID, DueAt: entry.LastHeartbeatAt.Add(policy.UnhealthyAfter), Phase: heartbeatPhaseUnhealth, Token: entry.Token, Revision: entry.Revision, OwnerEpoch: entry.OwnerEpoch},
		{ServiceKey: entry.ServiceKey, Namespace: entry.Namespace, Service: entry.Service, InstanceID: entry.InstanceID, DueAt: entry.LastHeartbeatAt.Add(policy.DeleteAfter), Phase: heartbeatPhaseDelete, Token: entry.Token, Revision: entry.Revision, OwnerEpoch: entry.OwnerEpoch},
	}
}

func (s *HeartbeatSupervisor) handleTask(ctx context.Context, task *heartbeatTask) {
	entryKey := task.ServiceKey + "/" + task.InstanceID
	s.mu.Lock()
	entry, ok := s.entries[entryKey]
	s.mu.Unlock()
	if !ok || entry.Token != task.Token || entry.OwnerEpoch != task.OwnerEpoch {
		return
	}
	instance, deleted, err := s.registry.ApplyHeartbeatTimeout(ctx, registrysvc.HeartbeatTimeoutInput{Namespace: task.Namespace, Service: task.Service, InstanceID: task.InstanceID, ExpectedRevision: task.Revision, ExpectedOwnerEpoch: task.OwnerEpoch})
	if err != nil {
		if apperrors.IsCode(err, apperrors.CodeConflict) || apperrors.IsCode(err, apperrors.CodeNotFound) {
			return
		}
		s.logger.Warn("heartbeat timeout apply failed", "namespace", task.Namespace, "service", task.Service, "instanceId", task.InstanceID, "err", err)
		return
	}
	if deleted {
		heartbeatStateTransitions.WithLabelValues("delete").Inc()
		s.logger.Info("heartbeat timeout deleted instance", "namespace", task.Namespace, "service", task.Service, "instanceId", task.InstanceID)
		s.mu.Lock()
		delete(s.entries, entryKey)
		s.mu.Unlock()
		return
	}
	if task.Phase == heartbeatPhaseUnhealth && instance.Status == model.InstanceStatusUnhealth {
		heartbeatStateTransitions.WithLabelValues("unhealth").Inc()
		s.logger.Info("heartbeat timeout downgraded instance", "namespace", task.Namespace, "service", task.Service, "instanceId", task.InstanceID)
		entry.Revision = instance.Revision
		entry.Status = instance.Status
		entry.LastHeartbeatAt = instance.LastHeartbeatAt
		s.mu.Lock()
		s.entries[entryKey] = entry
		s.mu.Unlock()
	}
}

func (s *HeartbeatSupervisor) pruneMissingEntries(ctx context.Context, serviceKey, namespaceName, serviceName string) {
	records, err := s.store.List(ctx, keyspace.InstanceServicePrefix(namespaceName, serviceName))
	if err != nil {
		return
	}
	active := map[string]struct{}{}
	for _, record := range records {
		var instance model.Instance
		if json.Unmarshal(record.Value, &instance) == nil && instance.HealthCheckMode == model.HealthCheckHeartbeat {
			active[instance.InstanceID] = struct{}{}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for entryKey, entry := range s.entries {
		if entry.ServiceKey != serviceKey {
			continue
		}
		if _, ok := active[entry.InstanceID]; !ok {
			delete(s.entries, entryKey)
		}
	}
}

// ReconcileNow rebuilds local heartbeat entries for the currently owned services.
func (s *HeartbeatSupervisor) ReconcileNow(ctx context.Context) {
	runtimeReconcileTotal.Inc()
	s.syncOwnedServices(ctx)
}

func decodeHeartbeatEntry(record port.Record, owned cluster.OwnedService) (heartbeatEntry, bool) {
	var instance model.Instance
	if err := json.Unmarshal(record.Value, &instance); err != nil {
		return heartbeatEntry{}, false
	}
	if instance.HealthCheckMode != model.HealthCheckHeartbeat {
		return heartbeatEntry{}, false
	}
	return heartbeatEntry{ServiceKey: owned.Namespace + "/" + owned.Service, Namespace: owned.Namespace, Service: owned.Service, InstanceID: instance.InstanceID, Revision: record.Revision, OwnerEpoch: owned.OwnerEpoch, Status: instance.Status, LastHeartbeatAt: instance.LastHeartbeatAt, LeaseID: int64(record.LeaseID)}, true
}
