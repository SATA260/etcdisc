// handlers.go exposes provider facing HTTP endpoints for registry operations.
package registry

import (
	"net/http"

	"etcdisc/internal/api/httpx"
	registrysvc "etcdisc/internal/core/service/registry"
)

// API serves registry write operations.
type API struct {
	Service *registrysvc.Service
}

// Register handles instance registration.
// @Summary Register instance
// @Description Register a runtime service instance.
// @Tags registry
// @Accept json
// @Produce json
// @Param request body RegisterRequestBody true "register request"
// @Success 201 {object} model.Instance
// @Failure 400 {object} httpx.ErrorResponse
// @Failure 409 {object} httpx.ErrorResponse
// @Failure 500 {object} httpx.ErrorResponse
// @Router /v1/registry/register [post]
func (h API) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input registrysvc.RegisterInput
	if err := httpx.DecodeJSON(r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	instance, err := h.Service.Register(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, instance)
}

// Heartbeat handles client heartbeats.
// @Summary Send heartbeat
// @Description Refresh a heartbeat-mode instance lease and state.
// @Tags registry
// @Accept json
// @Produce json
// @Param request body HeartbeatRequestBody true "heartbeat request"
// @Success 200 {object} model.Instance
// @Failure 400 {object} httpx.ErrorResponse
// @Failure 404 {object} httpx.ErrorResponse
// @Failure 412 {object} httpx.ErrorResponse
// @Router /v1/registry/heartbeat [post]
func (h API) Heartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input registrysvc.HeartbeatInput
	if err := httpx.DecodeJSON(r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	instance, err := h.Service.Heartbeat(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, instance)
}

// Update handles explicit instance updates.
// @Summary Update instance
// @Description Update mutable instance fields using expectedRevision CAS semantics.
// @Tags registry
// @Accept json
// @Produce json
// @Param request body UpdateRequestBody true "update request"
// @Success 200 {object} model.Instance
// @Failure 400 {object} httpx.ErrorResponse
// @Failure 404 {object} httpx.ErrorResponse
// @Failure 409 {object} httpx.ErrorResponse
// @Router /v1/registry/update [post]
func (h API) Update(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input registrysvc.UpdateInput
	if err := httpx.DecodeJSON(r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	instance, err := h.Service.Update(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, instance)
}

// Deregister handles graceful instance removal.
// @Summary Deregister instance
// @Description Remove a runtime instance and revoke its lease when present.
// @Tags registry
// @Accept json
// @Produce json
// @Param request body DeregisterRequestBody true "deregister request"
// @Success 200 {object} DeleteResponse
// @Failure 400 {object} httpx.ErrorResponse
// @Failure 404 {object} httpx.ErrorResponse
// @Router /v1/registry/deregister [post]
func (h API) Deregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input registrysvc.DeregisterInput
	if err := httpx.DecodeJSON(r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.Service.Deregister(r.Context(), input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
