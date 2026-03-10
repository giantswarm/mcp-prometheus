package tenancy

import (
	"testing"
)

func TestNewResolverForMode_Static_AllUsers(t *testing.T) {
	r, err := NewResolverForMode(ModeStatic, []string{"prod-eu"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*StaticResolver); !ok {
		t.Errorf("got %T, want *StaticResolver", r)
	}
}

func TestNewResolverForMode_Static_GroupMap(t *testing.T) {
	r, err := NewResolverForMode(ModeStatic, nil, map[string][]string{"team-ops": {"prod-eu"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.(*StaticResolver); !ok {
		t.Errorf("got %T, want *StaticResolver", r)
	}
}

func TestNewResolverForMode_GrafanaOrganization_ErrorOutsideCluster(t *testing.T) {
	_, err := NewResolverForMode(ModeGrafanaOrganization, nil, nil)
	if err == nil {
		t.Fatal("expected error outside a Kubernetes cluster, got nil")
	}
}

func TestNewResolverForMode_UnknownMode(t *testing.T) {
	_, err := NewResolverForMode(Mode("invalid-mode"), nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown mode, got nil")
	}
}

func TestNewResolverForMode_Static_EmptyConfig(t *testing.T) {
	// Static mode with no tenants and no group map must fail at construction
	// time rather than silently denying every authenticated request at runtime.
	_, err := NewResolverForMode(ModeStatic, nil, nil)
	if err == nil {
		t.Fatal("expected error for static mode with no tenants or groups, got nil")
	}
	_, err = NewResolverForMode(ModeStatic, []string{}, map[string][]string{})
	if err == nil {
		t.Fatal("expected error for static mode with empty tenants and empty groups, got nil")
	}
}
