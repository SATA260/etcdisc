// handlers.go exposes consumer facing HTTP discovery snapshot and watch endpoints.
package discovery

import (
	"net/http"
	"strconv"
	"strings"

	"etcdisc/internal/api/httpx"
	discoverysvc "etcdisc/internal/core/service/discovery"
)

// API serves discovery snapshot and watch requests.
type API struct {
	Service *discoverysvc.Service
}

// Snapshot handles discovery snapshot queries.
// @Summary List discovered instances
// @Description Return the current discovery snapshot for a namespace and service.
// @Tags discovery
// @Produce json
// @Param namespace query string true "namespace"
// @Param service query string true "service"
// @Param group query string false "group"
// @Param version query string false "version"
// @Param healthyOnly query bool false "only return healthy instances"
// @Success 200 {object} SnapshotResponse
// @Failure 400 {object} httpx.ErrorResponse
// @Failure 403 {object} httpx.ErrorResponse
// @Failure 404 {object} httpx.ErrorResponse
// @Router /v1/discovery/instances [get]
func (h API) Snapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	input := discoverysvc.SnapshotInput{
		Namespace: r.URL.Query().Get("namespace"),
		Service:   r.URL.Query().Get("service"),
		Group:     r.URL.Query().Get("group"),
		Version:   r.URL.Query().Get("version"),
		Metadata:  parseMetadataFilters(r),
	}
	if healthyOnly, ok := parseBoolQuery(r, "healthyOnly"); ok {
		input.HealthyOnly = &healthyOnly
	}
	instances, err := h.Service.Snapshot(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": instances})
}

// Watch handles SSE instance watch requests.
// @Summary Watch discovered instances
// @Description Open an SSE stream for instance watch events.
// @Tags discovery
// @Produce text/event-stream
// @Param namespace query string true "namespace"
// @Param service query string true "service"
// @Param healthyOnly query bool false "only stream healthy instances"
// @Param revision query int false "start revision"
// @Success 200 {string} string "SSE stream"
// @Router /v1/discovery/watch [get]
func (h API) Watch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := httpx.BeginSSE(w); err != nil {
		httpx.WriteError(w, err)
		return
	}
	input := discoverysvc.WatchInput{Namespace: r.URL.Query().Get("namespace"), Service: r.URL.Query().Get("service")}
	if revision := r.URL.Query().Get("revision"); revision != "" {
		parsed, err := strconv.ParseInt(revision, 10, 64)
		if err == nil {
			input.Revision = parsed
		}
	}
	if healthyOnly, ok := parseBoolQuery(r, "healthyOnly"); ok {
		input.HealthyOnly = &healthyOnly
	}
	for event := range h.Service.Watch(r.Context(), input) {
		if err := httpx.WriteSSE(w, string(event.Type), event); err != nil {
			return
		}
	}
}

func parseBoolQuery(r *http.Request, key string) (bool, bool) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, false
	}
	return parsed, true
}

func parseMetadataFilters(r *http.Request) map[string]string {
	filters := map[string]string{}
	for key, values := range r.URL.Query() {
		if !strings.HasPrefix(key, "meta.") || len(values) == 0 {
			continue
		}
		filters[strings.TrimPrefix(key, "meta.")] = values[0]
	}
	if len(filters) == 0 {
		return nil
	}
	return filters
}
