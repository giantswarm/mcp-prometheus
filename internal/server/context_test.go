package server

import (
	"context"
	"testing"
)

// stubResolver implements TenancyResolver for testing.
type stubResolver struct{}

func (s *stubResolver) TenantsForGroups(_ context.Context, _ []string) ([]string, error) {
	return []string{"tenant-a"}, nil
}

func TestWithOAuthEnabled(t *testing.T) {
	sc, err := NewServerContext(context.Background(), WithOAuthEnabled(true))
	if err != nil {
		t.Fatal(err)
	}
	if !sc.IsOAuthEnabled() {
		t.Error("expected IsOAuthEnabled() == true")
	}
}

func TestWithOAuthEnabledDefault(t *testing.T) {
	sc, err := NewServerContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sc.IsOAuthEnabled() {
		t.Error("expected IsOAuthEnabled() == false by default")
	}
}

func TestWithTenancyResolver(t *testing.T) {
	r := &stubResolver{}
	sc, err := NewServerContext(context.Background(), WithTenancyResolver(r))
	if err != nil {
		t.Fatal(err)
	}
	got := sc.TenancyResolver()
	if got != r {
		t.Errorf("TenancyResolver() returned unexpected value: %v", got)
	}
}

func TestTenancyResolverNilByDefault(t *testing.T) {
	sc, err := NewServerContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sc.TenancyResolver() != nil {
		t.Error("expected TenancyResolver() == nil by default")
	}
}

func TestOAuthAndTenancyTogether(t *testing.T) {
	r := &stubResolver{}
	sc, err := NewServerContext(context.Background(),
		WithOAuthEnabled(true),
		WithTenancyResolver(r),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.IsOAuthEnabled() {
		t.Error("expected IsOAuthEnabled() == true")
	}
	if sc.TenancyResolver() != r {
		t.Error("expected TenancyResolver() to be set")
	}
}

func TestIsDebugMode(t *testing.T) {
	sc, err := NewServerContext(context.Background(), WithDebugMode(true))
	if err != nil {
		t.Fatal(err)
	}
	if !sc.IsDebugMode() {
		t.Error("expected IsDebugMode() == true")
	}
}

func TestSetDebugMode(t *testing.T) {
	sc, err := NewServerContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sc.SetDebugMode(true)
	if !sc.IsDebugMode() {
		t.Error("expected IsDebugMode() == true after SetDebugMode(true)")
	}
	sc.SetDebugMode(false)
	if sc.IsDebugMode() {
		t.Error("expected IsDebugMode() == false after SetDebugMode(false)")
	}
}

func TestLoggerDefaultNoopLogger(t *testing.T) {
	sc, err := NewServerContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	l := sc.Logger()
	if l == nil {
		t.Fatal("Logger() returned nil")
	}
	// noopLogger methods must not panic.
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")
}

func TestWithLogger(t *testing.T) {
	custom := &noopLogger{}
	sc, err := NewServerContext(context.Background(), WithLogger(custom))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Logger() != custom {
		t.Error("expected custom logger to be set")
	}
}

func TestContextNotNil(t *testing.T) {
	sc, err := NewServerContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sc.Context() == nil {
		t.Error("Context() should not be nil")
	}
}

func TestPrometheusConfigOption(t *testing.T) {
	cfg := PrometheusConfig{URL: "http://prom:9090", OrgID: "tenant-a"}
	sc, err := NewServerContext(context.Background(), WithPrometheusConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	got := sc.PrometheusConfig()
	if got.URL != "http://prom:9090" {
		t.Errorf("unexpected URL: %s", got.URL)
	}
	if got.OrgID != "tenant-a" {
		t.Errorf("unexpected OrgID: %s", got.OrgID)
	}
}

func TestShutdown(t *testing.T) {
	sc, err := NewServerContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.Shutdown(); err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
	// Context should be cancelled after Shutdown.
	select {
	case <-sc.Context().Done():
		// expected
	default:
		t.Error("context should be cancelled after Shutdown()")
	}
	// Second Shutdown is safe (cancel is set to nil).
	if err := sc.Shutdown(); err != nil {
		t.Errorf("second Shutdown() returned error: %v", err)
	}
}
