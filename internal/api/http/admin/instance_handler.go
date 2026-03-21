// instance_handler.go exposes admin APIs for listing services and runtime instances.
package admin

import (
	"net/http"
	"strconv"

	"etcdisc/internal/api/httpx"
	registrysvc "etcdisc/internal/core/service/registry"
)

// InstanceAPI serves admin instance overview requests.
type InstanceAPI struct {
	Service *registrysvc.Service
}

// HandleList returns runtime instances for a namespace and optional service.
func (h InstanceAPI) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	healthyOnly := false
	if value := r.URL.Query().Get("healthyOnly"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			healthyOnly = parsed
		}
	}
	items, err := h.Service.List(r.Context(), registrysvc.ListFilter{Namespace: r.URL.Query().Get("namespace"), Service: r.URL.Query().Get("service"), Group: r.URL.Query().Get("group"), Version: r.URL.Query().Get("version"), HealthyOnly: healthyOnly, Admin: true})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
