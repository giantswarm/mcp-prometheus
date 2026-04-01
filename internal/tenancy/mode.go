package tenancy

import (
	"fmt"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

// Mode identifies which tenancy resolution strategy is active.
type Mode string

const (
	// ModeGrafanaOrganization resolves tenants via GrafanaOrganization CRDs in the
	// Kubernetes cluster. Requires in-cluster credentials and a ClusterRole granting
	// get/list/watch on grafanaorganizations.
	ModeGrafanaOrganization Mode = "grafana-organization"

	// ModeStatic resolves tenants from a fixed configuration supplied at startup.
	// No Kubernetes access is required.
	//
	// Two sub-modes are available depending on how [NewResolverForMode] is called:
	//   - All-users: a fixed slice of tenant IDs is returned for every authenticated user.
	//   - Group-mapping: a map of group name → tenant IDs is used to collect tenants
	//     for the user's JWT groups, with results deduplicated and sorted.
	ModeStatic Mode = "static"
)

// NewResolverForMode constructs the [server.TenancyResolver] for the given mode.
//
// For [ModeGrafanaOrganization] the resolver uses in-cluster Kubernetes credentials;
// staticTenants and groupMap are ignored.
//
// For [ModeStatic] a [StaticResolver] is returned. When groupMap is non-nil and
// non-empty, it is used for group-based resolution and staticTenants is ignored.
// Otherwise staticTenants is returned for every authenticated user.
//
// The function name avoids a collision with the existing [NewResolver] constructor
// that accepts a dynamic Kubernetes client.
func NewResolverForMode(mode Mode, staticTenants []string, groupMap map[string][]string) (server.TenancyResolver, error) {
	switch mode {
	case ModeGrafanaOrganization:
		return NewInClusterResolver()
	case ModeStatic:
		if len(staticTenants) == 0 && len(groupMap) == 0 {
			return nil, fmt.Errorf("tenancy: static mode requires at least one tenant or group mapping; " +
				"set MCP_STATIC_TENANTS or MCP_STATIC_GROUPS, otherwise every authenticated request will be denied")
		}
		return NewStaticResolver(staticTenants, groupMap), nil
	default:
		return nil, fmt.Errorf("tenancy: unknown mode %q (valid: grafana-organization, static)", mode)
	}
}
