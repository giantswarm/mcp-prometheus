# Private-address JWKS settings migration

`MCP_OAUTH_ALLOW_PRIVATE_IP_JWKS` and Helm `app.oauth.allowPrivateIPJWKS` have no effect:
the configured Dex issuer host may resolve to a private address without them. HTTPS and
certificate verification remain required. Dex discovery and token requests honor
`HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`. Setting the key to `true` logs a warning at
startup; remove it.

`MCP_OAUTH_ALLOW_PRIVATE_URLS`, `OAUTH_ALLOW_PRIVATE_URLS` and Helm
`app.oauth.allowPrivateURLs` are deprecated aliases of the same setting and equally have
no effect. Startup logs a deprecation warning naming the variable. Invalid boolean values
fail startup.

v0.14.0 refuses the aliases. Remove them before upgrading.
