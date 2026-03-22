// handlers.go exposes business facing HTTP config snapshot and watch endpoints.
package config

import (
	"net/http"
	"strconv"

	"etcdisc/internal/api/httpx"
	configsvc "etcdisc/internal/core/service/config"
)

// API serves config resolution and watch endpoints.
type API struct {
	Service *configsvc.Service
}

// Effective handles effective config reads.
// @Summary Get effective config
// @Description Return effective config after global, namespace, and service overlays are applied.
// @Tags config
// @Produce json
// @Param namespace query string true "namespace"
// @Param service query string true "service"
// @Success 200 {object} EffectiveResponse
// @Failure 400 {object} httpx.ErrorResponse
// @Failure 403 {object} httpx.ErrorResponse
// @Failure 404 {object} httpx.ErrorResponse
// @Router /v1/config/effective [get]
func (h API) Effective(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	resolved, err := h.Service.Resolve(r.Context(), configsvc.ResolveInput{Namespace: r.URL.Query().Get("namespace"), Service: r.URL.Query().Get("service")})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"effectiveConfig": resolved})
}

// Watch handles SSE config watch streams.
// @Summary Watch config changes
// @Description Open an SSE stream for config change events.
// @Tags config
// @Produce text/event-stream
// @Param namespace query string true "namespace"
// @Param service query string true "service"
// @Param revision query int false "start revision"
// @Success 200 {string} string "SSE stream"
// @Router /v1/config/watch [get]
func (h API) Watch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := httpx.BeginSSE(w); err != nil {
		httpx.WriteError(w, err)
		return
	}
	input := configsvc.WatchInput{Namespace: r.URL.Query().Get("namespace"), Service: r.URL.Query().Get("service")}
	if revision := r.URL.Query().Get("revision"); revision != "" {
		if parsed, err := strconv.ParseInt(revision, 10, 64); err == nil {
			input.Revision = parsed
		}
	}
	for event := range h.Service.Watch(r.Context(), input) {
		if err := httpx.WriteSSE(w, string(event.Type), event); err != nil {
			return
		}
	}
}
