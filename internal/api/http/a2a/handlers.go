// handlers.go exposes HTTP endpoints for AgentCard registration and capability discovery.
package a2a

import (
	"net/http"

	"etcdisc/internal/api/httpx"
	a2asvc "etcdisc/internal/core/service/a2a"
)

// API serves A2A registration and discovery endpoints.
type API struct {
	Service *a2asvc.Service
}

// UpsertCard handles AgentCard create and update requests.
func (h API) UpsertCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input a2asvc.PutInput
	if err := httpx.DecodeJSON(r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	card, err := h.Service.Put(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, card)
}

// Discover handles capability discovery requests.
func (h API) Discover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := h.Service.Discover(r.Context(), a2asvc.DiscoverInput{
		Namespace:        r.URL.Query().Get("namespace"),
		Capability:       r.URL.Query().Get("capability"),
		Protocol:         r.URL.Query().Get("protocol"),
		IncludeUnhealthy: r.URL.Query().Get("includeUnhealthy") == "true",
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": results})
}
