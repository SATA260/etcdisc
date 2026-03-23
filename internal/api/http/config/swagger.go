// swagger.go defines Swagger response models for config APIs.
package config

import "etcdisc/internal/core/model"

// EffectiveResponse wraps effective config results.
type EffectiveResponse struct {
	EffectiveConfig map[string]model.EffectiveConfigItem `json:"effectiveConfig"`
}
