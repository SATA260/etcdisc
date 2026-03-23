// metrics.go exposes Prometheus metrics handlers used by the etcdisc server.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns the Prometheus metrics endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}
