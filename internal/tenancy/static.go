package tenancy

import (
	"context"
	"sort"
)

// StaticResolver implements [server.TenancyResolver] using a configuration-time
// tenant list or group→tenants mapping. No Kubernetes or network access is made
// at runtime.
//
// Two sub-modes are supported:
//
//  1. All-users mode: when groupMap is nil or empty, every authenticated user
//     receives the same fixed tenant list regardless of group membership.
//
//  2. Group-mapping mode: when groupMap is non-nil and non-empty, tenant IDs are
//     collected from every group the user belongs to, deduplicated, and sorted.
//     Users with no matching groups get an empty slice, which [SelectOrgID]
//     converts to an access-denied error.
type StaticResolver struct {
	allTenants []string
	groupMap   map[string][]string
}

// NewStaticResolver returns a StaticResolver.
//
// When groupMap is non-nil and non-empty, group-mapping mode is active and
// allTenants is ignored. Otherwise every authenticated user receives allTenants.
//
// Both allTenants and each value slice in groupMap are copied to prevent the
// caller from mutating the resolver's internal state after construction.
func NewStaticResolver(allTenants []string, groupMap map[string][]string) *StaticResolver {
	r := &StaticResolver{}

	if len(groupMap) > 0 {
		r.groupMap = make(map[string][]string, len(groupMap))
		for k, v := range groupMap {
			cp := make([]string, len(v))
			copy(cp, v)
			r.groupMap[k] = cp
		}
		return r
	}

	r.allTenants = make([]string, len(allTenants))
	copy(r.allTenants, allTenants)
	return r
}

// TenantsForGroups implements [server.TenancyResolver].
//
// In all-users mode the configured tenant list is returned for every call.
// In group-mapping mode the tenant IDs for each of the user's groups are
// collected, deduplicated, and returned sorted. The context is not used.
func (r *StaticResolver) TenantsForGroups(_ context.Context, groups []string) ([]string, error) {
	if len(r.groupMap) == 0 {
		// All-users mode: return a copy so callers cannot mutate internal state.
		result := make([]string, len(r.allTenants))
		copy(result, r.allTenants)
		return result, nil
	}

	// Group-mapping mode: union tenants across all matching groups.
	seen := make(map[string]struct{})
	for _, g := range groups {
		for _, t := range r.groupMap[g] {
			seen[t] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	sort.Strings(result)
	return result, nil
}
