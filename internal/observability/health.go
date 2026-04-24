package observability

import (
	"net/http"
	"sync/atomic"
)

// Health tracks server readiness state and exposes HTTP health-check handlers.
type Health struct {
	ready atomic.Bool
}

// SetReady marks the server as ready (true) or not ready (false).
func (h *Health) SetReady(ready bool) {
	h.ready.Store(ready)
}

// HealthzHandler returns a handler that always responds HTTP 200 OK as long as
// the process is alive (Kubernetes liveness probe).
func (h *Health) HealthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// ReadyzHandler returns a handler that responds HTTP 200 OK when the server is
// ready to handle traffic, or HTTP 503 Service Unavailable otherwise
// (Kubernetes readiness probe).
func (h *Health) ReadyzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if h.ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
		}
	}
}
