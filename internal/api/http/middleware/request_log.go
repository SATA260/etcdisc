// request_log.go adds simple request logging middleware for HTTP handlers.
package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// WithRequestLog wraps an HTTP handler with structured request logging.
func WithRequestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}
