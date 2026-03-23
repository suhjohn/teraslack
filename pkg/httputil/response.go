package httputil

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/suhjohn/teraslack/internal/domain"
)

type requestIDKey struct{}

// APIError is the canonical error response body for the HTTP API.
type APIError struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	RequestID string       `json:"request_id,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

// FieldError provides per-field validation details when a request is invalid.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CollectionResponse is the standard paginated collection envelope.
type CollectionResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// WithRequestID stores the request id in context for downstream consumers.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// GetRequestID returns the request id from context if one is present.
func GetRequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteResource writes a resource representation with the provided status.
func WriteResource(w http.ResponseWriter, status int, v any) {
	WriteJSON(w, status, v)
}

// WriteCreated writes a created resource response and optional Location header.
func WriteCreated(w http.ResponseWriter, location string, v any) {
	if strings.TrimSpace(location) != "" {
		w.Header().Set("Location", location)
	}
	WriteJSON(w, http.StatusCreated, v)
}

// WriteNoContent writes a successful empty response.
func WriteNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// WriteCollection writes a standard collection response.
func WriteCollection[T any](w http.ResponseWriter, status int, items []T, nextCursor string) {
	WriteJSON(w, status, CollectionResponse[T]{
		Items:      items,
		NextCursor: nextCursor,
	})
}

// WriteErrorResponse writes a canonical API error body.
func WriteErrorResponse(w http.ResponseWriter, r *http.Request, status int, code, message string, fieldErrors ...FieldError) {
	if code == "" {
		code = "internal_error"
	}
	if message == "" {
		message = "An unexpected error occurred."
	}

	resp := APIError{
		Code:      code,
		Message:   message,
		RequestID: GetRequestID(r.Context()),
	}
	if len(fieldErrors) > 0 {
		resp.Errors = fieldErrors
	}
	WriteJSON(w, status, resp)
}

// WriteInternalError writes the canonical unexpected server error response.
func WriteInternalError(w http.ResponseWriter, r *http.Request) {
	WriteErrorResponse(w, r, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
}

// WriteError maps domain errors to canonical API errors.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "An unexpected error occurred."

	switch {
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
		message = "The requested resource was not found."
	case errors.Is(err, domain.ErrAlreadyExists):
		status = http.StatusConflict
		code = "already_exists"
		message = "A resource with the same identifier already exists."
	case errors.Is(err, domain.ErrInvalidArgument):
		status = http.StatusBadRequest
		code = "invalid_request"
		message = "The request is invalid."
	case errors.Is(err, domain.ErrForbidden):
		status = http.StatusForbidden
		code = "forbidden"
		message = "You are not allowed to perform this action."
	case errors.Is(err, domain.ErrAlreadyInChannel):
		status = http.StatusConflict
		code = "already_in_conversation"
		message = "The user is already a member of the conversation."
	case errors.Is(err, domain.ErrNotInChannel):
		status = http.StatusConflict
		code = "not_in_conversation"
		message = "The user is not a member of the conversation."
	case errors.Is(err, domain.ErrAlreadyShared):
		status = http.StatusConflict
		code = "already_shared"
		message = "The file is already shared with that conversation."
	case errors.Is(err, domain.ErrChannelArchived):
		status = http.StatusConflict
		code = "conversation_archived"
		message = "The conversation is archived."
	case errors.Is(err, domain.ErrAlreadyReacted):
		status = http.StatusConflict
		code = "reaction_exists"
		message = "The reaction already exists."
	case errors.Is(err, domain.ErrNoReaction):
		status = http.StatusNotFound
		code = "reaction_not_found"
		message = "The reaction was not found."
	case errors.Is(err, domain.ErrNameTaken):
		status = http.StatusConflict
		code = "name_taken"
		message = "The requested name is already in use."
	case errors.Is(err, domain.ErrAlreadyPinned):
		status = http.StatusConflict
		code = "pin_exists"
		message = "The pin already exists."
	case errors.Is(err, domain.ErrInvalidAuth):
		status = http.StatusUnauthorized
		code = "invalid_authentication"
		message = "Authentication credentials are invalid."
	case errors.Is(err, domain.ErrTokenRevoked):
		status = http.StatusUnauthorized
		code = "token_revoked"
		message = "The token has been revoked."
	}

	if status == http.StatusInternalServerError {
		slog.Error("internal error", "error", err.Error(), "request_id", GetRequestID(r.Context()))
	}
	WriteErrorResponse(w, r, status, code, message)
}

// DecodeJSON decodes a JSON request body into the given value.
func DecodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return err
	}
	return nil
}
