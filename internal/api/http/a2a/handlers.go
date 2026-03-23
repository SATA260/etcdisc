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
// @Summary Upsert AgentCard
// @Description Create or update an AgentCard resource.
// @Tags a2a
// @Accept json
// @Produce json
// @Param request body UpsertCardRequestBody true "AgentCard upsert request"
// @Success 201 {object} model.AgentCard
// @Failure 400 {object} httpx.ErrorResponse
// @Failure 403 {object} httpx.ErrorResponse
// @Failure 409 {object} httpx.ErrorResponse
// @Router /v1/a2a/agentcards [post]
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
// @Summary Discover agents by capability
// @Description Query AgentCards and runtime instances by exact capability match.
// @Tags a2a
// @Produce json
// @Param namespace query string true "namespace"
// @Param capability query string true "capability"
// @Param protocol query string false "protocol"
// @Param includeUnhealthy query bool false "include unhealthy instances"
// @Success 200 {object} DiscoverResponse
// @Failure 400 {object} httpx.ErrorResponse
// @Failure 403 {object} httpx.ErrorResponse
// @Failure 404 {object} httpx.ErrorResponse
// @Router /v1/a2a/discovery [get]
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
