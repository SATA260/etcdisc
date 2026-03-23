// config_handler.go exposes admin APIs for raw config item management.
package admin

import (
	"net/http"

	"etcdisc/internal/api/httpx"
	apperrors "etcdisc/internal/core/errors"
	"etcdisc/internal/core/model"
	auditsvc "etcdisc/internal/core/service/audit"
	configsvc "etcdisc/internal/core/service/config"
)

// ConfigAPI serves management requests for config items and policy values.
type ConfigAPI struct {
	Service *configsvc.Service
	Audit   *auditsvc.Service
}

// HandleItems serves config list, publish, and delete operations.
func (h ConfigAPI) HandleItems(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		scope := model.ConfigScope(r.URL.Query().Get("scope"))
		if !scope.Valid() {
			httpx.WriteError(w, modelErrInvalidScope())
			return
		}
		items, err := h.Service.GetRaw(r.Context(), scope, r.URL.Query().Get("namespace"), r.URL.Query().Get("service"), true)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var input configsvc.PutInput
		if err := httpx.DecodeJSON(r, &input); err != nil {
			httpx.WriteError(w, err)
			return
		}
		item, err := h.Service.Put(r.Context(), input)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		h.record(r, "put_config", item.Namespace, item.Service, item.Key)
		httpx.WriteJSON(w, http.StatusCreated, item)
	case http.MethodDelete:
		var input configsvc.DeleteInput
		if err := httpx.DecodeJSON(r, &input); err != nil {
			httpx.WriteError(w, err)
			return
		}
		if err := h.Service.Delete(r.Context(), input); err != nil {
			httpx.WriteError(w, err)
			return
		}
		h.record(r, "delete_config", input.Namespace, input.Service, input.Key)
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h ConfigAPI) record(r *http.Request, action, namespaceName, serviceName, key string) {
	if h.Audit == nil {
		return
	}
	_, _ = h.Audit.Record(r.Context(), model.AuditEntry{Actor: "admin", Action: action, Resource: "config", Namespace: namespaceName, Service: serviceName, Message: key})
}

func modelErrInvalidScope() error {
	return apperrors.New(apperrors.CodeInvalidArgument, "config scope is invalid")
}
