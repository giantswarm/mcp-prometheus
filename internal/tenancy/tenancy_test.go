package tenancy

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

// helpers

func grafanaOrg(name string, admins, editors, viewers []string, tenantNames []string) *unstructured.Unstructured {
	toIface := func(ss []string) []interface{} {
		out := make([]interface{}, len(ss))
		for i, s := range ss {
			out[i] = s
		}
		return out
	}
	tenants := make([]interface{}, len(tenantNames))
	for i, t := range tenantNames {
		tenants[i] = map[string]interface{}{"name": t}
	}
	rbac := map[string]interface{}{}
	if len(admins) > 0 {
		rbac["admins"] = toIface(admins)
	}
	if len(editors) > 0 {
		rbac["editors"] = toIface(editors)
	}
	if len(viewers) > 0 {
		rbac["viewers"] = toIface(viewers)
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "observability.giantswarm.io/v1alpha2",
			"kind":       "GrafanaOrganization",
			"metadata":   map[string]interface{}{"name": name},
			"spec": map[string]interface{}{
				"rbac":    rbac,
				"tenants": tenants,
			},
		},
	}
}

func newTestResolver(objs ...runtime.Object) *Resolver {
	fakeClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), objs...)
	return NewResolver(fakeClient)
}

// --- cacheKey ---

func TestCacheKeyStable(t *testing.T) {
	k1 := cacheKey([]string{"b", "a", "c"})
	k2 := cacheKey([]string{"a", "b", "c"})
	if k1 != k2 {
		t.Errorf("cacheKey should sort groups: got %q and %q", k1, k2)
	}
}

func TestCacheKeyDistinct(t *testing.T) {
	k1 := cacheKey([]string{"group-a"})
	k2 := cacheKey([]string{"group-b"})
	if k1 == k2 {
		t.Errorf("different groups should produce different cache keys")
	}
}

func TestCacheKeyEmpty(t *testing.T) {
	k := cacheKey(nil)
	if k == "" {
		t.Errorf("cacheKey should not return empty string for nil input")
	}
}

// --- SelectOrgID ---

func TestSelectOrgIDNoTenants(t *testing.T) {
	_, err := SelectOrgID(nil, "")
	if err == nil {
		t.Error("expected error for empty tenant list")
	}
}

