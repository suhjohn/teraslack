package handler

import (
	"net/http"
	"strings"
)

// CORS allows the frontend origin to call the API with credentials.
func CORS(frontendURL string, allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins)+1)
	addOrigin := func(value string) {
		origin := strings.TrimRight(strings.TrimSpace(value), "/")
		if origin == "" {
			return
		}
		allowed[origin] = struct{}{}
	}
	addOrigin(frontendURL)
	for _, origin := range allowedOrigins {
		addOrigin(origin)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
			if _, ok := allowed[origin]; origin != "" && ok {
				headers := w.Header()
				headers.Set("Access-Control-Allow-Origin", origin)
				headers.Set("Vary", "Origin")
				headers.Set("Access-Control-Allow-Credentials", "true")
				headers.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-Id")
				headers.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
