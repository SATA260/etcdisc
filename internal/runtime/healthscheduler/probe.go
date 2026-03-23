// probe.go implements owner-driven active probe scheduling with a min-heap queue and worker pool.
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
	healthsvc "etcdisc/internal/core/service/health"
	probesvc "etcdisc/internal/core/service/probe"
	registrysvc "etcdisc/internal/core/service/registry"
	"etcdisc/internal/infra/clock"
	cluster "etcdisc/internal/runtime/cluster"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	runtimeDeleteTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "etcdisc_runtime_delete_total",
		Help: "Count of runtime deletes triggered by heartbeat or probe state machines.",
	})
	runtimeOwnerEpochRejects = promauto.NewCounter(prometheus.CounterOpts{
		Name: "etcdisc_runtime_owner_epoch_reject_total",
		Help: "Count of runtime writes rejected due to owner epoch mismatch.",
	})
	runtimeRevisionConflicts = promauto.NewCounter(prometheus.CounterOpts{
		Name: "etcdisc_runtime_revision_conflict_total",
		Help: "Count of runtime writes rejected due to instance revision change.",
	})
	runtimeRebuildTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "etcdisc_runtime_rebuild_total",
		Help: "Count of runtime task rebuild operations from etcd facts.",
	})
	runtimeReconcileTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "etcdisc_runtime_reconcile_total",
		Help: "Count of runtime reconciliation loops executed by workers.",
	})
)

type probeEntry struct {
	ServiceKey  string
	Namespace   string
	Service     string
	InstanceID  string
	Revision    int64
	OwnerEpoch  int64
	Status      model.InstanceStatus
	LastProbeAt time.Time
	ProbeMode   model.HealthCheckMode
	ProbeConfig model.ProbeConfig
	Token       uint64
}

type probeTask struct {
	ServiceKey string
	Namespace  string
	Service    string
	InstanceID string
	DueAt      time.Time
	Token      uint64
	Revision   int64
	OwnerEpoch int64
	index      int
}

type probeTaskHeap []*probeTask

func (h probeTaskHeap) Len() int           { return len(h) }
func (h probeTaskHeap) Less(i, j int) bool { return h[i].DueAt.Before(h[j].DueAt) }
func (h probeTaskHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *probeTaskHeap) Push(x any) {
	task := x.(*probeTask)
	task.index = len(*h)
	*h = append(*h, task)
}
func (h *probeTaskHeap) Pop() any {
	old := *h
	last := len(old) - 1
	task := old[last]
	old[last] = nil
	task.index = -1
	*h = old[:last]
	return task
}

type probeJob struct {
	Entry probeEntry
}

// ProbeRunner executes one probe for one instance.
type ProbeRunner interface {
	Probe(ctx context.Context, instance model.Instance) probesvc.Result
}

// ProbeScheduler maintains active probe scheduling for locally owned services.
type ProbeScheduler struct {
	store       port.Store
	registry    *registrysvc.Service
	coordinator OwnedServicesProvider
	probe       ProbeRunner
	clock       clock.Clock
	logger      *slog.Logger

	interval    time.Duration
	timeout     time.Duration
	concurrency int

	mu         sync.Mutex
	entries    map[string]probeEntry
	ownedState map[string]cluster.OwnedService
	tasks      probeTaskHeap
	started    bool
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	jobs       chan probeJob
	counter    atomic.Uint64
}

// NewProbeScheduler creates a probe scheduler with the agreed defaults.
func NewProbeScheduler(store port.Store, registry *registrysvc.Service, coordinator OwnedServicesProvider, probe ProbeRunner, clk clock.Clock, logger *slog.Logger) *ProbeScheduler {
	if clk == nil {
		clk = clock.RealClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if probe == nil {
		probe = probesvc.NewService()
	}
	return &ProbeScheduler{store: store, registry: registry, coordinator: coordinator, probe: probe, clock: clk, logger: logger, interval: 10 * time.Second, timeout: 2 * time.Second, concurrency: 100, entries: map[string]probeEntry{}, ownedState: map[string]cluster.OwnedService{}, jobs: make(chan probeJob, 256)}
}

// Start launches sync, schedule, worker, and reconciliation loops.
func (s *ProbeScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.started = true
	heap.Init(&s.tasks)
	s.wg.Add(3 + s.concurrency)
	go s.syncLoop()
	go s.scheduleLoop()
	go s.reconcileLoop()
	for range s.concurrency {
		go s.workerLoop()
	}
}

// Close stops background loops.
func (s *ProbeScheduler) Close() error {
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

func (s *ProbeScheduler) syncLoop() {
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

func (s *ProbeScheduler) scheduleLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		task, waitFor := s.nextTask()
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
		s.dispatchTask(task)
	}
}

func (s *ProbeScheduler) workerLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case job := <-s.jobs:
			s.runProbeJob(job)
		}
	}
}

func (s *ProbeScheduler) reconcileLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			runtimeReconcileTotal.Inc()
			s.syncOwnedServices(s.ctx)
		}
	}
}

