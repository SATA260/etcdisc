// ready_handler.go serves a readiness endpoint backed by dependency checks such as etcd connectivity.
package admin

import (
	"context"
	"net/http"
	"time"
)

// ReadyCheck verifies whether the server is ready to serve traffic.
type ReadyCheck func(ctx context.Context) error

// ReadyHandler returns a simple readiness response.
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	NewReadyHandler(nil).ServeHTTP(w, r)
}

// NewReadyHandler returns a readiness handler bound to a dependency check.
func NewReadyHandler(check ReadyCheck) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := check(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("not ready"))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
}
