# Backend destinations

`PROMETHEUS_URL` and `ALERTMANAGER_URL` configure independent service endpoints.
Alertmanager shares the configured Prometheus authentication settings.

`MCP_ALLOW_BACKEND_URL_OVERRIDES` defaults to `true`. Set it to `false` to reject
caller-supplied destinations different from the corresponding configured endpoint.
An equivalent URL is accepted: hostname case, default ports and a trailing slash
are normalized. Invalid boolean configuration fails startup.

With overrides enabled, a different destination receives no configured bearer or
basic credentials, default tenant, custom CA or TLS verification exemption.
Explicit `org_id` selection still follows the configured tenancy authorization.
Changing the base path on the same host also selects a different backend.
URL userinfo, query strings, fragments and ambiguous or traversing base paths are
rejected. API query parameters continue to work normally.

API credentials are confined to the selected service base path. Readiness checks
also permit GET requests to the same origin's `/-/ready` and `/ready` endpoints.
Automatic redirects are disabled for API and readiness requests; configure the
final endpoint instead.

For a management cluster querying local Mimir, configure the two endpoints and
set `MCP_ALLOW_BACKEND_URL_OVERRIDES=false`. Keep tenant authorization enabled.
This restricts destinations even when Mimir does not require backend credentials.

Dynamic endpoints remain subject to existing outbound URL validation. It blocks
non-HTTP schemes and literal link-local addresses, but does not provide DNS-aware
SSRF protection. Use network egress policy to restrict reachable destinations.
