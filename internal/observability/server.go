package observability

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// NewServer builds an HTTP mux with the following routes:
//
//	GET /metrics  — Prometheus metrics (text/OpenMetrics format)
//	GET /healthz  — liveness probe (always 200 OK)
//	GET /readyz   — readiness probe (200 OK / 503 Service Unavailable)
func NewServer(metrics *Metrics, health *Health) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/healthz", health.HealthzHandler())
	mux.HandleFunc("/readyz", health.ReadyzHandler())
	return mux
}

// RunServer starts an HTTP server on addr and blocks until ctx is cancelled,
// at which point the server is shut down gracefully.
func RunServer(ctx context.Context, addr string, handler http.Handler) error {
	srv := &http.Server{
		Addr:        addr,
		Handler:     handler,
		ReadTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("observability server shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("observability server: %w", err)
		}
		return nil
	}
}
