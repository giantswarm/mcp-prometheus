// Package tenancy resolves Mimir tenant IDs for authenticated users.
//
// Two resolution modes are supported, selected via [NewResolverForMode]:
//
//   - grafana-organization ([ModeGrafanaOrganization]): reads GrafanaOrganization
//     CRDs from the Kubernetes API. Each CRD specifies which Dex groups can access
//     which Mimir tenants via spec.rbac and spec.tenants. Results are cached for
//     60 seconds per unique group set to reduce API-server load.
//
//   - static ([ModeStatic]): returns a fixed tenant list or a group→tenants mapping
//     from configuration. No Kubernetes access is required. Two sub-modes exist:
//     all-users (same tenants for every authenticated user) and group-mapping
//     (tenants collected per group and unioned).
//
// All modes implement [server.TenancyResolver] so they are interchangeable at the
// call site. [SelectOrgID] validates or auto-injects the Mimir X-Scope-OrgID value
// and is shared by all modes.
package tenancy
