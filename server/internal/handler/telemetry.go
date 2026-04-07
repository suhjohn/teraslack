package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/johnsuh/teraslack/server/internal/domain"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func (s *Server) recordAPIAccess(auth domain.AuthContext, r *http.Request, status int, duration time.Duration) {
	if strings.HasPrefix(r.URL.Path, "/dashboard") {
		return
	}

	authKind := "session"
	var apiKeyID *uuid.UUID
	if auth.APIKeyID != nil {
		authKind = "api_key"
		apiKeyID = auth.APIKeyID
	}

	durationMs := duration.Milliseconds()
	if durationMs < 0 {
		durationMs = 0
	}
	if status == 0 {
		status = http.StatusOK
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := s.db.Exec(ctx, `insert into api_request_logs (
		auth_kind,
		user_id,
		api_key_id,
		scope_workspace_id,
		method,
		path_template,
		status_code,
		duration_ms,
		request_id,
		created_at
	) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		authKind,
		auth.UserID,
		apiKeyID,
		auth.APIKeyWorkspaceID,
		r.Method,
		normalizeRequestPath(r.URL.Path),
		status,
		int(durationMs),
		requestIDFromContext(r.Context()),
		time.Now().UTC(),
	)
	if err != nil {
		s.logger.Warn("record api access", "error", err)
	}
}

func normalizeRequestPath(path string) string {
	if path == "" || path == "/" {
		return "/"
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		switch {
		case isUUIDSegment(part):
			parts[i] = "{id}"
		case isOpaqueTokenSegment(part):
			parts[i] = "{token}"
		}
	}
	return "/" + strings.Join(parts, "/")
}

func isUUIDSegment(part string) bool {
	_, err := uuid.Parse(part)
	return err == nil
}

func isOpaqueTokenSegment(part string) bool {
	if len(part) < 16 {
		return false
	}
	for _, r := range part {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}
