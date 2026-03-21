// manager_test.go verifies probe driven health transitions and delete semantics.
package health

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"etcdisc/internal/core/model"
)

func TestApplyProbeResult(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	require.Equal(t, Policy{FailureThreshold: 3, SuccessThreshold: 2, DeleteThreshold: 5}, DefaultPolicy())
	policy := Policy{FailureThreshold: 2, SuccessThreshold: 2, DeleteThreshold: 4}
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	instance := model.Instance{Status: model.InstanceStatusHealth}

	instance, shouldDelete := manager.ApplyProbeResult(instance, false, now, policy)
	require.False(t, shouldDelete)
	require.Equal(t, model.InstanceStatusHealth, instance.Status)

	instance, shouldDelete = manager.ApplyProbeResult(instance, false, now.Add(time.Second), policy)
	require.False(t, shouldDelete)
	require.Equal(t, model.InstanceStatusUnhealth, instance.Status)

	instance, shouldDelete = manager.ApplyProbeResult(instance, true, now.Add(2*time.Second), policy)
	require.False(t, shouldDelete)
	require.Equal(t, model.InstanceStatusUnhealth, instance.Status)

	instance, shouldDelete = manager.ApplyProbeResult(instance, true, now.Add(3*time.Second), policy)
	require.False(t, shouldDelete)
	require.Equal(t, model.InstanceStatusHealth, instance.Status)

	instance, _ = manager.ApplyProbeResult(instance, false, now.Add(4*time.Second), policy)
	instance, _ = manager.ApplyProbeResult(instance, false, now.Add(5*time.Second), policy)
	instance, _ = manager.ApplyProbeResult(instance, false, now.Add(6*time.Second), policy)
	_, shouldDelete = manager.ApplyProbeResult(instance, false, now.Add(7*time.Second), policy)
	require.True(t, shouldDelete)
}