func (s *ProbeScheduler) nextTask() (*probeTask, time.Duration) {
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

func (s *ProbeScheduler) dispatchTask(task *probeTask) {
	entryKey := task.ServiceKey + "/" + task.InstanceID
	s.mu.Lock()
	entry, ok := s.entries[entryKey]
	s.mu.Unlock()
	if !ok || entry.Token != task.Token || entry.OwnerEpoch != task.OwnerEpoch {
		return
	}
	select {
	case s.jobs <- probeJob{Entry: entry}:
	case <-s.ctx.Done():
	}
}

func (s *ProbeScheduler) runProbeJob(job probeJob) {
	baseCtx := s.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, s.timeout)
	defer cancel()
	result := s.probe.Probe(ctx, model.Instance{Namespace: job.Entry.Namespace, Service: job.Entry.Service, InstanceID: job.Entry.InstanceID, HealthCheckMode: job.Entry.ProbeMode, ProbeConfig: job.Entry.ProbeConfig})
	instance, deleted, err := s.registry.ApplyProbeResult(baseCtx, registrysvc.ProbeResultInput{Namespace: job.Entry.Namespace, Service: job.Entry.Service, InstanceID: job.Entry.InstanceID, Success: result.Success, PolicyOverride: &healthsvc.Policy{FailureThreshold: 3, SuccessThreshold: 2, DeleteThreshold: 5}, ExpectedRevision: job.Entry.Revision, ExpectedOwnerEpoch: job.Entry.OwnerEpoch})
	if err != nil {
		if apperrors.IsCode(err, apperrors.CodeConflict) {
			runtimeRevisionConflicts.Inc()
			return
		}
		if apperrors.IsCode(err, apperrors.CodeNotFound) {
			return
		}
		if apperrors.MessageOf(err) == "service owner epoch changed" {
			runtimeOwnerEpochRejects.Inc()
			return
		}
		s.logger.Warn("probe apply failed", "namespace", job.Entry.Namespace, "service", job.Entry.Service, "instanceId", job.Entry.InstanceID, "err", err)
		return
	}
	entryKey := job.Entry.ServiceKey + "/" + job.Entry.InstanceID
	if deleted {
		runtimeDeleteTotal.Inc()
		s.mu.Lock()
		delete(s.entries, entryKey)
		s.mu.Unlock()
		s.logger.Info("probe deleted instance", "namespace", job.Entry.Namespace, "service", job.Entry.Service, "instanceId", job.Entry.InstanceID)
		return
	}
	s.mu.Lock()
	entry := s.entries[entryKey]
	entry.Revision = instance.Revision
	entry.Status = instance.Status
	entry.LastProbeAt = s.clock.Now()
	entry.Token++
	s.entries[entryKey] = entry
	heap.Push(&s.tasks, &probeTask{ServiceKey: entry.ServiceKey, Namespace: entry.Namespace, Service: entry.Service, InstanceID: entry.InstanceID, DueAt: s.clock.Now().Add(s.interval), Token: entry.Token, Revision: entry.Revision, OwnerEpoch: entry.OwnerEpoch})
	s.mu.Unlock()
	s.logger.Info("probe applied", "namespace", job.Entry.Namespace, "service", job.Entry.Service, "instanceId", job.Entry.InstanceID, "success", result.Success)
}

func (s *ProbeScheduler) syncOwnedServices(ctx context.Context) {
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
			entry, ok := decodeProbeEntry(record, ownedService)
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
		s.pruneMissingEntries(ctx, ownedService.Namespace+"/"+ownedService.Service, ownedService.Namespace, ownedService.Service)
	}
}

func (s *ProbeScheduler) upsertEntry(entry probeEntry) {
	entryKey := entry.ServiceKey + "/" + entry.InstanceID
	s.mu.Lock()
	current, exists := s.entries[entryKey]
	if exists && current.Revision == entry.Revision && current.OwnerEpoch == entry.OwnerEpoch && current.Status == entry.Status && current.ProbeMode == entry.ProbeMode && current.ProbeConfig == entry.ProbeConfig {
		s.mu.Unlock()
		return
	}
	entry.Token = current.Token + 1
	s.entries[entryKey] = entry
	heap.Push(&s.tasks, &probeTask{ServiceKey: entry.ServiceKey, Namespace: entry.Namespace, Service: entry.Service, InstanceID: entry.InstanceID, DueAt: s.clock.Now().Add(s.interval), Token: entry.Token, Revision: entry.Revision, OwnerEpoch: entry.OwnerEpoch})
	s.mu.Unlock()
	if !exists {
		runtimeRebuildTotal.Inc()
	}
}

func (s *ProbeScheduler) pruneMissingEntries(ctx context.Context, serviceKey, namespaceName, serviceName string) {
	records, err := s.store.List(ctx, keyspace.InstanceServicePrefix(namespaceName, serviceName))
	if err != nil {
		return
	}
	active := map[string]struct{}{}
	for _, record := range records {
		var instance model.Instance
		if json.Unmarshal(record.Value, &instance) == nil && instance.HealthCheckMode.IsProbe() {
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

func decodeProbeEntry(record port.Record, owned cluster.OwnedService) (probeEntry, bool) {
	var instance model.Instance
	if err := json.Unmarshal(record.Value, &instance); err != nil {
		return probeEntry{}, false
	}
	if !instance.HealthCheckMode.IsProbe() {
		return probeEntry{}, false
	}
	return probeEntry{ServiceKey: owned.Namespace + "/" + owned.Service, Namespace: owned.Namespace, Service: owned.Service, InstanceID: instance.InstanceID, Revision: record.Revision, OwnerEpoch: owned.OwnerEpoch, Status: instance.Status, LastProbeAt: instance.LastProbeAt, ProbeMode: instance.HealthCheckMode, ProbeConfig: instance.ProbeConfig}, true
}
