// Package tenancy resolves Mimir tenant IDs for authenticated users by reading
// GrafanaOrganization CRDs from the Kubernetes API.
//
// Each GrafanaOrganization specifies which Dex groups can access which Mimir
// tenants via its spec.rbac and spec.tenants fields.  The [Resolver] lists all
// GrafanaOrganization objects, intersects the user's JWT groups with the RBAC
// fields, and returns the matching tenant names to be forwarded as the
// X-Scope-OrgID header on every Mimir request.
//
// Results are cached for 60 seconds per unique group set to reduce API-server
// load.  A no-op resolver is returned when tenancy is disabled so the rest of
// the code does not need to handle nil checks.
package tenancy
