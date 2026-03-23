// audit_handler.go exposes audit log read APIs for administrators.
package admin

import (
	"net/http"

	"etcdisc/internal/api/httpx"
	auditsvc "etcdisc/internal/core/service/audit"
)

// AuditAPI serves audit log reads.
type AuditAPI struct {
	Service *auditsvc.Service
}

// HandleList returns audit entries.
func (h AuditAPI) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := h.Service.List(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
