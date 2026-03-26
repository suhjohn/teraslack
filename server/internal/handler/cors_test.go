package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSAllowsFrontendURLByDefault(t *testing.T) {
	middleware := CORS("https://teraslack.ai", nil)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.Header.Set("Origin", "https://teraslack.ai")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://teraslack.ai" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://teraslack.ai")
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want %q", got, "true")
	}
}

func TestCORSAllowsAdditionalConfiguredOrigin(t *testing.T) {
	middleware := CORS("https://app.teraslack.ai", []string{"https://teraslack.ai"})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.Header.Set("Origin", "https://teraslack.ai")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://teraslack.ai" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://teraslack.ai")
	}
}

func TestCORSDoesNotAllowUnexpectedOrigin(t *testing.T) {
	middleware := CORS("https://teraslack.ai", []string{"https://admin.teraslack.ai"})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestCORSPreflightAllowedOriginReturnsNoContent(t *testing.T) {
	middleware := CORS("https://teraslack.ai", nil)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("preflight request should not hit next handler")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/users", nil)
	req.Header.Set("Origin", "https://teraslack.ai")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://teraslack.ai" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://teraslack.ai")
	}
}
