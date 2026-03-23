// swagger.go defines Swagger response models for admin APIs.
package admin

import "etcdisc/internal/core/model"

// NamespaceListResponse wraps namespace list results.
type NamespaceListResponse struct {
	Items []model.Namespace `json:"items"`
}

// ConfigItemsResponse wraps config item list results.
type ConfigItemsResponse struct {
	Items []model.ConfigItem `json:"items"`
}

// InstancesResponse wraps instance list results.
type InstancesResponse struct {
	Items []model.Instance `json:"items"`
}

// AgentCardsResponse wraps agent card list results.
type AgentCardsResponse struct {
	Items []model.AgentCard `json:"items"`
}

// CapabilityResultsResponse wraps admin A2A query results.
type CapabilityResultsResponse struct {
	Items []model.A2ADiscoveryResult `json:"items"`
}

// AuditResponse wraps audit entries.
type AuditResponse struct {
	Items []model.AuditEntry `json:"items"`
}

// SystemSummaryResponse describes basic system summary values.
type SystemSummaryResponse struct {
	Ready       bool   `json:"ready"`
	MetricsPath string `json:"metricsPath"`
	HealthPath  string `json:"healthPath"`
	ReadyPath   string `json:"readyPath"`
}

// DeleteResponse describes a successful delete result.
type DeleteResponse struct {
	Status string `json:"status"`
}

// CreateNamespaceRequestBody wraps namespace creation input.
type CreateNamespaceRequestBody struct {
	Name string `json:"name"`
}

// UpdateNamespaceRequestBody wraps namespace access-mode update input.
type UpdateNamespaceRequestBody struct {
	AccessMode       string `json:"accessMode"`
	ExpectedRevision int64  `json:"expectedRevision"`
}
