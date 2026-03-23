// service_test.go verifies instance registration, updates, heartbeats, probe transitions, and deregistration.
package registry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/model"
	healthsvc "etcdisc/internal/core/service/health"
	namespacesvc "etcdisc/internal/core/service/namespace"
	"etcdisc/test/testkit"
)

func TestRegisterAppliesDefaultsAndLeaseForHeartbeat(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)

	svc := NewService(store, nsService, healthsvc.NewManager(), namespacesvc.NewFixedClock(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)))
	instance, err := svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	require.Equal(t, model.DefaultGroup, instance.Group)
	require.Equal(t, model.DefaultWeight, instance.Weight)
	require.Equal(t, model.HealthCheckHeartbeat, instance.HealthCheckMode)
	require.NotEmpty(t, instance.InstanceID)
	require.NotZero(t, instance.LeaseID)
}

func TestRegisterRejectsDuplicateInstance(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, nil, namespacesvc.NewFixedClock(time.Now()))

	input := RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}}
	_, err = svc.Register(context.Background(), input)
	require.NoError(t, err)
	_, err = svc.Register(context.Background(), input)
	require.Error(t, err)
	require.Equal(t, apperrors.CodeAlreadyExists, apperrors.CodeOf(err))
}

func TestUpdateOnlyChangesAllowedFields(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, nil, namespacesvc.NewFixedClock(time.Now()))

	registered, err := svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	weight := 200
	version := "v2"
	updated, err := svc.Update(context.Background(), UpdateInput{Namespace: registered.Namespace, Service: registered.Service, InstanceID: registered.InstanceID, Weight: &weight, Version: &version, Metadata: map[string]string{"zone": "az1"}, ExpectedRevision: registered.Revision})
	require.NoError(t, err)
	require.Equal(t, 200, updated.Weight)
	require.Equal(t, "v2", updated.Version)
	require.Equal(t, "az1", updated.Metadata["zone"])
	require.Equal(t, registered.Address, updated.Address)
}

func TestUpdateRequiresCASRevision(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, nil, namespacesvc.NewFixedClock(time.Now()))

	registered, err := svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	weight := 200
	_, err = svc.Update(context.Background(), UpdateInput{Namespace: registered.Namespace, Service: registered.Service, InstanceID: registered.InstanceID, Weight: &weight, ExpectedRevision: registered.Revision + 1})
	require.Error(t, err)
	require.Equal(t, apperrors.CodeConflict, apperrors.CodeOf(err))
	_, err = svc.Update(context.Background(), UpdateInput{Namespace: registered.Namespace, Service: registered.Service, InstanceID: registered.InstanceID, Weight: &weight})
	require.Equal(t, apperrors.CodeInvalidArgument, apperrors.CodeOf(err))
}

func TestHeartbeatRefreshesHeartbeatModeOnly(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, nil, namespacesvc.NewFixedClock(time.Now().Add(time.Minute)))

	registered, err := svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)

	heartbeat, err := svc.Heartbeat(context.Background(), HeartbeatInput{Namespace: registered.Namespace, Service: registered.Service, InstanceID: registered.InstanceID})
	require.NoError(t, err)
	require.NotZero(t, heartbeat.LastHeartbeatAt)

	probeInstance, err := svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "probe-api", InstanceID: "node-2", Address: "127.0.0.2", Port: 8081, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)
	_, err = svc.Heartbeat(context.Background(), HeartbeatInput{Namespace: probeInstance.Namespace, Service: probeInstance.Service, InstanceID: probeInstance.InstanceID})
	require.Equal(t, apperrors.CodeFailedPrecondition, apperrors.CodeOf(err))
}

func TestProbeRegistrationDoesNotUseLeaseAndSupportsDefaultProbeTarget(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, nil, namespacesvc.NewFixedClock(time.Now()))

	registered, err := svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080, HealthCheckMode: model.HealthCheckHTTPProbe}})
	require.NoError(t, err)
	require.Zero(t, registered.LeaseID)
	require.Equal(t, "/etcdisc/http/health", registered.ProbeConfig.Path)
}

