package tenancy

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const (
	grafanaOrgGroup    = "observability.giantswarm.io"
	grafanaOrgVersion  = "v1alpha2"
	grafanaOrgResource = "grafanaorganizations"

	// cacheTTL is how long resolved tenant lists are cached per group set.
	cacheTTL = 60 * time.Second
)

// The types a GrafanaOrganization tenant carries (spec.tenants[*].types).
const (
	// TenantTypeData: the tenant holds metrics, logs and traces. Mimir's
	// querier federates such tenants in one request.
	TenantTypeData = "data"
	// TenantTypeAlerting: the tenant carries rules and an Alertmanager
	// configuration. Mimir's ruler and Alertmanager serve one tenant per
	// request.
	TenantTypeAlerting = "alerting"
)

var grafanaOrgGVR = schema.GroupVersionResource{
	Group:    grafanaOrgGroup,
	Version:  grafanaOrgVersion,
	Resource: grafanaOrgResource,
}

// Tenant is one Mimir tenant of a GrafanaOrganization with the types it
// carries.
type Tenant struct {
	Name  string
	Types []string
}

// HasType reports whether the tenant carries typ. A tenant that lists no
// types carries data only, the CRD's default.
func (t Tenant) HasType(typ string) bool {
	if len(t.Types) == 0 {
		return typ == TenantTypeData
	}
	return slices.Contains(t.Types, typ)
}

// cacheEntry holds a cached tenant resolution result.
type cacheEntry struct {
	tenants  []Tenant
	cachedAt time.Time
}

// Resolver resolves Mimir tenant IDs for a user based on their group memberships
// and the GrafanaOrganization CRDs present in the cluster.
type Resolver struct {
	client dynamic.Interface
	mu     sync.Mutex
	cache  map[string]cacheEntry
	group  singleflight.Group
}

// NewInClusterResolver creates a Resolver using in-cluster service account credentials.
// Returns an error when called outside a Kubernetes cluster.
func NewInClusterResolver() (*Resolver, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("tenancy: load in-cluster config: %w", err)
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("tenancy: create dynamic client: %w", err)
	}
	return &Resolver{
		client: client,
		cache:  make(map[string]cacheEntry),
	}, nil
}

// NewResolver creates a Resolver from an existing dynamic client.
// Useful for testing or when a client is already available.
func NewResolver(client dynamic.Interface) *Resolver {
	return &Resolver{
		client: client,
		cache:  make(map[string]cacheEntry),
	}
}

// TenantsForGroups returns the names of every Mimir tenant accessible to a
// user that belongs to the given Dex groups, whatever the tenant's types: the
// set Mimir's querier federates.
//
// It lists all GrafanaOrganization CRDs, finds those whose spec.rbac fields
// contain at least one of the user's groups, and collects the spec.tenants[*].name
// values from matching organisations.
//
// The result is deduplicated and sorted. An empty slice means the user has no
// access to any tenant (caller should return 403).
//
// Results are cached for [cacheTTL]. Concurrent requests for the same group set
// are coalesced via singleflight so the Kubernetes API is called at most once
// per cache miss. If the Kubernetes API is unavailable and a (potentially stale)
// cached entry exists, the stale entry is returned rather than failing every
// in-flight tool call.
func (r *Resolver) TenantsForGroups(ctx context.Context, groups []string) ([]string, error) {
	tenants, err := r.tenantsForGroups(ctx, groups)
	if err != nil {
		return nil, err
	}
	return names(tenants, func(Tenant) bool { return true }), nil
}

// AlertingTenantsForGroups returns the names of the tenants of
// [Resolver.TenantsForGroups] that carry alerting ([TenantTypeAlerting]): the
// ones the ruler and the Alertmanager may be read for. A tenant of type data
// only never joins them.
func (r *Resolver) AlertingTenantsForGroups(ctx context.Context, groups []string) ([]string, error) {
	tenants, err := r.tenantsForGroups(ctx, groups)
	if err != nil {
		return nil, err
	}
	return names(tenants, func(t Tenant) bool { return t.HasType(TenantTypeAlerting) }), nil
}

// names lists, in order, the names of the tenants keep accepts.
func names(tenants []Tenant, keep func(Tenant) bool) []string {
	out := make([]string, 0, len(tenants))
	for _, t := range tenants {
		if keep(t) {
			out = append(out, t.Name)
		}
	}
	return out
}

