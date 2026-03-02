package tenancy

import (
	"context"
	"testing"
)

func TestStaticResolver_AllUsersMode_ReturnsFixedList(t *testing.T) {
	r := NewStaticResolver([]string{"prod-eu", "staging"}, nil)
	tenants, err := r.TenantsForGroups(context.Background(), []string{"any-group"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 2 || tenants[0] != "prod-eu" || tenants[1] != "staging" {
		t.Errorf("got %v, want [prod-eu staging]", tenants)
	}
}

func TestStaticResolver_AllUsersMode_NilGroups(t *testing.T) {
	r := NewStaticResolver([]string{"prod-eu"}, nil)
	tenants, err := r.TenantsForGroups(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 1 || tenants[0] != "prod-eu" {
		t.Errorf("got %v, want [prod-eu]", tenants)
	}
}

func TestStaticResolver_AllUsersMode_EmptyGroupMap(t *testing.T) {
	r := NewStaticResolver([]string{"prod-eu"}, map[string][]string{})
	tenants, err := r.TenantsForGroups(context.Background(), []string{"team-ops"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 1 || tenants[0] != "prod-eu" {
		t.Errorf("got %v, want [prod-eu]", tenants)
	}
}

func TestStaticResolver_AllUsersMode_MutationIsolation(t *testing.T) {
	input := []string{"prod-eu", "staging"}
	r := NewStaticResolver(input, nil)

	// Mutate the input slice after construction.
	input[0] = "OVERWRITTEN"

	tenants, _ := r.TenantsForGroups(context.Background(), nil)
	if tenants[0] != "prod-eu" {
		t.Errorf("resolver was affected by external mutation: got %v", tenants)
	}
}

func TestStaticResolver_AllUsersMode_ReturnCopyIsolation(t *testing.T) {
	r := NewStaticResolver([]string{"prod-eu"}, nil)
	tenants, _ := r.TenantsForGroups(context.Background(), nil)

	// Mutate the returned slice.
	tenants[0] = "OVERWRITTEN"

	tenants2, _ := r.TenantsForGroups(context.Background(), nil)
	if tenants2[0] != "prod-eu" {
		t.Errorf("resolver state was mutated via returned slice: got %v", tenants2)
	}
}

func TestStaticResolver_GroupMapping_SingleMatch(t *testing.T) {
	r := NewStaticResolver(nil, map[string][]string{
		"team-ops": {"prod-eu", "prod-us"},
		"team-dev": {"staging"},
	})
	tenants, err := r.TenantsForGroups(context.Background(), []string{"team-ops"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 2 || tenants[0] != "prod-eu" || tenants[1] != "prod-us" {
		t.Errorf("got %v, want [prod-eu prod-us]", tenants)
	}
}

func TestStaticResolver_GroupMapping_MultipleGroups_UnionAndDedup(t *testing.T) {
	r := NewStaticResolver(nil, map[string][]string{
		"team-ops": {"prod-eu", "prod-us"},
		"team-dev": {"prod-eu", "staging"}, // prod-eu shared
	})
	tenants, err := r.TenantsForGroups(context.Background(), []string{"team-ops", "team-dev"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect sorted, deduplicated union.
	want := []string{"prod-eu", "prod-us", "staging"}
	if len(tenants) != len(want) {
		t.Fatalf("got %v, want %v", tenants, want)
	}
	for i, v := range want {
		if tenants[i] != v {
			t.Errorf("tenants[%d] = %q, want %q", i, tenants[i], v)
		}
	}
}

func TestStaticResolver_GroupMapping_NoMatch(t *testing.T) {
	r := NewStaticResolver(nil, map[string][]string{
		"team-ops": {"prod-eu"},
	})
	tenants, err := r.TenantsForGroups(context.Background(), []string{"team-unknown"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 0 {
		t.Errorf("got %v, want empty slice", tenants)
	}
}

func TestStaticResolver_GroupMapping_NilGroups(t *testing.T) {
	r := NewStaticResolver(nil, map[string][]string{
		"team-ops": {"prod-eu"},
	})
	tenants, err := r.TenantsForGroups(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 0 {
		t.Errorf("got %v, want empty slice", tenants)
	}
}

func TestStaticResolver_GroupMapping_TakesPrecedenceOverAllTenants(t *testing.T) {
	// When groupMap is non-empty, allTenants must be ignored.
	r := NewStaticResolver([]string{"should-be-ignored"}, map[string][]string{
		"team-ops": {"prod-eu"},
	})
	tenants, err := r.TenantsForGroups(context.Background(), []string{"team-ops"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 1 || tenants[0] != "prod-eu" {
		t.Errorf("got %v, want [prod-eu]", tenants)
	}
}

func TestStaticResolver_GroupMapping_MutationIsolation(t *testing.T) {
	groupMap := map[string][]string{
		"team-ops": {"prod-eu"},
	}
	r := NewStaticResolver(nil, groupMap)

	// Mutate the map and its values after construction.
	groupMap["team-ops"][0] = "OVERWRITTEN"
	groupMap["team-new"] = []string{"staging"}

	tenants, _ := r.TenantsForGroups(context.Background(), []string{"team-ops"})
	if len(tenants) != 1 || tenants[0] != "prod-eu" {
		t.Errorf("resolver was affected by external mutation: got %v", tenants)
	}
	tenants2, _ := r.TenantsForGroups(context.Background(), []string{"team-new"})
	if len(tenants2) != 0 {
		t.Errorf("resolver picked up externally added group: got %v", tenants2)
	}
}
