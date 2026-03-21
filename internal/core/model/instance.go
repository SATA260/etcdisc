// instance.go defines the runtime instance model and health semantics used by registry, probe, and discovery flows.
package model

import (
	"fmt"
	"regexp"
	"time"

	apperrors "etcdisc/internal/core/errors"
)

var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$`)

const (
	// DefaultGroup is applied when a provider omits the group field.
	DefaultGroup = "default"
	// DefaultWeight is applied when a provider omits the weight field.
	DefaultWeight = 100
)

// HealthCheckMode identifies how an instance stays alive.
type HealthCheckMode string

const (
	// HealthCheckHeartbeat binds instance liveness to heartbeats and a lease.
	HealthCheckHeartbeat HealthCheckMode = "heartbeat"
	// HealthCheckHTTPProbe makes the server probe an HTTP endpoint.
	HealthCheckHTTPProbe HealthCheckMode = "http_probe"
	// HealthCheckGRPCProbe makes the server probe a gRPC endpoint.
	HealthCheckGRPCProbe HealthCheckMode = "grpc_probe"
	// HealthCheckTCPProbe makes the server probe a TCP socket.
	HealthCheckTCPProbe HealthCheckMode = "tcp_probe"
)

// InstanceStatus identifies the current externally visible state of an instance.
type InstanceStatus string

const (
	// InstanceStatusHealth means the instance is currently healthy.
	InstanceStatusHealth InstanceStatus = "health"
	// InstanceStatusUnhealth means the instance is currently unhealthy but still visible when requested.
	InstanceStatusUnhealth InstanceStatus = "unhealth"
)

// ProbeConfig captures explicit probe target overrides supplied by the client.
type ProbeConfig struct {
	Address string `json:"address,omitempty"`
	Port    int    `json:"port,omitempty"`
	Path    string `json:"path,omitempty"`
}

// Instance represents a registered service instance.
type Instance struct {
	Namespace            string            `json:"namespace"`
	Service              string            `json:"service"`
	Group                string            `json:"group"`
	Version              string            `json:"version"`
	Weight               int               `json:"weight"`
	InstanceID           string            `json:"instanceId"`
	AgentID              string            `json:"agentId,omitempty"`
	Address              string            `json:"address"`
	Port                 int               `json:"port"`
	Metadata             map[string]string `json:"metadata"`
	HealthCheckMode      HealthCheckMode   `json:"healthCheckMode"`
	ProbeConfig          ProbeConfig       `json:"probeConfig,omitempty"`
	Status               InstanceStatus    `json:"status"`
	LeaseID              int64             `json:"leaseId,omitempty"`
	ConsecutiveFailures  int               `json:"consecutiveFailures"`
	ConsecutiveSuccesses int               `json:"consecutiveSuccesses"`
	LastHeartbeatAt      time.Time         `json:"lastHeartbeatAt"`
	LastProbeAt          time.Time         `json:"lastProbeAt"`
	CreatedAt            time.Time         `json:"createdAt"`
	UpdatedAt            time.Time         `json:"updatedAt"`
	StatusUpdatedAt      time.Time         `json:"statusUpdatedAt"`
	Revision             int64             `json:"revision"`
}

// Normalize applies documented defaults before validation.
func (i *Instance) Normalize() {
	if i.Group == "" {
		i.Group = DefaultGroup
	}
	if i.Weight == 0 {
		i.Weight = DefaultWeight
	}
	if i.HealthCheckMode == "" {
		i.HealthCheckMode = HealthCheckHeartbeat
	}
	if i.Metadata == nil {
		i.Metadata = map[string]string{}
	}
	if i.Status == "" {
		i.Status = InstanceStatusHealth
	}
	if i.ProbeConfig.Address == "" {
		i.ProbeConfig.Address = i.Address
	}
	if i.ProbeConfig.Port == 0 {
		i.ProbeConfig.Port = i.Port
	}
	if i.HealthCheckMode == HealthCheckHTTPProbe && i.ProbeConfig.Path == "" {
		i.ProbeConfig.Path = "/etcdisc/http/health"
	}
	if i.HealthCheckMode == HealthCheckGRPCProbe && i.ProbeConfig.Path == "" {
		i.ProbeConfig.Path = "/etcdisc/grpc/health"
	}
	if i.HealthCheckMode == HealthCheckTCPProbe {
		i.ProbeConfig.Path = ""
	}
}

// Validate validates the instance registration shape.
func (i Instance) Validate() error {
	if err := ValidateNamespaceName(i.Namespace); err != nil {
		return err
	}
	if err := ValidateServiceName(i.Service); err != nil {
		return err
	}
	if i.InstanceID != "" {
		if err := ValidateInstanceID(i.InstanceID); err != nil {
			return err
		}
	}
	if i.Address == "" || !hostnamePattern.MatchString(i.Address) {
		return apperrors.New(apperrors.CodeInvalidArgument, "instance address must be a non-empty host or IP")
	}
	if i.Port < 1 || i.Port > 65535 {
		return apperrors.New(apperrors.CodeInvalidArgument, "instance port must be within 1-65535")
	}
	if i.Weight < 1 || i.Weight > 10000 {
		return apperrors.New(apperrors.CodeInvalidArgument, "instance weight must be within 1-10000")
	}
	if err := ValidateFlatMetadata(i.Metadata); err != nil {
		return err
	}
	if !i.HealthCheckMode.Valid() {
		return apperrors.New(apperrors.CodeInvalidArgument, "healthCheckMode is invalid")
	}
	if !i.Status.Valid() {
		return apperrors.New(apperrors.CodeInvalidArgument, "instance status is invalid")
	}
	if i.ProbeConfig.Port < 0 || i.ProbeConfig.Port > 65535 {
		return apperrors.New(apperrors.CodeInvalidArgument, "probe port must be within 0-65535")
	}
	if i.ProbeConfig.Address == "" && i.HealthCheckMode != HealthCheckHeartbeat {
		return apperrors.New(apperrors.CodeInvalidArgument, "probe address is required for probe mode")
	}
	return nil
}

// Valid reports whether the health check mode is supported.
func (m HealthCheckMode) Valid() bool {
	switch m {
	case HealthCheckHeartbeat, HealthCheckHTTPProbe, HealthCheckGRPCProbe, HealthCheckTCPProbe:
		return true
	default:
		return false
	}
}

// IsProbe reports whether the instance uses server-side probing.
func (m HealthCheckMode) IsProbe() bool {
	return m == HealthCheckHTTPProbe || m == HealthCheckGRPCProbe || m == HealthCheckTCPProbe
}

// Valid reports whether the instance status is supported.
func (s InstanceStatus) Valid() bool {
	switch s {
	case InstanceStatusHealth, InstanceStatusUnhealth:
		return true
	default:
		return false
	}
}

// Endpoint returns the host:port endpoint for the registered instance.
func (i Instance) Endpoint() string {
	return fmt.Sprintf("%s:%d", i.Address, i.Port)
}
