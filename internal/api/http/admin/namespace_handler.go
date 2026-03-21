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
