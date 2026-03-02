package observability

import (
	"context"
	"fmt"
	"net"
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

// Listen creates a TCP listener on addr.  The error is returned synchronously
// so the caller can fail fast before launching any goroutines.
func Listen(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("observability server listen %s: %w", addr, err)
	}
	return ln, nil
}

// Serve starts an HTTP server using the pre-bound listener and blocks until
// ctx is cancelled, at which point the server is shut down gracefully.
//
// Callers should use [Listen] first to surface bind errors synchronously, then
// launch Serve in a goroutine:
//
//	ln, err := observability.Listen(addr)
//	if err != nil { return err }
//	go observability.Serve(ctx, ln, handler)
func Serve(ctx context.Context, ln net.Listener, handler http.Handler) error {
	srv := &http.Server{
		Handler:     handler,
		ReadTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
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

// RunServer is a convenience wrapper around [Listen] and [Serve].
// Prefer calling them separately when you need to surface bind errors before
// launching a goroutine.
func RunServer(ctx context.Context, addr string, handler http.Handler) error {
	ln, err := Listen(addr)
	if err != nil {
		return err
	}
	return Serve(ctx, ln, handler)
}
