// Package tenancy resolves Mimir tenant IDs for authenticated users.
//
// Two resolution modes are supported, selected via [NewResolverForMode]:
//
//   - grafana-organization ([ModeGrafanaOrganization]): reads GrafanaOrganization
//     CRDs from the Kubernetes API. Each CRD specifies which Dex groups can access
//     which Mimir tenants via spec.rbac and spec.tenants, and which types each
//     tenant carries (data, alerting). Results are cached for 60 seconds per
//     unique group set to reduce API-server load.
//
//   - static ([ModeStatic]): returns a fixed tenant list or a group→tenants mapping
//     from configuration. No Kubernetes access is required. Two sub-modes exist:
//     all-users (same tenants for every authenticated user) and group-mapping
//     (tenants collected per group and unioned). Every tenant counts as alerting.
//
// All modes implement [server.TenancyResolver] so they are interchangeable at the
// call site: TenantsForGroups for the querier, which federates tenants, and
// AlertingTenantsForGroups for the ruler and the Alertmanager, which serve one
// tenant per request. [SelectOrgID] and [SelectOrgIDs] validate or auto-inject
// the Mimir X-Scope-OrgID values and are shared by all modes.
package tenancy