func TestApplyProbeResultTransitionsAndDeletes(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, nil, namespacesvc.NewFixedClock(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)))

	registered, err := svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)

	policy := &healthsvc.Policy{FailureThreshold: 2, SuccessThreshold: 2, DeleteThreshold: 4}
	instance, deleted, err := svc.ApplyProbeResult(context.Background(), ProbeResultInput{Namespace: registered.Namespace, Service: registered.Service, InstanceID: registered.InstanceID, Success: false, PolicyOverride: policy})
	require.NoError(t, err)
	require.False(t, deleted)
	require.Equal(t, model.InstanceStatusHealth, instance.Status)

	instance, deleted, err = svc.ApplyProbeResult(context.Background(), ProbeResultInput{Namespace: registered.Namespace, Service: registered.Service, InstanceID: registered.InstanceID, Success: false, PolicyOverride: policy})
	require.NoError(t, err)
	require.False(t, deleted)
	require.Equal(t, model.InstanceStatusUnhealth, instance.Status)

	_, _, _ = svc.ApplyProbeResult(context.Background(), ProbeResultInput{Namespace: registered.Namespace, Service: registered.Service, InstanceID: registered.InstanceID, Success: false, PolicyOverride: policy})
	_, deleted, err = svc.ApplyProbeResult(context.Background(), ProbeResultInput{Namespace: registered.Namespace, Service: registered.Service, InstanceID: registered.InstanceID, Success: false, PolicyOverride: policy})
	require.NoError(t, err)
	require.True(t, deleted)

	heartbeatInstance, err := svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "heartbeat-api", InstanceID: "node-2", Address: "127.0.0.2", Port: 8081}})
	require.NoError(t, err)
	_, _, err = svc.ApplyProbeResult(context.Background(), ProbeResultInput{Namespace: heartbeatInstance.Namespace, Service: heartbeatInstance.Service, InstanceID: heartbeatInstance.InstanceID, Success: false})
	require.Equal(t, apperrors.CodeFailedPrecondition, apperrors.CodeOf(err))
}

func TestListFiltersByHealthyStateAndMetadata(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, nil, namespacesvc.NewFixedClock(time.Now()))

	_, err = svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080, Metadata: map[string]string{"zone": "a"}}})
	require.NoError(t, err)
	_, err = svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-2", Address: "127.0.0.2", Port: 8080, Status: model.InstanceStatusUnhealth, Metadata: map[string]string{"zone": "b"}, HealthCheckMode: model.HealthCheckTCPProbe}})
	require.NoError(t, err)

	items, err := svc.List(context.Background(), ListFilter{Namespace: "prod-core", Service: "payment-api", HealthyOnly: true, Metadata: map[string]string{"zone": "a"}})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "node-1", items[0].InstanceID)

	items, err = svc.List(context.Background(), ListFilter{Namespace: "prod-core", Service: "payment-api", HealthyOnly: false, Version: "", Group: model.DefaultGroup})
	require.NoError(t, err)
	require.Len(t, items, 2)
}

func TestDeregisterRemovesInstance(t *testing.T) {
	t.Parallel()

	store := testkit.NewMemoryStore()
	nsService := namespacesvc.NewService(store, namespacesvc.NewFixedClock(time.Now()))
	_, err := nsService.Create(context.Background(), namespacesvc.CreateNamespaceInput{Name: "prod-core"})
	require.NoError(t, err)
	svc := NewService(store, nsService, nil, namespacesvc.NewFixedClock(time.Now()))

	registered, err := svc.Register(context.Background(), RegisterInput{Instance: model.Instance{Namespace: "prod-core", Service: "payment-api", InstanceID: "node-1", Address: "127.0.0.1", Port: 8080}})
	require.NoError(t, err)
	require.NoError(t, svc.Deregister(context.Background(), DeregisterInput{Namespace: registered.Namespace, Service: registered.Service, InstanceID: registered.InstanceID}))
	_, err = svc.Get(context.Background(), registered.Namespace, registered.Service, registered.InstanceID)
	require.Error(t, err)
	require.Equal(t, apperrors.CodeNotFound, apperrors.CodeOf(err))
}
