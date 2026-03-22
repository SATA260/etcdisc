// swagger.go defines request and response models used by Swagger documentation.
package registry

import "etcdisc/internal/core/model"

// DeleteResponse describes a successful deregistration response.
type DeleteResponse struct {
	Status string `json:"status"`
}

// RegisterRequestBody wraps a registry register request.
type RegisterRequestBody struct {
	Instance        model.Instance `json:"instance"`
	LeaseTTLSeconds int64          `json:"leaseTTLSeconds"`
}

// HeartbeatRequestBody wraps a registry heartbeat request.
type HeartbeatRequestBody struct {
	Namespace  string `json:"namespace"`
	Service    string `json:"service"`
	InstanceID string `json:"instanceId"`
}

// UpdateRequestBody wraps a registry update request.
type UpdateRequestBody struct {
	Namespace        string            `json:"namespace"`
	Service          string            `json:"service"`
	InstanceID       string            `json:"instanceId"`
	Weight           *int              `json:"weight,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Version          *string           `json:"version,omitempty"`
	ExpectedRevision int64             `json:"expectedRevision"`
}

// DeregisterRequestBody wraps a registry deregister request.
type DeregisterRequestBody struct {
	Namespace  string `json:"namespace"`
	Service    string `json:"service"`
	InstanceID string `json:"instanceId"`
}
