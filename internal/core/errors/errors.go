// errors.go defines typed application errors shared by phase 1 services and transports.
package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Code identifies a stable error category exposed by HTTP and gRPC.
type Code string

const (
	// CodeInvalidArgument marks validation and bad-request failures.
	CodeInvalidArgument Code = "invalid_argument"
	// CodeNotFound marks missing resources.
	CodeNotFound Code = "not_found"
	// CodeAlreadyExists marks create conflicts.
	CodeAlreadyExists Code = "already_exists"
	// CodeConflict marks CAS conflicts and concurrent write failures.
	CodeConflict Code = "conflict"
	// CodeForbidden marks permission denials caused by namespace access modes.
	CodeForbidden Code = "forbidden"
	// CodeUnauthorized marks missing or invalid admin authentication.
	CodeUnauthorized Code = "unauthorized"
	// CodeFailedPrecondition marks state dependent rejections.
	CodeFailedPrecondition Code = "failed_precondition"
	// CodeUnavailable marks temporary dependency failures.
	CodeUnavailable Code = "unavailable"
	// CodeInternal marks unexpected server failures.
	CodeInternal Code = "internal"
)

// Error carries a stable code plus human-readable message.
type Error struct {
	Code    Code
	Message string
	Err     error
}

// Error returns a printable error string.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

// Unwrap exposes the underlying cause.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// GRPCStatus exposes the error as a gRPC status.
func (e *Error) GRPCStatus() *status.Status {
	if e == nil {
		return status.New(codes.OK, "")
	}
	return status.New(e.GRPCCode(), e.Message)
}

// HTTPStatus returns the mapped HTTP status code.
func (e *Error) HTTPStatus() int {
	if e == nil {
		return http.StatusOK
	}
	switch e.Code {
	case CodeInvalidArgument:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeAlreadyExists, CodeConflict:
		return http.StatusConflict
	case CodeFailedPrecondition:
		return http.StatusPreconditionFailed
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// GRPCCode returns the mapped gRPC status code.
func (e *Error) GRPCCode() codes.Code {
	if e == nil {
		return codes.OK
	}
	switch e.Code {
	case CodeInvalidArgument:
		return codes.InvalidArgument
	case CodeUnauthorized:
		return codes.Unauthenticated
	case CodeForbidden:
		return codes.PermissionDenied
	case CodeNotFound:
		return codes.NotFound
	case CodeAlreadyExists:
		return codes.AlreadyExists
	case CodeConflict:
		return codes.Aborted
	case CodeFailedPrecondition:
		return codes.FailedPrecondition
	case CodeUnavailable:
		return codes.Unavailable
	default:
		return codes.Internal
	}
}

// New creates a typed application error.
func New(code Code, message string) error {
	return &Error{Code: code, Message: message}
}

// Wrap creates a typed application error with an underlying cause.
func Wrap(code Code, message string, err error) error {
	return &Error{Code: code, Message: message, Err: err}
}

// CodeOf extracts the stable error code from an error chain.
func CodeOf(err error) Code {
	var appErr *Error
	if stderrors.As(err, &appErr) {
		return appErr.Code
	}
	return CodeInternal
}

// MessageOf extracts the public message from an error chain.
func MessageOf(err error) string {
	var appErr *Error
	if stderrors.As(err, &appErr) {
		return appErr.Message
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// HTTPStatusOf maps any error to an HTTP status code.
func HTTPStatusOf(err error) int {
	var appErr *Error
	if stderrors.As(err, &appErr) {
		return appErr.HTTPStatus()
	}
	if err == nil {
		return http.StatusOK
	}
	return http.StatusInternalServerError
}

// GRPCStatusOf maps any error to a gRPC status.
func GRPCStatusOf(err error) *status.Status {
	var appErr *Error
	if stderrors.As(err, &appErr) {
		return appErr.GRPCStatus()
	}
	if err == nil {
		return status.New(codes.OK, "")
	}
	return status.New(codes.Internal, err.Error())
}

// IsCode reports whether an error chain contains the given typed code.
func IsCode(err error, code Code) bool {
	return CodeOf(err) == code
}