// tenantsForGroups resolves the tenants of the groups through the cache and
// the singleflight group (see [Resolver.TenantsForGroups]).
func (r *Resolver) tenantsForGroups(ctx context.Context, groups []string) ([]Tenant, error) {
	key := cacheKey(groups)

	// Fast path: serve from cache while fresh.
	r.mu.Lock()
	if e, ok := r.cache[key]; ok && time.Since(e.cachedAt) < cacheTTL {
		r.mu.Unlock()
		return e.tenants, nil
	}
	r.mu.Unlock()

	// Slow path: deduplicate concurrent misses for the same key.
	type result struct{ tenants []Tenant }
	v, err, _ := r.group.Do(key, func() (interface{}, error) {
		tenants, err := r.resolve(ctx, groups)
		if err != nil {
			// K8s API unavailable — serve stale cache if available.
			r.mu.Lock()
			e, ok := r.cache[key]
			r.mu.Unlock()
			if ok {
				return result{tenants: e.tenants}, nil
			}
			return nil, err
		}
		r.mu.Lock()
		r.cache[key] = cacheEntry{tenants: tenants, cachedAt: time.Now()}
		r.mu.Unlock()
		return result{tenants: tenants}, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(result).tenants, nil
}

// resolve fetches all GrafanaOrganization objects and matches them against
// groups. A tenant listed by several matching organisations carries the union
// of their types.
func (r *Resolver) resolve(ctx context.Context, groups []string) ([]Tenant, error) {
	list, err := r.client.Resource(grafanaOrgGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("tenancy: list grafanaorganizations: %w", err)
	}

	groupSet := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		groupSet[g] = struct{}{}
	}

	byName := make(map[string]*Tenant)
	for i := range list.Items {
		spec, _ := list.Items[i].Object["spec"].(map[string]interface{})
		if spec == nil {
			continue
		}

		if !matchesRBAC(spec["rbac"], groupSet) {
			continue
		}

		// Collect spec.tenants[*].name with spec.tenants[*].types.
		tenants, _ := spec["tenants"].([]interface{})
		for _, raw := range tenants {
			tm, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := tm["name"].(string)
			if name == "" {
				continue
			}
			t, ok := byName[name]
			if !ok {
				t = &Tenant{Name: name}
				byName[name] = t
			}
			for _, typ := range tenantTypes(tm["types"]) {
				if !slices.Contains(t.Types, typ) {
					t.Types = append(t.Types, typ)
				}
			}
		}
	}

	result := make([]Tenant, 0, len(byName))
	for _, t := range byName {
		result = append(result, *t)
	}
	slices.SortFunc(result, func(a, b Tenant) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

// tenantTypes reads a tenant's spec.tenants[*].types. A tenant that lists
// none carries data only, the CRD's default.
func tenantTypes(raw interface{}) []string {
	list, _ := raw.([]interface{})
	types := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok && s != "" {
			types = append(types, s)
		}
	}
	if len(types) == 0 {
		return []string{TenantTypeData}
	}
	return types
}

// matchesRBAC returns true when at least one group in groupSet appears in any of
// the spec.rbac.{admins,editors,viewers} lists.
func matchesRBAC(rbacRaw interface{}, groupSet map[string]struct{}) bool {
	rbac, ok := rbacRaw.(map[string]interface{})
	if !ok {
		return false
	}
	for _, field := range []string{"admins", "editors", "viewers"} {
		if list, ok := rbac[field].([]interface{}); ok {
			for _, g := range list {
				if gs, ok := g.(string); ok {
					if _, found := groupSet[gs]; found {
						return true
					}
				}
			}
		}
	}
	return false
}

// cacheKey creates a stable string key from a sorted, deduplicated list of groups.
// json.Marshal on []string never returns an error; the blank identifier is safe.
func cacheKey(groups []string) string {
	cp := make([]string, len(groups))
	copy(cp, groups)
	sort.Strings(cp)
	b, _ := json.Marshal(cp) //nolint:errcheck // []string marshal is infallible
	return string(b)
}

// SelectOrgIDs selects the tenants a request addresses, one X-Scope-OrgID
// value each, for an API that serves a single tenant per request: Mimir's
// Alertmanager and ruler.
//
// Any explicit override — including a pipe-separated multi-tenant selector —
// is validated against the caller's tenant list so that a user cannot reach
// tenants outside their GrafanaOrganization membership.
//
// Rules:
//   - len(tenants) == 0 → error (no tenant to address).
//   - override != "" → each pipe-separated part is validated; error if any
//     part is not in tenants. The parts are returned in their order, trimmed
//     and deduplicated.
//   - override == "" → every tenant, one request each.
func SelectOrgIDs(tenants []string, override string) ([]string, error) {
	if len(tenants) == 0 {
		return nil, fmt.Errorf("tenancy: user has no access to any tenant; access denied")
	}
	if override == "" {
		return slices.Clone(tenants), nil
	}

	allowed := make(map[string]struct{}, len(tenants))
	for _, t := range tenants {
		allowed[t] = struct{}{}
	}

	selected := make([]string, 0, len(tenants))
	for _, part := range strings.Split(override, "|") {
		part = strings.TrimSpace(part)
		if _, ok := allowed[part]; !ok {
			return nil, fmt.Errorf("tenancy: org_id part %q is not one of the tenants you may address here (%s)",
				part, strings.Join(tenants, ", "))
		}
		if !slices.Contains(selected, part) {
			selected = append(selected, part)
		}
	}
	return selected, nil
}

// SelectOrgID selects the Mimir X-Scope-OrgID value for a request against an
// API that federates tenants, the querier: the tenants of [SelectOrgIDs]
// joined with a pipe character (e.g. "tenant-a|tenant-b"), which Mimir
// queries all at once.
func SelectOrgID(tenants []string, override string) (string, error) {
	selected, err := SelectOrgIDs(tenants, override)
	if err != nil {
		return "", err
	}
	return strings.Join(selected, "|"), nil
}
