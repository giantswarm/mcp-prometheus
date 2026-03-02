package tenancy

import "context"

type contextKey string

const orgIDKey contextKey = "tenancy_org_id"

// WithOrgID returns a copy of ctx with the given Mimir tenant ID stored inside.
// The tenant ID is later read by the Prometheus client to set X-Scope-OrgID.
func WithOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgIDKey, orgID)
}

// OrgIDFromContext retrieves the Mimir tenant ID injected by [WithOrgID].
// Returns the empty string when no tenant ID has been stored.
func OrgIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(orgIDKey).(string); ok {
		return v
	}
	return ""
}
