package teraslackmcp

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

type heartbeatResponseWriter struct {
	http.ResponseWriter

	mu          sync.Mutex
	wroteHeader bool
}

func (w *heartbeatResponseWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
	w.mu.Unlock()
}

func (w *heartbeatResponseWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.wroteHeader = true
	return w.ResponseWriter.Write(p)
}

func (w *heartbeatResponseWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *heartbeatResponseWriter) headerWritten() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.wroteHeader
}

func withSSEHeartbeat(next http.Handler, interval time.Duration) http.Handler {
	if interval <= 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !acceptsEventStream(r.Header) {
			next.ServeHTTP(w, r)
			return
		}

		hw := &heartbeatResponseWriter{ResponseWriter: w}

		ctx := r.Context()
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					// Avoid writing before the SDK has set Content-Type headers.
					if !hw.headerWritten() {
						continue
					}
					_, _ = hw.Write([]byte(": keepalive\n\n"))
					hw.Flush()
				}
			}
		}()

		next.ServeHTTP(hw, r)
	})
}

func acceptsEventStream(h http.Header) bool {
	accept := strings.Split(strings.Join(h.Values("Accept"), ","), ",")
	for _, v := range accept {
		v = strings.TrimSpace(v)
		if v == "text/event-stream" || v == "text/*" || v == "*/*" {
			return true
		}
	}
	return false
}
