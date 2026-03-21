// system_handler.go exposes system metrics and readiness summary APIs for administrators.
package admin

import (
	"context"
	"net/http"

	"etcdisc/internal/api/httpx"
)

// SystemAPI serves lightweight system status responses.
type SystemAPI struct {
	Ready   ReadyCheck
	Metrics http.Handler
}

// HandleSummary returns current readiness and metrics endpoint metadata.
func (h SystemAPI) HandleSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ready := true
	if h.Ready != nil {
		ready = h.Ready(context.Background()) == nil
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ready": ready, "metricsPath": "/metrics", "healthPath": "/healthz", "readyPath": "/ready"})
}

// HandleMetrics proxies the Prometheus metrics endpoint through the admin surface.
func (h SystemAPI) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.Metrics == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.Metrics.ServeHTTP(w, r)
}
