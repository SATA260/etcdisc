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
