package httputil

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/suhjohn/workspace/internal/domain"
)

// SlackResponse mirrors Slack's API response format.
type SlackResponse struct {
	OK               bool            `json:"ok"`
	Error            string          `json:"error,omitempty"`
	Warning          string          `json:"warning,omitempty"`
	ResponseMetadata json.RawMessage `json:"response_metadata,omitempty"`
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// WriteOK writes a successful Slack-style response.
func WriteOK(w http.ResponseWriter, data map[string]any) {
	resp := map[string]any{"ok": true}
	for k, v := range data {
		resp[k] = v
	}
	WriteJSON(w, http.StatusOK, resp)
}

// WriteError maps domain errors to Slack-style error responses.
func WriteError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	errCode := "internal_error"

	switch {
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusOK // Slack returns 200 with ok:false
		errCode = "not_found"
	case errors.Is(err, domain.ErrAlreadyExists):
		status = http.StatusOK
		errCode = "name_taken"
	case errors.Is(err, domain.ErrInvalidArgument):
		status = http.StatusOK
		errCode = "invalid_arguments"
	case errors.Is(err, domain.ErrForbidden):
		status = http.StatusOK
		errCode = "restricted_action"
	case errors.Is(err, domain.ErrAlreadyInChannel):
		status = http.StatusOK
		errCode = "already_in_channel"
	case errors.Is(err, domain.ErrNotInChannel):
		status = http.StatusOK
		errCode = "not_in_channel"
	case errors.Is(err, domain.ErrChannelArchived):
		status = http.StatusOK
		errCode = "is_archived"
	case errors.Is(err, domain.ErrAlreadyReacted):
		status = http.StatusOK
		errCode = "already_reacted"
	case errors.Is(err, domain.ErrNoReaction):
		status = http.StatusOK
		errCode = "no_reaction"
	case errors.Is(err, domain.ErrNameTaken):
		status = http.StatusOK
		errCode = "name_taken"
	}

	if errCode == "internal_error" {
		slog.Error("internal error", "error", err.Error())
	}
	WriteJSON(w, status, SlackResponse{OK: false, Error: errCode})
}

// DecodeJSON decodes a JSON request body into the given value.
func DecodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return err
	}
	return nil
}
