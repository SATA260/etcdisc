// health_handler.go serves the basic health endpoint for liveness checks.
package admin

import "net/http"

// HealthHandler returns a simple liveness response.
func HealthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
