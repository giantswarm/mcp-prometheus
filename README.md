# MCP Prometheus

An [MCP (Model Context Protocol)][mcp] server for Prometheus and Mimir, written in Go.
Deployed in-cluster at Giant Swarm to give AI assistants authenticated, multi-tenant access to metrics infrastructure.

[mcp]: https://modelcontextprotocol.io

## What it does

MCP Prometheus exposes 18 read-only MCP tools that wrap the Prometheus HTTP API:
instant and range PromQL queries, metric/label/series discovery, target and runtime information, TSDB stats, alerting rules, and exemplars.

When deployed with OAuth enabled it acts as a full **OAuth 2.1 Authorization Server** (backed by Dex/OIDC or Google),
so MCP clients authenticate with the server before any tool call.
The server then resolves the authenticated user's Mimir tenant IDs and enforces them on every query.

---

## Contents

- [Architecture](#architecture)
- [Installation](#installation)
- [Configuration reference](#configuration-reference)
- [Transport modes](#transport-modes)
- [OAuth 2.1 authentication](#oauth-21-authentication)
  - [Full OAuth flow](#full-oauth-flow)
  - [SSO token forwarding (trustedAudiences)](#sso-token-forwarding-trustedaudiences)
- [Multi-tenancy](#multi-tenancy)
  - [GrafanaOrganization mode (default)](#grafanaorganization-mode-default)
  - [Static mode](#static-mode)
- [Available tools](#available-tools)
- [Kubernetes deployment (Helm)](#kubernetes-deployment-helm)
- [Development](#development)

---

## Architecture

```
MCP Client (Claude, muster, …)
        │  OAuth 2.1 + MCP over HTTP
        ▼
┌──────────────────────────────────┐
│         mcp-prometheus           │
│                                  │
│  ┌────────────┐  ┌─────────────┐ │
│  │ OAuth 2.1  │  │  MCP Tools  │ │
│  │ server     │  │  (PromQL,   │ │
│  │ (mcp-oauth)│  │   labels, …)│ │
│  └─────┬──────┘  └──────┬──────┘ │
│        │                │        │
│  ┌─────▼──────┐  ┌──────▼──────┐ │
│  │ Dex OIDC   │  │  Tenancy    │ │
│  │ provider   │  │  resolver   │ │
│  └────────────┘  └──────┬──────┘ │
└─────────────────────────┼────────┘
                          │
          ┌───────────────┴──────────────┐
          │                              │
   ┌──────▼───────┐              ┌───────▼──────┐
   │ GrafanaOrg   │              │  Prometheus  │
   │ CRDs (k8s)   │              │  / Mimir     │
   └──────────────┘              └──────────────┘
```

The server listens on two ports:
- **`:8080`** — MCP + OAuth endpoints (served to clients)
- **`:9091`** — observability: `/metrics`, `/healthz`, `/readyz` (internal only)

---

## Installation

### Pre-built binaries

Download the latest release from the [releases page](../../releases).

### From source

```bash
git clone https://github.com/giantswarm/mcp-prometheus.git
cd mcp-prometheus
go build -o mcp-prometheus ./...
```

### Kubernetes (Helm)

See [Kubernetes deployment (Helm)](#kubernetes-deployment-helm).

---

## Configuration reference

All configuration is via environment variables.

### Prometheus connection

| Variable | Default | Description |
|---|---|---|
| `PROMETHEUS_URL` | — | Prometheus/Mimir base URL |
| `PROMETHEUS_USERNAME` | — | Basic auth username |
| `PROMETHEUS_PASSWORD` | — | Basic auth password |
| `PROMETHEUS_TOKEN` | — | Bearer token |
| `PROMETHEUS_ORGID` | — | Default Mimir org/tenant ID |
| `PROMETHEUS_TLS_SKIP_VERIFY` | `false` | Skip TLS verification (dev only) |
| `PROMETHEUS_TLS_CA_CERT` | — | Path to PEM CA certificate |

### OAuth 2.1

| Variable | Default | Description |
|---|---|---|
| `MCP_OAUTH_ISSUER` | **required** | Public base URL of this server (e.g. `https://mcp.example.com`) |
| `MCP_OAUTH_ENCRYPTION_KEY` | — | 32-byte base64 AES-256-GCM key for token encryption (`openssl rand -base64 32`) |
| `MCP_OAUTH_PROVIDER` | `dex` | Identity provider: `dex` or `google` (see the provider tables below) |
| `OAUTH_REDIRECT_URL` | **required** | Callback URL registered at the provider (e.g. `https://mcp.example.com/oauth/callback`). The legacy `DEX_REDIRECT_URL` is still honoured when this is unset |
| `MCP_OAUTH_ALLOW_PUBLIC_REGISTRATION` | `false` | Allow unauthenticated dynamic client registration (dev/MCP Inspector only) |
| `MCP_OAUTH_ALLOW_PRIVATE_URLS` | `false` | Allow OIDC discovery against Dex on private/internal IPs (see [below](#allow-private-urls)); `dex` only |
| `OAUTH_TRUSTED_AUDIENCES` | — | Comma-separated client IDs trusted for SSO token forwarding |
| `OAUTH_STORAGE` | `memory` | Token storage: `memory` or `valkey` |
| `VALKEY_URL` | — | Valkey/Redis address (required when `OAUTH_STORAGE=valkey`) |
| `VALKEY_PASSWORD` | — | Valkey auth password |
| `VALKEY_TLS_ENABLED` | `false` | Enable TLS for Valkey |
| `VALKEY_KEY_PREFIX` | `mcp:` | Key namespace prefix |

### Dex OIDC provider (`MCP_OAUTH_PROVIDER=dex`)

| Variable | Default | Description |
|---|---|---|
| `DEX_ISSUER_URL` | **required** | Dex issuer URL (e.g. `https://dex.mc.example.io`) |
| `DEX_CLIENT_ID` | **required** | OAuth client ID registered in Dex |
| `DEX_CLIENT_SECRET` | **required** | OAuth client secret |
| `DEX_REDIRECT_URL` | — | Legacy alias of `OAUTH_REDIRECT_URL`; used when the latter is unset |
| `DEX_CA_FILE` | — | PEM CA file verifying TLS for Dex and JWKS endpoints (private-CA installations). Added on top of the system trust store |

### Google provider (`MCP_OAUTH_PROVIDER=google`)

| Variable | Default | Description |
|---|---|---|
| `GOOGLE_CLIENT_ID` | **required** | OAuth client ID of the Google Cloud OAuth client (`….apps.googleusercontent.com`) |
| `GOOGLE_CLIENT_SECRET` | **required** | OAuth client secret of that client |

Google's discovery, token and JWKS endpoints are public, so `DEX_CA_FILE` and
`MCP_OAUTH_ALLOW_PRIVATE_URLS` do not apply (they are ignored with a warning).
Google ID tokens carry no `groups` claim: pair this provider with
[tenancy mode `none`](#none-mode-single-tenant-prometheus) or a static all-users tenant list.

### Tenancy

| Variable | Default | Description |
|---|---|---|
| `TENANCY_STATIC_GROUP_MAP` | — | JSON map of Dex group → list of Mimir tenant IDs (static mode) |

Tenancy mode is a flag: `--tenancy-mode grafana-organization|static|none` (see [Multi-tenancy](#multi-tenancy)).

### Observability

| Variable | Default | Description |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | OTLP HTTP endpoint for tracing (no-op if unset) |
| `OTEL_SERVICE_NAME` | `mcp-prometheus` | Service name in traces |

---

## Transport modes

Start the server with `serve --transport <mode>`:

| Mode | Flag | Use case |
|---|---|---|
| `stdio` | `--transport stdio` | Local desktop clients (Claude Desktop, MCP Inspector) |
| `sse` | `--transport sse` | Legacy SSE clients |
| `streamable-http` | `--transport streamable-http` | Production in-cluster (default) |

OAuth requires `sse` or `streamable-http`.

```bash
# Local stdio (no OAuth)
./mcp-prometheus serve

# In-cluster HTTP with OAuth
./mcp-prometheus serve --transport streamable-http --http-addr :8080 --enable-oauth
```

---

## OAuth 2.1 authentication

MCP Prometheus implements OAuth 2.1 ([RFC 9700][rfc9700]) using [mcp-oauth][mcp-oauth] with the platform's
identity provider upstream: [Dex][dex] (default) or Google, selected with `MCP_OAUTH_PROVIDER`.
The flow, endpoints and token handling are identical for both; only the upstream login and the
provider-specific environment variables differ.

[rfc9700]: https://datatracker.ietf.org/doc/html/rfc9700
[mcp-oauth]: https://github.com/giantswarm/mcp-oauth
[dex]: https://dexidp.io

### Endpoints

| Path | Method | Purpose |
|---|---|---|
| `/.well-known/oauth-authorization-server` | GET | OAuth server metadata (RFC 8414) |
| `/.well-known/protected-resources` | GET | Protected resource metadata |
| `/oauth/authorize` | GET | Authorization endpoint (redirects to the identity provider) |
| `/oauth/callback` | GET | Identity provider callback (`OAUTH_REDIRECT_URL`) |
| `/oauth/token` | POST | Token exchange |
| `/oauth/register` | POST | Dynamic client registration (RFC 7591) |
| `/oauth/revoke` | POST | Token revocation |

### Full OAuth flow

```
MCP Client                mcp-prometheus              Dex OIDC
    │                           │                        │
    │  1. GET /.well-known/…    │                        │
    │──────────────────────────>│                        │
    │  server metadata          │                        │
    │<──────────────────────────│                        │
    │                           │                        │
    │  2. POST /oauth/register  │                        │
    │──────────────────────────>│                        │
    │  client_id + secret       │                        │
    │<──────────────────────────│                        │
    │                           │                        │
    │  3. GET /oauth/authorize  │                        │
    │──────────────────────────>│                        │
    │                           │  4. redirect to Dex    │
    │                           │───────────────────────>│
    │<──────────────────────────────────────────────────│
    │  browser: Dex login UI    │                        │
    │  (user authenticates)     │                        │
    │                           │                        │
    │                           │  5. callback + code    │
    │                           │<───────────────────────│
    │                           │                        │
    │  6. POST /oauth/token     │                        │
    │──────────────────────────>│                        │
    │  access_token + refresh   │                        │
    │<──────────────────────────│                        │
    │                           │                        │
    │  7. MCP tool call         │                        │
    │  Authorization: Bearer …  │                        │
    │──────────────────────────>│                        │
    │                           │  8. validate token     │
    │                           │  resolve tenants       │
    │                           │  forward to Mimir      │
```

The access token is a short-lived JWT signed by mcp-prometheus and validated on every request.
Refresh token rotation is enabled — every refresh issues a new refresh token.

### Token storage

- **`memory`** (default): in-process, lost on restart. Suitable for single-replica deployments and development.
- **`valkey`**: production-grade Redis/Valkey backend. Required for multi-replica deployments.

### Allow private URLs

When `DEX_ISSUER_URL` uses an internal DNS name that resolves to a private IP (RFC-1918 range),
the built-in SSRF protection in the OIDC discovery client would reject the connection.

Set `MCP_OAUTH_ALLOW_PRIVATE_URLS=true` to inject an HTTP client that allows private-IP connections
for OIDC discovery. TLS verification is still enforced.

```bash
MCP_OAUTH_ALLOW_PRIVATE_URLS=true
DEX_ISSUER_URL=https://dex.mc.my-cluster.example.io   # resolves to 10.x.x.x
```

In Helm: `app.oauth.allowPrivateURLs: true`

### Private CA (DEX_CA_FILE)

When Dex is served with a certificate from a private/internal CA (e.g. private management
clusters), TLS verification fails with `x509: certificate signed by unknown authority`.
Point `DEX_CA_FILE` at a PEM CA file to add that CA on top of the system trust store.
The pool verifies the Dex provider connection (OIDC discovery, code flow, userinfo),
the forwarded-ID-token JWKS endpoint, and trusted-issuer JWKS endpoints.

```bash
DEX_CA_FILE=/etc/ssl/certs/dex-ca/ca.crt
```

In Helm, reference a Secret holding the CA certificate:

```yaml
app:
  oauth:
    dexCASecret:
      name: mcp-prometheus-dex-ca
      key: ca.crt
```

### SSO token forwarding (trustedAudiences)

When users connect through an upstream MCP aggregator (e.g. [muster][muster]) that has already authenticated them,
the aggregator can forward the user's ID token from the platform IdP directly instead of starting a new OAuth flow.

[muster]: https://github.com/giantswarm/muster

Configure `OAUTH_TRUSTED_AUDIENCES` with a comma-separated list of the aggregator's OAuth client IDs:

```bash
OAUTH_TRUSTED_AUDIENCES=muster-client,my-aggregator
```

mcp-prometheus will:
1. Detect that the incoming token's audience matches a trusted client ID
2. Verify the token signature against the provider's JWKS endpoint (Dex or Google)
3. Accept the token and proceed with tenant resolution

Tokens **must** still originate from the configured provider's issuer. With the Google
provider, list the platform's Google OAuth client ID (the one muster logs users in with) here.

---

## Multi-tenancy

When OAuth is enabled, every tool call is scoped to the authenticated user's allowed Mimir tenant IDs.
The user can pass an explicit `org_id` parameter; the server validates it against their allowed tenants.
If no `org_id` is given, all allowed tenants are injected as a Mimir pipe-separated multi-tenant selector.

Three resolution modes are available, selected with `--tenancy-mode` (or `app.tenancy.mode` in Helm):
`grafana-organization` (default), `static`, and `none`.

### GrafanaOrganization mode (default)

**`--tenancy-mode grafana-organization`**

Reads `GrafanaOrganization` custom resources from the Kubernetes API.
Each CR declares which Dex groups have access (`spec.rbac`) and which Mimir tenant IDs map to it (`spec.tenants`).

```yaml
apiVersion: observability.giantswarm.io/v1alpha1
kind: GrafanaOrganization
metadata:
  name: team-platform
spec:
  rbac:
    - groupName: github-org:team-platform   # Dex group from LDAP/GitHub
  tenants:
    - prod-eu-west
    - prod-us-east
```

When a user authenticates:
1. The server reads the `groups` claim from their Dex token
2. It looks up all `GrafanaOrganization` CRs where any group in `spec.rbac` matches
3. It collects all tenant IDs from `spec.tenants` across matching CRs
4. Results are cached per group-set for 60 seconds

The Helm chart creates a `ClusterRole` + `ClusterRoleBinding` granting `get, list, watch` on `grafanaorganizations.observability.giantswarm.io` when this mode is active.

### Static mode

**`--tenancy-mode static`**

No Kubernetes API access required. Tenants are configured statically.

#### All-users: same tenants for everyone

```bash
# All authenticated users get these tenant IDs
--static-tenants=prod-eu,prod-us
```

Helm:
```yaml
app:
  tenancy:
    mode: static
    static:
      tenants: "prod-eu,prod-us"
```

#### Group mapping: per-group tenant assignment

When `TENANCY_STATIC_GROUP_MAP` is set (or `app.tenancy.static.groups` in Helm), tenant IDs are resolved per group:

```bash
TENANCY_STATIC_GROUP_MAP='{"team-ops":["prod-eu","prod-us"],"team-dev":["staging"]}'
```

Helm:
```yaml
app:
  tenancy:
    mode: static
    static:
      groups:
        team-ops:
          - prod-eu
          - prod-us
        team-dev:
          - staging
```

The user's allowed tenants are the union of all tenants from their Dex groups.

### None mode (single-tenant Prometheus)

**`--tenancy-mode none`**

For a plain, single-tenant Prometheus (no Mimir, no `X-Scope-OrgID`). OAuth still
authenticates every caller — unauthenticated requests are rejected, forwarded tokens are
validated and the identity is available for auditing — but no tenant is derived from the
identity and no tenant header is injected. An explicit `org_id` tool parameter or
`PROMETHEUS_ORGID` passes through verbatim. The chart creates no `ClusterRole` in this mode.

This is the mode to use with the Google provider (Google tokens have no `groups` claim)
and whenever mcp-prometheus sits behind [muster][muster] in front of a single Prometheus.

Helm:
```yaml
app:
  tenancy:
    mode: none
```

---

## Available tools

All tools accept optional `prometheus_url` and `org_id` parameters for per-call overrides.

### Query execution

| Tool | Description |
|---|---|
| `mcp_prometheus_execute_query` | PromQL instant query |
| `mcp_prometheus_execute_range_query` | PromQL range query with `start`, `end`, `step` |

Query tools accept: `timeout`, `limit`, `stats`, `lookback_delta`, `unlimited`.

### Metrics & discovery

| Tool | Description |
|---|---|
| `mcp_prometheus_get_metric_metadata` | Metadata for a specific metric |
| `mcp_prometheus_list_label_names` | All label names |
| `mcp_prometheus_list_label_values` | Values for a specific label |
| `mcp_prometheus_find_series` | Find series by label matchers |

### Targets & system info

| Tool | Description |
|---|---|
| `mcp_prometheus_get_targets` | Scrape target list and health |
| `mcp_prometheus_get_build_info` | Build/version information |
| `mcp_prometheus_get_runtime_info` | Runtime information |
| `mcp_prometheus_get_flags` | Runtime flags |
| `mcp_prometheus_get_config` | Prometheus configuration |
| `mcp_prometheus_get_tsdb_stats` | TSDB cardinality statistics |
| `mcp_prometheus_check_ready` | Readiness check (`/-/ready`), works with Mimir |

### Alerting & rules

| Tool | Description |
|---|---|
| `mcp_prometheus_get_alerts` | Active alerts |
| `mcp_prometheus_get_alertmanagers` | AlertManager discovery |
| `mcp_prometheus_get_rules` | Recording and alerting rules |

### Advanced

| Tool | Description |
|---|---|
| `mcp_prometheus_query_exemplars` | Exemplar queries for trace correlation |
| `mcp_prometheus_get_targets_metadata` | Per-target metric metadata |

Large query results are automatically truncated with guidance for the AI to refine its query.

---

## Kubernetes deployment (Helm)

### Minimal (no OAuth)

```yaml
app:
  env:
    - name: PROMETHEUS_URL
      value: "http://mimir-gateway.monitoring:8080/prometheus"
    - name: PROMETHEUS_ORGID
      value: "my-tenant"
```

### Production with OAuth + GrafanaOrganization tenancy

```yaml
app:
  server:
    transport: streamable-http

  oauth:
    enabled: true
    dexClientSecret: "..."        # stored in K8s Secret
    encryptionKey: "..."          # openssl rand -hex 32
    storage:
      type: valkey
      valkey:
        url: "valkey:6379"
    trustedAudiences:
      - muster-client

  tenancy:
    mode: grafana-organization

  env:
    - name: MCP_OAUTH_ISSUER
      value: "https://mcp-prometheus.mc.example.io"
    - name: DEX_ISSUER_URL
      value: "https://dex.mc.example.io"
    - name: DEX_CLIENT_ID
      value: "mcp-prometheus"
    - name: DEX_REDIRECT_URL
      value: "https://mcp-prometheus.mc.example.io/oauth/callback"
    - name: PROMETHEUS_URL
      value: "http://mimir-gateway.monitoring:8080/prometheus"
```

### Google provider + single-tenant Prometheus behind muster

The platform IdP is Google, muster forwards the user's Google ID token
(`auth.forwardToken: true`), and there are no Mimir tenants to resolve.

```yaml
app:
  oauth:
    enabled: true
    provider: google
    redirectURL: "https://mcp-prometheus.example.io/oauth/callback"
    google:
      clientID: "1234567890-abc.apps.googleusercontent.com"   # this server's OAuth client
    googleClientSecret: "..."   # or existingSecret with key GOOGLE_CLIENT_SECRET
    encryptionKey: "..."        # openssl rand -base64 32
    trustedAudiences:
      - "1234567890-platform.apps.googleusercontent.com"       # the platform client muster logs in with

  tenancy:
    mode: none

  env:
    - name: MCP_OAUTH_ISSUER
      value: "https://mcp-prometheus.example.io"
    - name: PROMETHEUS_URL
      value: "http://prometheus-operated.monitoring:9090"
```

### Production with OAuth + static group mapping

```yaml
app:
  oauth:
    enabled: true
    dexClientSecret: "..."
    encryptionKey: "..."

  tenancy:
    mode: static
    static:
      groups:
        team-ops:
          - prod-eu
          - prod-us
        team-dev:
          - staging
```

### Private Dex (internal DNS)

When `DEX_ISSUER_URL` resolves to a private IP:

```yaml
app:
  oauth:
    enabled: true
    allowPrivateURLs: true    # enables private-IP OIDC discovery
    dexClientSecret: "..."
    encryptionKey: "..."
```

### Valkey token storage (multi-replica)

```yaml
app:
  oauth:
    storage:
      type: valkey
      valkey:
        url: "valkey.default:6379"
        password: ""
        tlsEnabled: false
        keyPrefix: "mcp-prometheus:"
```

---

## Development

### Project structure

```
mcp-prometheus/
├── cmd/                      # CLI (serve, version)
├── internal/
│   ├── oauth/                # OAuth 2.1 setup (Config, NewHandler)
│   ├── server/               # ServerContext, PrometheusConfig
│   ├── tenancy/              # TenancyResolver, GrafanaOrg + static modes
│   ├── tools/prometheus/     # 18 MCP tool registrations
│   └── observability/        # /metrics, /healthz, /readyz, OTel
├── helm/mcp-prometheus/      # Helm chart
├── go.mod
└── README.md
```

### Building & testing

```bash
go build -o mcp-prometheus ./...
go test ./...
```

### Code conventions

- Every package has a `doc.go`
- 80%+ unit test coverage on new code
- Run `goimports -w . && go fmt ./...` before committing
- Files kept under 500 lines; GoDoc on all exported members
