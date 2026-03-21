// errors_test.go verifies stable error code mappings for HTTP and gRPC transports.
package errors

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func TestHTTPStatusOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid argument", err: New(CodeInvalidArgument, "bad request"), status: http.StatusBadRequest},
		{name: "unauthorized", err: New(CodeUnauthorized, "unauthorized"), status: http.StatusUnauthorized},
		{name: "forbidden", err: New(CodeForbidden, "forbidden"), status: http.StatusForbidden},
		{name: "not found", err: New(CodeNotFound, "missing"), status: http.StatusNotFound},
		{name: "already exists", err: New(CodeAlreadyExists, "exists"), status: http.StatusConflict},
		{name: "conflict", err: New(CodeConflict, "conflict"), status: http.StatusConflict},
		{name: "failed precondition", err: New(CodeFailedPrecondition, "precondition"), status: http.StatusPreconditionFailed},
		{name: "unavailable", err: New(CodeUnavailable, "unavailable"), status: http.StatusServiceUnavailable},
		{name: "fallback", err: errors.New("boom"), status: http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.status, HTTPStatusOf(tc.err))
		})
	}
}

func TestGRPCStatusOf(t *testing.T) {
	t.Parallel()

	appErr := Wrap(CodeConflict, "conflict", errors.New("revision mismatch"))
	status := GRPCStatusOf(appErr)

	require.Equal(t, codes.Aborted, status.Code())
	require.Equal(t, "conflict", status.Message())
	require.True(t, IsCode(appErr, CodeConflict))
	require.Equal(t, CodeConflict, CodeOf(appErr))
	require.Equal(t, "conflict", MessageOf(appErr))
}
