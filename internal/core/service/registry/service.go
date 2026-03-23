// service.go implements instance registration, updates, heartbeats, deregistration, and probe result handling.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/keyspace"
	"etcdisc/internal/core/model"
	"etcdisc/internal/core/port"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	"etcdisc/internal/infra/clock"
)

// Service manages runtime service instances.
type Service struct {
	store      port.Store
	namespaces *namespacesvc.Service
	health     *healthsvc.Manager
	clock      clock.Clock
	idCounter  atomic.Uint64
	ingress    ServiceIngressRecorder
}

// ServiceIngressRecorder captures the first ingress node for service ownership assignment.
type ServiceIngressRecorder interface {
	RecordServiceSeed(ctx context.Context, namespaceName, serviceName string) error
}

// RegisterInput captures provider registration inputs.
type RegisterInput struct {
	Instance        model.Instance `json:"instance"`
	LeaseTTLSeconds int64          `json:"leaseTTLSeconds"`
}

// UpdateInput captures the allowed mutable fields for an instance update.
type UpdateInput struct {
	Namespace        string            `json:"namespace"`
	Service          string            `json:"service"`
	InstanceID       string            `json:"instanceId"`
	Weight           *int              `json:"weight,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Version          *string           `json:"version,omitempty"`
	ExpectedRevision int64             `json:"expectedRevision"`
}

// HeartbeatInput identifies a heartbeat target.
type HeartbeatInput struct {
	Namespace  string `json:"namespace"`
	Service    string `json:"service"`
	InstanceID string `json:"instanceId"`
}

// DeregisterInput identifies an instance to remove.
type DeregisterInput struct {
	Namespace  string `json:"namespace"`
	Service    string `json:"service"`
	InstanceID string `json:"instanceId"`
}

// ProbeResultInput contains one probe outcome for one instance.
type ProbeResultInput struct {
	Namespace          string            `json:"namespace"`
	Service            string            `json:"service"`
	InstanceID         string            `json:"instanceId"`
	Success            bool              `json:"success"`
	PolicyOverride     *healthsvc.Policy `json:"policyOverride,omitempty"`
	ExpectedRevision   int64             `json:"expectedRevision,omitempty"`
	ExpectedOwnerEpoch int64             `json:"expectedOwnerEpoch,omitempty"`
}

// HeartbeatPolicy defines timeout windows for heartbeat driven state transitions.
type HeartbeatPolicy struct {
	UnhealthyAfter time.Duration
	DeleteAfter    time.Duration
}

// DefaultHeartbeatPolicy returns the default heartbeat timeout windows.
func DefaultHeartbeatPolicy() HeartbeatPolicy {
	return HeartbeatPolicy{UnhealthyAfter: 15 * time.Second, DeleteAfter: 45 * time.Second}
}

// HeartbeatTimeoutInput identifies one heartbeat timeout evaluation target.
type HeartbeatTimeoutInput struct {
	Namespace          string          `json:"namespace"`
	Service            string          `json:"service"`
	InstanceID         string          `json:"instanceId"`
	ExpectedRevision   int64           `json:"expectedRevision"`
	ExpectedOwnerEpoch int64           `json:"expectedOwnerEpoch"`
	PolicyOverride     HeartbeatPolicy `json:"policyOverride,omitempty"`
}

// ListFilter selects instances for discovery and management views.
type ListFilter struct {
	Namespace   string
	Service     string
	Group       string
	Version     string
	Metadata    map[string]string
	HealthyOnly bool
	Admin       bool
}

// NewService creates a registry service.
func NewService(store port.Store, namespaces *namespacesvc.Service, health *healthsvc.Manager, clk clock.Clock) *Service {
	if health == nil {
		health = healthsvc.NewManager()
	}
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &Service{store: store, namespaces: namespaces, health: health, clock: clk}
}

// SetIngressRecorder installs the optional service seed recorder used by clustered ownership.
func (s *Service) SetIngressRecorder(recorder ServiceIngressRecorder) {
	s.ingress = recorder
}

// Register validates and stores a new runtime instance.
func (s *Service) Register(ctx context.Context, input RegisterInput) (model.Instance, error) {
	instance := input.Instance
	instance.Normalize()
	if instance.InstanceID == "" {
		instance.InstanceID = s.newInstanceID(instance.Service)
	}
	if err := instance.Validate(); err != nil {
		return model.Instance{}, err
	}
	if err := s.namespaces.CheckWrite(ctx, instance.Namespace, false); err != nil {
		return model.Instance{}, err
	}
	now := s.clock.Now()
	instance.CreatedAt = now
	instance.UpdatedAt = now
	instance.StatusUpdatedAt = now
	if instance.HealthCheckMode == model.HealthCheckHeartbeat {
		ttl := input.LeaseTTLSeconds
		if ttl <= 0 {
			ttl = 15
		}
		leaseID, err := s.store.GrantLease(ctx, ttl)
		if err != nil {
			return model.Instance{}, err
		}
		instance.LeaseID = int64(leaseID)
		instance.LastHeartbeatAt = now
	}
	payload, err := json.Marshal(instance)
	if err != nil {
		return model.Instance{}, apperrors.Wrap(apperrors.CodeInternal, "marshal instance failed", err)
	}
	revision, err := s.store.Create(ctx, keyspace.InstanceKey(instance.Namespace, instance.Service, instance.InstanceID), payload, port.LeaseID(instance.LeaseID))
	if err != nil {
		return model.Instance{}, err
	}
	instance.Revision = revision
	if s.ingress != nil {
		if err := s.ingress.RecordServiceSeed(ctx, instance.Namespace, instance.Service); err != nil {
			return model.Instance{}, err
		}
	}
	return instance, nil
}

// Get returns one instance.
func (s *Service) Get(ctx context.Context, namespaceName, serviceName, instanceID string) (model.Instance, error) {
	record, err := s.store.Get(ctx, keyspace.InstanceKey(namespaceName, serviceName, instanceID))
	if err != nil {
		return model.Instance{}, err
	}
	return decodeInstance(record)
}

// Update changes only the allowed mutable fields using CAS.
func (s *Service) Update(ctx context.Context, input UpdateInput) (model.Instance, error) {
	if input.ExpectedRevision <= 0 {
		return model.Instance{}, apperrors.New(apperrors.CodeInvalidArgument, "expectedRevision must be greater than zero")
	}
	if err := s.namespaces.CheckWrite(ctx, input.Namespace, false); err != nil {
		return model.Instance{}, err
	}
	current, err := s.Get(ctx, input.Namespace, input.Service, input.InstanceID)
	if err != nil {
		return model.Instance{}, err
	}
	if input.Weight != nil {
		current.Weight = *input.Weight
	}
	if input.Metadata != nil {
		current.Metadata = model.CopyStringMap(input.Metadata)
	}
	if input.Version != nil {
		current.Version = *input.Version
	}
	current.UpdatedAt = s.clock.Now()
	current.Normalize()
	if err := current.Validate(); err != nil {
		return model.Instance{}, err
	}
	payload, err := json.Marshal(current)
	if err != nil {
		return model.Instance{}, apperrors.Wrap(apperrors.CodeInternal, "marshal instance failed", err)
	}
	revision, err := s.store.CAS(ctx, keyspace.InstanceKey(input.Namespace, input.Service, input.InstanceID), input.ExpectedRevision, payload, port.LeaseID(current.LeaseID))
	if err != nil {
		return model.Instance{}, err
	}
	current.Revision = revision
	return current, nil
}

// Heartbeat refreshes the lease and last heartbeat timestamp for a heartbeat-mode instance.
func (s *Service) Heartbeat(ctx context.Context, input HeartbeatInput) (model.Instance, error) {
	if err := s.namespaces.CheckWrite(ctx, input.Namespace, false); err != nil {
		return model.Instance{}, err
	}
	instance, err := s.Get(ctx, input.Namespace, input.Service, input.InstanceID)
	if err != nil {
		return model.Instance{}, err
	}
	if instance.HealthCheckMode != model.HealthCheckHeartbeat {
		return model.Instance{}, apperrors.New(apperrors.CodeFailedPrecondition, "heartbeat is only valid for heartbeat mode instances")
	}
	if instance.LeaseID != 0 {
		if err := s.store.KeepAliveOnce(ctx, port.LeaseID(instance.LeaseID)); err != nil {
			return model.Instance{}, err
		}
	}
	now := s.clock.Now()
	instance.LastHeartbeatAt = now
	instance.UpdatedAt = now
	instance.Status = model.InstanceStatusHealth
	instance.StatusUpdatedAt = now
	payload, err := json.Marshal(instance)
	if err != nil {
		return model.Instance{}, apperrors.Wrap(apperrors.CodeInternal, "marshal instance failed", err)
	}
	revision, err := s.store.Put(ctx, keyspace.InstanceKey(input.Namespace, input.Service, input.InstanceID), payload, port.LeaseID(instance.LeaseID))
	if err != nil {
		return model.Instance{}, err
	}
	instance.Revision = revision
	return instance, nil
}

// ApplyHeartbeatTimeout updates heartbeat-mode instance state according to timeout windows using owner fencing and CAS.
func (s *Service) ApplyHeartbeatTimeout(ctx context.Context, input HeartbeatTimeoutInput) (model.Instance, bool, error) {
	instance, err := s.Get(ctx, input.Namespace, input.Service, input.InstanceID)
	if err != nil {
		return model.Instance{}, false, err
	}
	if instance.HealthCheckMode != model.HealthCheckHeartbeat {
		return model.Instance{}, false, apperrors.New(apperrors.CodeFailedPrecondition, "heartbeat timeout is only valid for heartbeat mode instances")
	}
	if input.ExpectedOwnerEpoch <= 0 {
		return model.Instance{}, false, apperrors.New(apperrors.CodeInvalidArgument, "expectedOwnerEpoch must be greater than zero")
	}
	if err := s.ensureOwnerEpoch(ctx, input.Namespace, input.Service, input.ExpectedOwnerEpoch); err != nil {
		return model.Instance{}, false, err
	}
	if input.ExpectedRevision > 0 && instance.Revision != input.ExpectedRevision {
		return model.Instance{}, false, apperrors.New(apperrors.CodeConflict, "instance revision changed before heartbeat timeout apply")
	}
	policy := input.PolicyOverride
	if policy.UnhealthyAfter <= 0 || policy.DeleteAfter <= 0 {
		policy = DefaultHeartbeatPolicy()
	}
	now := s.clock.Now()
	since := now.Sub(instance.LastHeartbeatAt)
	if since < policy.UnhealthyAfter {
		return instance, false, nil
	}
	if since >= policy.DeleteAfter {
		_, err := s.store.DeleteCAS(ctx, keyspace.InstanceKey(input.Namespace, input.Service, input.InstanceID), instance.Revision)
		if err != nil {
			return model.Instance{}, false, err
		}
		return instance, true, nil
	}
	if instance.Status == model.InstanceStatusUnhealth {
		return instance, false, nil
	}
	instance.Status = model.InstanceStatusUnhealth
	instance.StatusUpdatedAt = now
	instance.UpdatedAt = now
	payload, err := json.Marshal(instance)
	if err != nil {
		return model.Instance{}, false, apperrors.Wrap(apperrors.CodeInternal, "marshal instance failed", err)
	}
	revision, err := s.store.CAS(ctx, keyspace.InstanceKey(input.Namespace, input.Service, input.InstanceID), instance.Revision, payload, port.LeaseID(instance.LeaseID))
	if err != nil {
		return model.Instance{}, false, err
	}
	instance.Revision = revision
	return instance, false, nil
}

// Deregister removes a runtime instance and revokes its lease when present.
func (s *Service) Deregister(ctx context.Context, input DeregisterInput) error {
	if err := s.namespaces.CheckWrite(ctx, input.Namespace, false); err != nil {
		return err
	}
	instance, err := s.Get(ctx, input.Namespace, input.Service, input.InstanceID)
	if err != nil {
		return err
	}
	if instance.LeaseID != 0 {
		if err := s.store.RevokeLease(ctx, port.LeaseID(instance.LeaseID)); err != nil {
			return err
		}
	}
	_, err = s.store.Delete(ctx, keyspace.InstanceKey(input.Namespace, input.Service, input.InstanceID))
	return err
}

// ApplyProbeResult updates the instance health state based on one probe outcome.
func (s *Service) ApplyProbeResult(ctx context.Context, input ProbeResultInput) (model.Instance, bool, error) {
	instance, err := s.Get(ctx, input.Namespace, input.Service, input.InstanceID)
	if err != nil {
		return model.Instance{}, false, err
	}
	if !instance.HealthCheckMode.IsProbe() {
		return model.Instance{}, false, apperrors.New(apperrors.CodeFailedPrecondition, "probe result is only valid for probe mode instances")
	}
	if input.ExpectedOwnerEpoch > 0 {
		if err := s.ensureOwnerEpoch(ctx, input.Namespace, input.Service, input.ExpectedOwnerEpoch); err != nil {
			return model.Instance{}, false, err
		}
	}
	if input.ExpectedRevision > 0 && instance.Revision != input.ExpectedRevision {
		return model.Instance{}, false, apperrors.New(apperrors.CodeConflict, "instance revision changed before probe apply")
	}
	policy := healthsvc.DefaultPolicy()
	if input.PolicyOverride != nil {
		policy = *input.PolicyOverride
	}
	instance, shouldDelete := s.health.ApplyProbeResult(instance, input.Success, s.clock.Now(), policy)
	if shouldDelete {
		_, err := s.store.DeleteCAS(ctx, keyspace.InstanceKey(input.Namespace, input.Service, input.InstanceID), instance.Revision)
		return instance, true, err
	}
	payload, err := json.Marshal(instance)
	if err != nil {
		return model.Instance{}, false, apperrors.Wrap(apperrors.CodeInternal, "marshal instance failed", err)
	}
	revision, err := s.store.CAS(ctx, keyspace.InstanceKey(input.Namespace, input.Service, input.InstanceID), instance.Revision, payload, 0)
	if err != nil {
		return model.Instance{}, false, err
	}
	instance.Revision = revision
	return instance, false, nil
}

// List returns instances matching the filter.
func (s *Service) List(ctx context.Context, filter ListFilter) ([]model.Instance, error) {
	if err := s.namespaces.CheckRead(ctx, filter.Namespace, filter.Admin); err != nil {
		return nil, err
	}
	prefix := keyspace.InstanceNamespacePrefix(filter.Namespace)
	if filter.Service != "" {
		prefix = keyspace.InstanceServicePrefix(filter.Namespace, filter.Service)
	}
	records, err := s.store.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	instances := make([]model.Instance, 0, len(records))
	for _, record := range records {
		instance, err := decodeInstance(record)
		if err != nil {
			return nil, err
		}
		if filter.HealthyOnly && instance.Status != model.InstanceStatusHealth {
			continue
		}
		if filter.Group != "" && instance.Group != filter.Group {
			continue
		}
		if filter.Version != "" && instance.Version != filter.Version {
			continue
		}
		if !matchesMetadata(instance.Metadata, filter.Metadata) {
			continue
		}
		instances = append(instances, instance)
	}
	sort.Slice(instances, func(i, j int) bool {
		left := strings.Join([]string{instances[i].Namespace, instances[i].Service, instances[i].InstanceID}, "/")
		right := strings.Join([]string{instances[j].Namespace, instances[j].Service, instances[j].InstanceID}, "/")
		return left < right
	})
	return instances, nil
}

func decodeInstance(record port.Record) (model.Instance, error) {
	var instance model.Instance
	if err := json.Unmarshal(record.Value, &instance); err != nil {
		return model.Instance{}, apperrors.Wrap(apperrors.CodeInternal, "unmarshal instance failed", err)
	}
	instance.Revision = record.Revision
	instance.LeaseID = int64(record.LeaseID)
	return instance, nil
}

func matchesMetadata(instanceMetadata, expected map[string]string) bool {
	for key, value := range expected {
		if instanceMetadata[key] != value {
			return false
		}
	}
	return true
}

func (s *Service) ensureOwnerEpoch(ctx context.Context, namespaceName, serviceName string, expectedEpoch int64) error {
	record, err := s.store.Get(ctx, keyspace.ServiceOwnerKey(namespaceName, serviceName))
	if err != nil {
		return err
	}
	var owner model.ServiceOwner
	if err := json.Unmarshal(record.Value, &owner); err != nil {
		return apperrors.Wrap(apperrors.CodeInternal, "unmarshal service owner failed", err)
	}
	if owner.Epoch != expectedEpoch {
		return apperrors.New(apperrors.CodeConflict, "service owner epoch changed")
	}
	return nil
}

func (s *Service) newInstanceID(serviceName string) string {
	next := s.idCounter.Add(1)
	return fmt.Sprintf("%s-%d", serviceName, next)
}
