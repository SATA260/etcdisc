// response.go provides small HTTP helpers for JSON APIs, typed errors, and SSE output.
package httpx

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"

	apperrors "etcdisc/internal/core/errors"
)

// ErrorResponse is the JSON error envelope used by HTTP handlers.
type ErrorResponse struct {
	Code    apperrors.Code `json:"code"`
	Message string         `json:"message"`
}

// DecodeJSON reads a JSON request body into the target structure.
func DecodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return apperrors.Wrap(apperrors.CodeInvalidArgument, "invalid JSON request body", err)
	}
	return nil
}

// WriteJSON writes a JSON response with the provided status code.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// WriteError maps typed application errors to HTTP JSON responses.
func WriteError(w http.ResponseWriter, err error) {
	WriteJSON(w, apperrors.HTTPStatusOf(err), ErrorResponse{Code: apperrors.CodeOf(err), Message: apperrors.MessageOf(err)})
}

// BeginSSE prepares the response writer for a server-sent events stream.
func BeginSSE(w http.ResponseWriter) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return apperrors.New(apperrors.CodeInternal, "response writer does not support streaming")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return nil
}

// WriteSSE writes a single SSE event and flushes the stream.
func WriteSSE(w http.ResponseWriter, event string, payload any) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return apperrors.New(apperrors.CodeInternal, "response writer does not support streaming")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return apperrors.Wrap(apperrors.CodeInternal, "marshal sse payload failed", err)
	}
	writer := bufio.NewWriter(w)
	if _, err := fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, body); err != nil {
		return apperrors.Wrap(apperrors.CodeUnavailable, "write sse payload failed", err)
	}
	if err := writer.Flush(); err != nil {
		return apperrors.Wrap(apperrors.CodeUnavailable, "flush sse payload failed", err)
	}
	flusher.Flush()
	return nil
}
