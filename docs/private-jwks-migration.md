# Private-address JWKS settings migration

Use `MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS=true` or Helm `app.oauth.allowPrivateIPJWKS: true`
only when the configured Dex JWKS endpoint must resolve to private addresses.
HTTPS and certificate verification remain required. Dex discovery and token requests
already support private addresses and honor `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`.

`MCP_OAUTH_ALLOW_PRIVATE_URLS`, `OAUTH_ALLOW_PRIVATE_URLS` and Helm
`app.oauth.allowPrivateURLs` are deprecated aliases of the JWKS setting: `true` in
any of them permits private JWKS addresses, and startup logs a deprecation warning
naming the variable. Invalid boolean values fail startup.

v0.14.0 refuses the aliases. Replace them with the JWKS setting before upgrading.
