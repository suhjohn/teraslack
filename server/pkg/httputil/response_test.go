package httputil

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suhjohn/teraslack/internal/domain"
)

func TestWriteError_EmailAuthDisabled(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/signup", nil)
	rec := httptest.NewRecorder()

	WriteError(rec, req, domain.ErrEmailAuthDisabled)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var body APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if body.Code != "email_auth_unavailable" {
		t.Fatalf("code = %q, want email_auth_unavailable", body.Code)
	}
	if body.Message != "Email sign-in is not configured." {
		t.Fatalf("message = %q", body.Message)
	}
}

func TestWriteError_Unavailable(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()

	WriteError(rec, req, errors.Join(domain.ErrUnavailable, errors.New("backend offline")))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var body APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if body.Code != "service_unavailable" {
		t.Fatalf("code = %q, want service_unavailable", body.Code)
	}
}