func TestSelectOrgIDSingleTenantNoOverride(t *testing.T) {
	got, err := SelectOrgID([]string{"prod-eu"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "prod-eu" {
		t.Errorf("expected %q, got %q", "prod-eu", got)
	}
}

func TestSelectOrgIDMultiTenantNoOverride(t *testing.T) {
	// Multiple tenants should be joined with "|" for Mimir fan-out.
	got, err := SelectOrgID([]string{"a", "b"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a|b" {
		t.Errorf("expected %q, got %q", "a|b", got)
	}
}

func TestSelectOrgIDValidOverride(t *testing.T) {
	got, err := SelectOrgID([]string{"a", "b", "c"}, "b")
	if err != nil {
		t.Fatal(err)
	}
	if got != "b" {
		t.Errorf("expected %q, got %q", "b", got)
	}
}

func TestSelectOrgIDInvalidOverride(t *testing.T) {
	_, err := SelectOrgID([]string{"a", "b"}, "z")
	if err == nil {
		t.Error("expected error for org_id not in allowed list")
	}
}

func TestSelectOrgIDPipeSeparatedValid(t *testing.T) {
	// Pipe-separated override where all parts are in allowed list.
	got, err := SelectOrgID([]string{"a", "b", "c"}, "a|b")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a|b" {
		t.Errorf("expected %q, got %q", "a|b", got)
	}
}

func TestSelectOrgIDPipeSeparatedInvalid(t *testing.T) {
	// One part not in allowed list should fail.
	_, err := SelectOrgID([]string{"a", "b"}, "a|z")
	if err == nil {
		t.Error("expected error when one pipe-separated part is not in allowed list")
	}
}

// --- Resolver.TenantsForGroups ---

func TestTenantsForGroupsMatch(t *testing.T) {
	org := grafanaOrg("my-org", []string{"team-ops"}, nil, nil, []string{"prod-eu", "prod-us"})
	r := newTestResolver(org)

	tenants, err := r.TenantsForGroups(context.Background(), []string{"team-ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 2 {
		t.Fatalf("expected 2 tenants, got %v", tenants)
	}
	// Result is sorted
	if tenants[0] != "prod-eu" || tenants[1] != "prod-us" {
		t.Errorf("unexpected tenants: %v", tenants)
	}
}

func TestTenantsForGroupsNoMatch(t *testing.T) {
	org := grafanaOrg("my-org", []string{"team-ops"}, nil, nil, []string{"prod-eu"})
	r := newTestResolver(org)

	tenants, err := r.TenantsForGroups(context.Background(), []string{"other-team"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 0 {
		t.Errorf("expected 0 tenants for non-matching group, got %v", tenants)
	}
}

func TestTenantsForGroupsEditorMatch(t *testing.T) {
	org := grafanaOrg("my-org", nil, []string{"team-dev"}, nil, []string{"staging"})
	r := newTestResolver(org)

	tenants, err := r.TenantsForGroups(context.Background(), []string{"team-dev"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 1 || tenants[0] != "staging" {
		t.Errorf("expected [staging], got %v", tenants)
	}
}

func TestTenantsForGroupsViewerMatch(t *testing.T) {
	org := grafanaOrg("my-org", nil, nil, []string{"team-viewer"}, []string{"prod"})
	r := newTestResolver(org)

	tenants, err := r.TenantsForGroups(context.Background(), []string{"team-viewer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 1 || tenants[0] != "prod" {
		t.Errorf("expected [prod], got %v", tenants)
	}
}

func TestTenantsForGroupsDeduplication(t *testing.T) {
	// Two orgs with overlapping tenants — result should be deduplicated.
	org1 := grafanaOrg("org1", []string{"team-ops"}, nil, nil, []string{"shared", "prod-eu"})
	org2 := grafanaOrg("org2", []string{"team-ops"}, nil, nil, []string{"shared", "prod-us"})
	r := newTestResolver(org1, org2)

	tenants, err := r.TenantsForGroups(context.Background(), []string{"team-ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 3 {
		t.Errorf("expected 3 deduplicated tenants, got %v", tenants)
	}
}

func TestTenantsForGroupsCaching(t *testing.T) {
	org := grafanaOrg("my-org", []string{"team-ops"}, nil, nil, []string{"prod-eu"})
	r := newTestResolver(org)

	tenants1, err := r.TenantsForGroups(context.Background(), []string{"team-ops"})
	if err != nil {
		t.Fatal(err)
	}
	// Second call should return cached result (same slice)
	tenants2, err := r.TenantsForGroups(context.Background(), []string{"team-ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants1) != len(tenants2) {
		t.Errorf("cached result differs from original: %v vs %v", tenants1, tenants2)
	}
}

func TestTenantsForGroupsEmptyOrg(t *testing.T) {
	// Org with no spec should be skipped gracefully.
	empty := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "observability.giantswarm.io/v1alpha2",
			"kind":       "GrafanaOrganization",
			"metadata":   map[string]interface{}{"name": "empty-org"},
		},
	}
	r := newTestResolver(empty)

	tenants, err := r.TenantsForGroups(context.Background(), []string{"any-group"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 0 {
		t.Errorf("expected 0 tenants for empty org, got %v", tenants)
	}
}

// --- NewResolver vs NewInClusterResolver ---

func TestNewResolver(t *testing.T) {
	fakeClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	r := NewResolver(fakeClient)
	if r == nil {
		t.Fatal("NewResolver returned nil")
	}
	if r.client != fakeClient {
		t.Error("NewResolver did not set client")
	}
	if r.cache == nil {
		t.Error("NewResolver did not initialise cache")
	}
}

// TestNewInClusterResolverOutsideCluster verifies that NewInClusterResolver
// returns an error when not running inside a Kubernetes cluster.
func TestNewInClusterResolverOutsideCluster(t *testing.T) {
	_, err := NewInClusterResolver()
	if err == nil {
		t.Error("expected error when not running in a cluster")
	}
}

// Verify the GVR constants are correct (regression guard).
func TestGrafanaOrgGVR(t *testing.T) {
	if grafanaOrgGVR.Group != "observability.giantswarm.io" {
		t.Errorf("unexpected group: %s", grafanaOrgGVR.Group)
	}
	if grafanaOrgGVR.Version != "v1alpha2" {
		t.Errorf("unexpected version: %s", grafanaOrgGVR.Version)
	}
	if grafanaOrgGVR.Resource != "grafanaorganizations" {
		t.Errorf("unexpected resource: %s", grafanaOrgGVR.Resource)
	}
}

// Suppress unused import (metav1 needed for ListOptions reference in resolver.go)
var _ = metav1.ListOptions{}
