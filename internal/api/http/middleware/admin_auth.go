// admin_auth.go enforces the static admin token used by phase 1 admin APIs and console pages.
package middleware

import (
	"net/http"
	"strings"

	"etcdisc/internal/api/httpx"
	apperrors "etcdisc/internal/core/errors"
)

const adminHeader = "Authorization"

// WithAdminToken protects admin endpoints with a static bearer token.
func WithAdminToken(expectedToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get(adminHeader))
		if expectedToken == "" || token != expectedToken {
			httpx.WriteError(w, apperrors.New(apperrors.CodeUnauthorized, "admin token is missing or invalid"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(header, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(header, prefix))
	}
	return strings.TrimSpace(header)
}
