// audit.go defines audit entries used by phase 1 admin APIs and console pages.
package model

import "time"

// AuditEntry captures one management or runtime action for later inspection.
type AuditEntry struct {
	ID        string            `json:"id"`
	Actor     string            `json:"actor"`
	Action    string            `json:"action"`
	Resource  string            `json:"resource"`
	Namespace string            `json:"namespace,omitempty"`
	Service   string            `json:"service,omitempty"`
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	Revision  int64             `json:"revision"`
}
