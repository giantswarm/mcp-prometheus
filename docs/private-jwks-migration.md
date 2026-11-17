# Private-address JWKS settings migration

Use `MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS=true` or Helm `app.oauth.allowPrivateIPJWKS: true`
only when the configured Dex JWKS endpoint must resolve to private addresses.
HTTPS and certificate verification remain required. Dex discovery and token requests
already support private addresses and honor `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`.

Remove `MCP_OAUTH_ALLOW_PRIVATE_URLS` and Helm `app.oauth.allowPrivateURLs`.
The environment spelling `OAUTH_ALLOW_PRIVATE_URLS` is also rejected when enabled.
An enabled obsolete setting fails startup or chart rendering with a migration message,
including when the replacement setting is false. Obsolete false values remain accepted
temporarily and do not change the JWKS policy; invalid boolean values fail startup.

Removal of the obsolete false compatibility values is planned for v0.14.0 or
2027-02-17, whichever comes first. Until then, the Helm schema restricts the obsolete
value to false and no obsolete environment variable is rendered.
