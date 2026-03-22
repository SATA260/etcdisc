// swagger.go defines Swagger request and response models for A2A APIs.
package a2a

import "etcdisc/internal/core/model"

// DiscoverResponse wraps A2A discovery results.
type DiscoverResponse struct {
	Items []model.A2ADiscoveryResult `json:"items"`
}

// UpsertCardRequestBody wraps AgentCard upsert input.
type UpsertCardRequestBody struct {
	Card model.AgentCard `json:"card"`
}
