package tenancy

import (
	"context"
	"testing"
)

const (
	tenantProdEU    = "prod-eu"
	tenantProdUS    = "prod-us"
	tenantStaging   = "staging"
	groupTeamOps    = "team-ops"
	groupTeamDev    = "team-dev"
	overwrittenMark = "OVERWRITTEN"
)

func TestStaticResolver_AllUsersMode_ReturnsFixedList(t *testing.T) {
	r := NewStaticResolver([]string{tenantProdEU, tenantStaging}, nil)
	tenants, err := r.TenantsForGroups(context.Background(), []string{"any-group"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 2 || tenants[0] != tenantProdEU || tenants[1] != tenantStaging {
		t.Errorf("got %v, want [prod-eu staging]", tenants)
	}
}

func TestStaticResolver_AllUsersMode_NilGroups(t *testing.T) {
	r := NewStaticResolver([]string{tenantProdEU}, nil)
	tenants, err := r.TenantsForGroups(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 1 || tenants[0] != tenantProdEU {
		t.Errorf("got %v, want [prod-eu]", tenants)
	}
}

func TestStaticResolver_AllUsersMode_EmptyGroupMap(t *testing.T) {
	r := NewStaticResolver([]string{tenantProdEU}, map[string][]string{})
	tenants, err := r.TenantsForGroups(context.Background(), []string{groupTeamOps})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 1 || tenants[0] != tenantProdEU {
		t.Errorf("got %v, want [prod-eu]", tenants)
	}
}

func TestStaticResolver_AllUsersMode_MutationIsolation(t *testing.T) {
	input := []string{tenantProdEU, tenantStaging}
	r := NewStaticResolver(input, nil)

	// Mutate the input slice after construction.
	input[0] = overwrittenMark

	tenants, _ := r.TenantsForGroups(context.Background(), nil)
	if tenants[0] != tenantProdEU {
		t.Errorf("resolver was affected by external mutation: got %v", tenants)
	}
}

func TestStaticResolver_AllUsersMode_ReturnCopyIsolation(t *testing.T) {
	r := NewStaticResolver([]string{tenantProdEU}, nil)
	tenants, _ := r.TenantsForGroups(context.Background(), nil)

	// Mutate the returned slice.
	tenants[0] = overwrittenMark

	tenants2, _ := r.TenantsForGroups(context.Background(), nil)
	if tenants2[0] != tenantProdEU {
		t.Errorf("resolver state was mutated via returned slice: got %v", tenants2)
	}
}

func TestStaticResolver_GroupMapping_SingleMatch(t *testing.T) {
	r := NewStaticResolver(nil, map[string][]string{
		groupTeamOps: {tenantProdEU, tenantProdUS},
		groupTeamDev: {tenantStaging},
	})
	tenants, err := r.TenantsForGroups(context.Background(), []string{groupTeamOps})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 2 || tenants[0] != tenantProdEU || tenants[1] != tenantProdUS {
		t.Errorf("got %v, want [prod-eu prod-us]", tenants)
	}
}

func TestStaticResolver_GroupMapping_MultipleGroups_UnionAndDedup(t *testing.T) {
	r := NewStaticResolver(nil, map[string][]string{
		groupTeamOps: {tenantProdEU, tenantProdUS},
		groupTeamDev: {tenantProdEU, tenantStaging}, // prod-eu shared
	})
	tenants, err := r.TenantsForGroups(context.Background(), []string{groupTeamOps, groupTeamDev})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect sorted, deduplicated union.
	want := []string{tenantProdEU, tenantProdUS, tenantStaging}
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
		groupTeamOps: {tenantProdEU},
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
		groupTeamOps: {tenantProdEU},
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
		groupTeamOps: {tenantProdEU},
	})
	tenants, err := r.TenantsForGroups(context.Background(), []string{groupTeamOps})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tenants) != 1 || tenants[0] != tenantProdEU {
		t.Errorf("got %v, want [prod-eu]", tenants)
	}
}

func TestStaticResolver_GroupMapping_MutationIsolation(t *testing.T) {
	groupMap := map[string][]string{
		groupTeamOps: {tenantProdEU},
	}
	r := NewStaticResolver(nil, groupMap)

	// Mutate the map and its values after construction.
	groupMap[groupTeamOps][0] = overwrittenMark
	groupMap["team-new"] = []string{tenantStaging}

	tenants, _ := r.TenantsForGroups(context.Background(), []string{groupTeamOps})
	if len(tenants) != 1 || tenants[0] != tenantProdEU {
		t.Errorf("resolver was affected by external mutation: got %v", tenants)
	}
	tenants2, _ := r.TenantsForGroups(context.Background(), []string{"team-new"})
	if len(tenants2) != 0 {
		t.Errorf("resolver picked up externally added group: got %v", tenants2)
	}
}
