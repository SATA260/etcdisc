// swagger.go defines Swagger response models for discovery APIs.
package discovery

import "etcdisc/internal/core/model"

// SnapshotResponse wraps discovery snapshot results.
type SnapshotResponse struct {
	Items []model.Instance `json:"items"`
}
