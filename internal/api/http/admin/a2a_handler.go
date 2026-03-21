// a2a_handler.go exposes admin APIs for AgentCard inventory and capability discovery inspection.
package admin

import (
	"net/http"

	"etcdisc/internal/api/httpx"
	a2asvc "etcdisc/internal/core/service/a2a"
)

// A2AAPI serves admin requests related to AgentCards and capabilities.
type A2AAPI struct {
	Service *a2asvc.Service
}

// HandleCards returns AgentCards or capability discovery results based on query parameters.
func (h A2AAPI) HandleCards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	namespaceName := r.URL.Query().Get("namespace")
	if capability := r.URL.Query().Get("capability"); capability != "" {
		items, err := h.Service.Discover(r.Context(), a2asvc.DiscoverInput{Namespace: namespaceName, Capability: capability, Protocol: r.URL.Query().Get("protocol"), IncludeUnhealthy: r.URL.Query().Get("includeUnhealthy") == "true", Admin: true})
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	items, err := h.Service.List(r.Context(), namespaceName, true)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
