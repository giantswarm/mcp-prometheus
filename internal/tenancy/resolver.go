package tenancy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"golang.org/x/sync/singleflight"
)

const (
	grafanaOrgGroup    = "observability.giantswarm.io"
	grafanaOrgVersion  = "v1alpha2"
	grafanaOrgResource = "grafanaorganizations"

	// cacheTTL is how long resolved tenant lists are cached per group set.
	cacheTTL = 60 * time.Second
)

var grafanaOrgGVR = schema.GroupVersionResource{
	Group:    grafanaOrgGroup,
	Version:  grafanaOrgVersion,
	Resource: grafanaOrgResource,
}

// cacheEntry holds a cached tenant resolution result.
type cacheEntry struct {
	tenants  []string
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

// TenantsForGroups returns the Mimir tenant IDs accessible to a user that
// belongs to the given Dex groups.
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
	key := cacheKey(groups)

	// Fast path: serve from cache while fresh.
	r.mu.Lock()
	if e, ok := r.cache[key]; ok && time.Since(e.cachedAt) < cacheTTL {
		r.mu.Unlock()
		return e.tenants, nil
	}
	r.mu.Unlock()

	// Slow path: deduplicate concurrent misses for the same key.
	type result struct{ tenants []string }
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

// resolve fetches all GrafanaOrganization objects and matches them against groups.
func (r *Resolver) resolve(ctx context.Context, groups []string) ([]string, error) {
	list, err := r.client.Resource(grafanaOrgGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("tenancy: list grafanaorganizations: %w", err)
	}

	groupSet := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		groupSet[g] = struct{}{}
	}

	seen := make(map[string]struct{})
	for i := range list.Items {
		spec, _ := list.Items[i].Object["spec"].(map[string]interface{})
		if spec == nil {
			continue
		}

		if !matchesRBAC(spec["rbac"], groupSet) {
			continue
		}

		// Collect tenant names from spec.tenants[*].name
		if tenants, ok := spec["tenants"].([]interface{}); ok {
			for _, t := range tenants {
				if tm, ok := t.(map[string]interface{}); ok {
					if name, ok := tm["name"].(string); ok && name != "" {
						seen[name] = struct{}{}
					}
				}
			}
		}
	}

	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
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

// SelectOrgID selects the Mimir X-Scope-OrgID value for a request.
//
// Mimir supports querying multiple tenants simultaneously by joining their
// names with a pipe character (e.g. "tenant-a|tenant-b").  This function
// validates any explicit override — including pipe-separated multi-tenant
// selectors — against the caller's allowed tenant list so that a user cannot
// reach tenants outside their GrafanaOrganization membership.
//
// Rules:
//   - len(tenants) == 0 → error (no GrafanaOrganization membership).
//   - override != "" → each pipe-separated part is validated; error if any
//     part is not in the allowed list.
//   - override == "" and len(tenants) == 1 → auto-inject the single tenant.
//   - override == "" and len(tenants) > 1 → auto-inject all allowed tenants
//     joined with "|" (Mimir will query all of them).
func SelectOrgID(tenants []string, override string) (string, error) {
	if len(tenants) == 0 {
		return "", fmt.Errorf("tenancy: user has no access to any tenant; access denied")
	}

	allowed := make(map[string]struct{}, len(tenants))
	for _, t := range tenants {
		allowed[t] = struct{}{}
	}

	if override != "" {
		// Support Mimir's pipe-separated multi-tenant selector.
		parts := strings.Split(override, "|")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if _, ok := allowed[part]; !ok {
				return "", fmt.Errorf("tenancy: org_id part %q is not in the list of tenants you have access to (%s)",
					part, strings.Join(tenants, ", "))
			}
		}
		return override, nil
	}

	// No explicit override: auto-inject all allowed tenants (Mimir fan-out).
	return strings.Join(tenants, "|"), nil
}
