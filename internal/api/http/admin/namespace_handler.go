// namespace_handler.go exposes namespace lifecycle APIs for administrators.
package admin

import (
	"net/http"
	"strings"

	"etcdisc/internal/api/httpx"
	"etcdisc/internal/core/model"
	auditsvc "etcdisc/internal/core/service/audit"
	namespacesvc "etcdisc/internal/core/service/namespace"
)

// NamespaceAPI serves namespace lifecycle management requests.
type NamespaceAPI struct {
	Service *namespacesvc.Service
	Audit   *auditsvc.Service
}

// HandleCollection serves namespace create and list operations.
// @Summary List or create namespaces
// @Description GET lists namespaces; POST creates a namespace. Requires admin token.
// @Tags admin
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer admin token"
// @Param request body CreateNamespaceRequestBody false "create namespace request"
// @Success 200 {object} NamespaceListResponse
// @Success 201 {object} model.Namespace
// @Failure 400 {object} httpx.ErrorResponse
// @Failure 401 {object} httpx.ErrorResponse
// @Failure 409 {object} httpx.ErrorResponse
// @Router /admin/v1/namespaces [get]
// @Router /admin/v1/namespaces [post]
func (h NamespaceAPI) HandleCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		namespaces, err := h.Service.List(r.Context())
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": namespaces})
	case http.MethodPost:
		var input namespacesvc.CreateNamespaceInput
		if err := httpx.DecodeJSON(r, &input); err != nil {
			httpx.WriteError(w, err)
			return
		}
		ns, err := h.Service.Create(r.Context(), input)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		h.record(r, "create", ns.Name, "created namespace")
		httpx.WriteJSON(w, http.StatusCreated, ns)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleItem serves namespace access-mode updates.
// @Summary Update namespace access mode
// @Description Update namespace accessMode using expectedRevision. Requires admin token.
// @Tags admin
// @Accept json
// @Produce json
// @Param Authorization header string true "Bearer admin token"
// @Param name path string true "namespace name"
// @Param request body UpdateNamespaceRequestBody true "update namespace request"
// @Success 200 {object} model.Namespace
// @Failure 400 {object} httpx.ErrorResponse
// @Failure 401 {object} httpx.ErrorResponse
// @Failure 404 {object} httpx.ErrorResponse
// @Failure 409 {object} httpx.ErrorResponse
// @Router /admin/v1/namespaces/{name} [patch]
func (h NamespaceAPI) HandleItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/admin/v1/namespaces/")
	var input namespacesvc.UpdateAccessModeInput
	if err := httpx.DecodeJSON(r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	input.Name = name
	ns, err := h.Service.UpdateAccessMode(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	h.record(r, "update_access_mode", ns.Name, "updated namespace access mode")
	httpx.WriteJSON(w, http.StatusOK, ns)
}

func (h NamespaceAPI) record(r *http.Request, action, namespaceName, message string) {
	if h.Audit == nil {
		return
	}
	_, _ = h.Audit.Record(r.Context(), model.AuditEntry{Actor: "admin", Action: action, Resource: "namespace", Namespace: namespaceName, Message: message})
}
