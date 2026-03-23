// manager.go applies phase 1 health state transitions for heartbeat and probe driven instances.
package health

import (
	"time"

	"etcdisc/internal/core/model"
)

// Policy contains the threshold settings used by the instance health state machine.
type Policy struct {
	FailureThreshold int
	SuccessThreshold int
	DeleteThreshold  int
}

// DefaultPolicy returns the documented default threshold values used when no config overrides exist.
func DefaultPolicy() Policy {
	return Policy{FailureThreshold: 3, SuccessThreshold: 2, DeleteThreshold: 5}
}

// Manager applies health transitions to mutable instance records.
type Manager struct{}

// NewManager creates a health state manager.
func NewManager() *Manager {
	return &Manager{}
}

// ApplyProbeResult updates counters and status based on one probe result.
func (m *Manager) ApplyProbeResult(instance model.Instance, success bool, now time.Time, policy Policy) (model.Instance, bool) {
	if success {
		instance.ConsecutiveSuccesses++
		instance.ConsecutiveFailures = 0
		instance.LastProbeAt = now
		instance.UpdatedAt = now
		if instance.Status == model.InstanceStatusUnhealth && instance.ConsecutiveSuccesses >= max(policy.SuccessThreshold, 1) {
			instance.Status = model.InstanceStatusHealth
			instance.StatusUpdatedAt = now
		}
		return instance, false
	}

	instance.ConsecutiveFailures++
	instance.ConsecutiveSuccesses = 0
	instance.LastProbeAt = now
	instance.UpdatedAt = now
	if instance.Status == model.InstanceStatusHealth && instance.ConsecutiveFailures >= max(policy.FailureThreshold, 1) {
		instance.Status = model.InstanceStatusUnhealth
		instance.StatusUpdatedAt = now
	}
	if instance.ConsecutiveFailures >= max(policy.DeleteThreshold, max(policy.FailureThreshold, 1)+1) {
		return instance, true
	}
	return instance, false
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
