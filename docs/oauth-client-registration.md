# OAuth client registration

CIMD (Client ID Metadata Documents) is enabled whenever OAuth is enabled.
The application explicitly enables the existing mcp-oauth CIMD implementation.
Discovery advertises `client_id_metadata_document_supported: true` while enabled.
Unfamiliar clients require the library's default browser-bound consent before
the upstream login. No trusted-client or consent bypass is configured by default.

Prefer pre-registration for known clients and CIMD for unfamiliar clients.
[MCP 2026-07-28 deprecates Dynamic Client Registration (DCR)](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration#dynamic-client-registration).
DCR remains a compatibility fallback: its configuration, registration endpoint
and conditional discovery advertisement continue to work. Enabling CIMD does
not open unauthenticated DCR. Keep the existing registration access controls.
The [specification's deprecated registry](https://modelcontextprotocol.io/specification/2026-07-28/deprecated)
allows removal in the first specification revision released on or after
2027-07-28. This is not a scheduled mcp-oauth removal date.

## Network and trust

Metadata is fetched over HTTPS with system CA verification, bounded responses,
timeouts and the library's DNS and connection-time SSRF checks. Private,
loopback and link-local metadata destinations remain blocked. An identity
provider's CA or JWKS private-host allowance does not grant access to private
CIMD hosts. Prefer pre-registered metadata for internal clients; any private
redirect exception must name the exact host and retain TLS verification.

The chart's existing egress rules permit public HTTPS metadata fetches.
Installation-level policies must also allow DNS, public TCP/443 and any
configured proxy, alongside the identity-provider and backend peers.
The container includes system CA roots. For an HTTPS inspection proxy, extend
system trust with the proxy CA rather than disabling certificate verification.
Verify a real public metadata fetch from the deployed pod after changing egress,
proxy or trust settings.
