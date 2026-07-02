# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

* Team ownership: team ownership aligned to the canonical `io.giantswarm.application.team: atlas` annotation (was key `application.giantswarm.io/team`, value `planeteers`).

### Added

* `allowPrivateIPJWKSHosts` on `OAUTH_TRUSTED_ISSUERS` entries (and the `app.oauth.trustedIssuers` Helm values): a per-host allowlist for issuers whose JWKS URL resolves to a private/loopback IP, keeping SSRF protection on every other host. Prefer it over the blanket `allowPrivateIPJWKS` flag.

## [0.1.80](https://github.com/giantswarm/mcp-prometheus/compare/v0.1.79...v0.1.80) (2026-06-03)


### Fixed

* **deps:** update module github.com/giantswarm/mcp-oauth to v0.2.186 ([#181](https://github.com/giantswarm/mcp-prometheus/issues/181)) ([c90fad5](https://github.com/giantswarm/mcp-prometheus/commit/c90fad57f2115c22162ec4844a08c77e45be788f))
* **deps:** update module github.com/prometheus/common to v0.68.1 ([#182](https://github.com/giantswarm/mcp-prometheus/issues/182)) ([5c54845](https://github.com/giantswarm/mcp-prometheus/commit/5c548459023a1e0dc16994f54112ad975f8ed177))


### Changed

* **deps:** update dependency architect to v9.0.2 ([#176](https://github.com/giantswarm/mcp-prometheus/issues/176)) ([e8a787a](https://github.com/giantswarm/mcp-prometheus/commit/e8a787a730b1c6f23896260123aa517b3d149e11))
* **deps:** update golang docker tag to v1.26.4 ([#179](https://github.com/giantswarm/mcp-prometheus/issues/179)) ([e0d6dce](https://github.com/giantswarm/mcp-prometheus/commit/e0d6dce2ccdf38a804b6ff73b6b32716ddf7a21b))
* **main:** release 0.1.77 ([#184](https://github.com/giantswarm/mcp-prometheus/issues/184)) ([f556ea5](https://github.com/giantswarm/mcp-prometheus/commit/f556ea57502afb18959d7c0d7a0a85487eb8a10e))
* **main:** release 0.1.78 ([#185](https://github.com/giantswarm/mcp-prometheus/issues/185)) ([f3805dd](https://github.com/giantswarm/mcp-prometheus/commit/f3805dd14a84a6dccf524eb7d780c9c3e7f109df))
* **main:** release 0.1.79 ([#186](https://github.com/giantswarm/mcp-prometheus/issues/186)) ([9e22184](https://github.com/giantswarm/mcp-prometheus/commit/9e22184127f046e337d13c7cd4ae88898c12d4d9))

## [0.1.79](https://github.com/giantswarm/mcp-prometheus/compare/v0.1.78...v0.1.79) (2026-06-03)


### Fixed

* **deps:** update module github.com/prometheus/common to v0.68.1 ([#182](https://github.com/giantswarm/mcp-prometheus/issues/182)) ([5c54845](https://github.com/giantswarm/mcp-prometheus/commit/5c548459023a1e0dc16994f54112ad975f8ed177))


### Changed

* **deps:** update dependency architect to v9.0.2 ([#176](https://github.com/giantswarm/mcp-prometheus/issues/176)) ([e8a787a](https://github.com/giantswarm/mcp-prometheus/commit/e8a787a730b1c6f23896260123aa517b3d149e11))
* **deps:** update golang docker tag to v1.26.4 ([#179](https://github.com/giantswarm/mcp-prometheus/issues/179)) ([e0d6dce](https://github.com/giantswarm/mcp-prometheus/commit/e0d6dce2ccdf38a804b6ff73b6b32716ddf7a21b))
* **main:** release 0.1.76 ([#183](https://github.com/giantswarm/mcp-prometheus/issues/183)) ([8b3c00a](https://github.com/giantswarm/mcp-prometheus/commit/8b3c00a043011b662a41d8bdc13acfadc6da4c33))
* **main:** release 0.1.77 ([#184](https://github.com/giantswarm/mcp-prometheus/issues/184)) ([f556ea5](https://github.com/giantswarm/mcp-prometheus/commit/f556ea57502afb18959d7c0d7a0a85487eb8a10e))
* **main:** release 0.1.78 ([#185](https://github.com/giantswarm/mcp-prometheus/issues/185)) ([f3805dd](https://github.com/giantswarm/mcp-prometheus/commit/f3805dd14a84a6dccf524eb7d780c9c3e7f109df))

## [0.1.78](https://github.com/giantswarm/mcp-prometheus/compare/v0.1.77...v0.1.78) (2026-06-03)


### Fixed

* **deps:** update module github.com/prometheus/common to v0.68.1 ([#182](https://github.com/giantswarm/mcp-prometheus/issues/182)) ([5c54845](https://github.com/giantswarm/mcp-prometheus/commit/5c548459023a1e0dc16994f54112ad975f8ed177))


### Changed

* **deps:** update actions/checkout action to v6.0.3 ([#178](https://github.com/giantswarm/mcp-prometheus/issues/178)) ([23f1332](https://github.com/giantswarm/mcp-prometheus/commit/23f13320d519cb69a1056f67abd1aaa6c63e9bf3))
* **deps:** update golang docker tag to v1.26.4 ([#179](https://github.com/giantswarm/mcp-prometheus/issues/179)) ([e0d6dce](https://github.com/giantswarm/mcp-prometheus/commit/e0d6dce2ccdf38a804b6ff73b6b32716ddf7a21b))
* **main:** release 0.1.76 ([#183](https://github.com/giantswarm/mcp-prometheus/issues/183)) ([8b3c00a](https://github.com/giantswarm/mcp-prometheus/commit/8b3c00a043011b662a41d8bdc13acfadc6da4c33))
* **main:** release 0.1.77 ([#184](https://github.com/giantswarm/mcp-prometheus/issues/184)) ([f556ea5](https://github.com/giantswarm/mcp-prometheus/commit/f556ea57502afb18959d7c0d7a0a85487eb8a10e))

## [0.1.77](https://github.com/giantswarm/mcp-prometheus/compare/v0.1.76...v0.1.77) (2026-06-03)


### Changed

* **deps:** update actions/checkout action to v6.0.3 ([#178](https://github.com/giantswarm/mcp-prometheus/issues/178)) ([23f1332](https://github.com/giantswarm/mcp-prometheus/commit/23f13320d519cb69a1056f67abd1aaa6c63e9bf3))
* **deps:** update golang docker tag to v1.26.4 ([#179](https://github.com/giantswarm/mcp-prometheus/issues/179)) ([e0d6dce](https://github.com/giantswarm/mcp-prometheus/commit/e0d6dce2ccdf38a804b6ff73b6b32716ddf7a21b))
* **main:** release 0.1.75 ([#180](https://github.com/giantswarm/mcp-prometheus/issues/180)) ([1fb26ab](https://github.com/giantswarm/mcp-prometheus/commit/1fb26ab3c55886aab2193c3ab5f565a56d414b16))
* **main:** release 0.1.76 ([#183](https://github.com/giantswarm/mcp-prometheus/issues/183)) ([8b3c00a](https://github.com/giantswarm/mcp-prometheus/commit/8b3c00a043011b662a41d8bdc13acfadc6da4c33))

## [0.1.76](https://github.com/giantswarm/mcp-prometheus/compare/v0.1.75...v0.1.76) (2026-06-03)


### Changed

* align files according to platform standards ([#175](https://github.com/giantswarm/mcp-prometheus/issues/175)) ([536d521](https://github.com/giantswarm/mcp-prometheus/commit/536d5215909a3ef6a4380b01a4894cac0e718dc0))
* **deps:** update actions/checkout action to v6.0.3 ([#178](https://github.com/giantswarm/mcp-prometheus/issues/178)) ([23f1332](https://github.com/giantswarm/mcp-prometheus/commit/23f13320d519cb69a1056f67abd1aaa6c63e9bf3))
* **main:** release 0.1.75 ([#180](https://github.com/giantswarm/mcp-prometheus/issues/180)) ([1fb26ab](https://github.com/giantswarm/mcp-prometheus/commit/1fb26ab3c55886aab2193c3ab5f565a56d414b16))

## [0.1.75](https://github.com/giantswarm/mcp-prometheus/compare/v0.1.74...v0.1.75) (2026-06-03)


### Fixed

* **deps:** update module github.com/giantswarm/mcp-oauth to v0.2.185 ([#177](https://github.com/giantswarm/mcp-prometheus/issues/177)) ([342bf86](https://github.com/giantswarm/mcp-prometheus/commit/342bf86529f388bd9a2a563546bc5af27420ce1f))


### Changed

* align files according to platform standards ([#175](https://github.com/giantswarm/mcp-prometheus/issues/175)) ([536d521](https://github.com/giantswarm/mcp-prometheus/commit/536d5215909a3ef6a4380b01a4894cac0e718dc0))

## [Unreleased]

### Changed




- Bump `giantswarm/architect` orb to `8.2.2` and re-enable cosign keyless chart signing (`sign: false` removed from every `push-to-app-catalog*` invocation). v8.2.2 ships [architect-orb#772](https://github.com/giantswarm/architect-orb/pull/772) which upgrades the `app-build-suite` executor image from `1.8.0-circleci` to `1.8.1-circleci` -- the new image includes the `cosign` binary that v8.2.0's chart signing defaults require. Closes [architect-orb#769](https://github.com/giantswarm/architect-orb/issues/769).
- Disable cosign keyless chart signing on the `push-to-app-catalog*` jobs (`sign: false`). The architect orb's `push-to-app-catalog` defaults `sign` to `true` since v8.2.0 and shells out to `cosign`, but this repo uses `executor: app-build-suite` (so the `app_build_suite` Python CLI is available to package the chart with metadata) and the `app-build-suite` image doesn't ship `cosign`. Without this opt-out, every chart push fails on the `Mint Sigstore OIDC token` step with `cosign: command not found`. To be removed once architect-orb makes `cosign-prepare` resilient to a missing binary (or ships cosign in the `app-build-suite` executor).
- Bump `giantswarm/architect` orb to `8.2.1` to pick up [architect-orb#767](https://github.com/giantswarm/architect-orb/pull/767): `image-login-to-registries` is now POSIX-portable, unblocking `architect/sync-china-registry` (the gsoci -> Aliyun mirror via the in-China `giantswarm/galaxy-runner`). The v8.1.0 refactor accidentally introduced bash-only `${!var}` indirect expansion in the shared login command, which BusyBox `/bin/sh` (used by the regctl executor) rejected with `bad substitution` -- so no Aliyun mirror has been happening since the migration to `split-china-push: true`. v8.2.x also enables cosign keyless signing, SLSA provenance, and SBOM attestations by default for public images and charts.
- Replace the `push-to-gsoci-release` + `push-to-all-registries-release` workaround pair with a single `push-to-registries-release` job using `split-china-push: true` and a companion `sync-china-registry` job. The cross-Pacific `docker buildx` push to the Aliyun mirror is gone; the in-China `giantswarm/galaxy-runner` self-hosted CircleCI runner does `regctl image copy` (gsoci -> Aliyun) via the Singapore geo-replica. Chart catalog publish still does not gate on Aliyun.
- Bump `giantswarm/architect` orb to `8.1.0` and migrate all three image pushes from the deprecated `push-to-registries-multiarch` job to `push-to-registries` with `multiarch: true`. Picks up the v8.1.0 QEMU/binfmt auto-registration, hardened buildx bootstrap, and standard OCI image labels.

### Added

- Added MCP tool annotations (`readOnlyHint`, `openWorldHint`) to all tools to help clients and users assess tool behavior ([#36355](https://github.com/giantswarm/giantswarm/issues/36355)).
- Enable JSON-Schema input validation for every tool (per [SEP-1303](https://modelcontextprotocol.io/seps/1303-input-validation-errors-as-tool-execution-errors)). Calls with unknown property names, wrong types, or missing required fields now return a structured tool execution error instead of being silently dropped, so the model can self-correct ([#36458](https://github.com/giantswarm/giantswarm/issues/36458)).

## [0.1.0] - 2026-03-30

### Removed

- Removed `list_metrics` tool. Agents don't use it effectively and fall back to `execute_query` successfully.

### Changed

- Unified all logging on `log/slog`. Removed the custom `server.Logger` interface, `simpleLogger`, and `noopLogger` in favour of a single `*slog.Logger` threaded through the application. All startup and lifecycle messages are now structured and written to stderr. The `--debug` flag controls the slog level.

### Fixed

- Graceful shutdown timeout was `30` (30 nanoseconds) instead of `30 * time.Second`; the SSE and Streamable-HTTP servers now wait up to 30 seconds for in-flight requests to complete before exiting.
- Use icon URL that is accessible for applications.
- OAuth encryption key format changed from hex to base64 encoding for consistency with `mcp-kubernetes`

### Added

- OAuth 2.1 Authorization Server via [`giantswarm/mcp-oauth`](https://github.com/giantswarm/mcp-oauth):
  - PKCE, token rotation, and dynamic client registration (RFC 7591, `--allow-public-registration`)
  - Backed by Dex OIDC; supports in-memory or Valkey/Redis token storage (`OAUTH_STORAGE`)
  - `--enable-oauth` flag — SSE and streamable-http transports only
  - Environment variables: `MCP_OAUTH_ISSUER`, `MCP_OAUTH_ENCRYPTION_KEY` (AES-256-GCM base64),
    `DEX_ISSUER_URL`, `DEX_CLIENT_ID`, `DEX_CLIENT_SECRET`, `DEX_REDIRECT_URL`
  - OAuth endpoints at `/oauth/{authorize,callback,token,register,revoke}`
- Automatic Mimir multi-tenancy via **GrafanaOrganization** CRDs (`observability.giantswarm.io/v1alpha2`):
  - Resolves Mimir tenant IDs from the authenticated user's Dex groups matched against
    `spec.rbac.{admins,editors,viewers}`
  - Auto-injects `X-Scope-OrgID` on every Prometheus/Mimir request
  - Single tenant: auto-inject; multiple tenants: Mimir fan-out via `|` pipe syntax
  - 60-second TTL cache per unique group set to reduce Kubernetes API server load
- Helm chart additions for OAuth and tenancy:
  - `app.oauth.*` — OAuth configuration with `existingSecret` support, Valkey storage options,
    and `allowPublicRegistration`
  - `rbac.yaml` — ClusterRole + ClusterRoleBinding for `grafanaorganizations` (get/list/watch),
    created only when `app.oauth.enabled: true`
  - `oauth-secret.yaml` — Kubernetes Secret for `DEX_CLIENT_SECRET`, `MCP_OAUTH_ENCRYPTION_KEY`,
    and optional `VALKEY_PASSWORD`; base64/length validation in `values.schema.json`
  - `httproute.yaml` — optional Gateway API `HTTPRoute` for Envoy/Cilium gateway deployments
  - `values.schema.json` — JSON Schema validation for all new `app.oauth.*` and `gatewayAPI.*` fields
- Observability HTTP server (`--metrics-addr`, default `:9091`) exposing:
  - `GET /metrics` — Prometheus metrics in OpenMetrics format (Go runtime + process + MCP tool call counters/histograms)
  - `GET /healthz` — liveness probe (always 200 OK while the process is alive)
  - `GET /readyz` — readiness probe (200 OK after all tools are registered, 503 before)
- `mcp_prometheus_tool_calls_total{tool,status}` counter and `mcp_prometheus_tool_call_duration_seconds{tool}` histogram for every MCP tool invocation
- OpenTelemetry tracing: no-op by default; set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable OTLP HTTP export and `OTEL_SERVICE_NAME` to override the service name (default: `mcp-prometheus`)
- `ToolMiddleware` extension point in `RegisterPrometheusTools` for injecting custom cross-cutting concerns (metrics, tracing, rate-limiting, etc.)

## [0.0.11] - 2025-07-25

### Added

- TLS support for the Prometheus/Mimir client:
  - `PROMETHEUS_TLS_SKIP_VERIFY=true` — disable TLS certificate verification (not recommended for production)
  - `PROMETHEUS_TLS_CA_CERT=<path>` — path to a PEM-encoded CA certificate file for custom/private PKI
- `check_ready` tool: check whether the Prometheus or Mimir server is ready to serve traffic (`GET /-/ready`); compatible with both Prometheus and Mimir
- Initial implementation of MCP (Model Context Protocol) server for Prometheus
- Comprehensive Helm chart for Kubernetes deployment
- Docker container setup with security best practices
- CircleCI configuration for automated builds and deployments
- 18 MCP tools for Prometheus interaction:
  - Query execution tools (`execute_query`, `execute_range_query`)
  - Metrics discovery tools (`list_metrics`, `get_metric_metadata`)
  - Label and series discovery tools (`list_label_names`, `list_label_values`, `find_series`)
  - System information tools (`get_targets`, `get_build_info`, `get_runtime_info`, `get_flags`, `get_config`, `get_tsdb_stats`)
  - Alerting and rules tools (`get_alerts`, `get_alertmanagers`, `get_rules`)
  - Advanced features (`query_exemplars`, `get_targets_metadata`)
- Support for multiple transport protocols:
  - Standard I/O (stdio)
  - Server-Sent Events (SSE)
  - Streamable HTTP
- Authentication support for Prometheus:
  - Basic authentication (username/password)
  - Bearer token authentication
  - Multi-tenant configurations (Cortex/Mimir/Thanos)
- CiliumNetworkPolicy for network security
- Comprehensive documentation and usage examples

### Security

- Non-root container execution (UID 1000)
- Read-only root filesystem
- Dropped all capabilities
- No privilege escalation allowed
- Secure service account configuration
- Network policies for traffic isolation

### Infrastructure

- Helm chart following Giant Swarm standards
- Values schema validation (JSON Schema)
- Support for horizontal pod autoscaling
- Ingress configuration for external access
- ConfigMap and Secret integration for configuration
- Resource limits and requests properly configured

[Unreleased]: https://github.com/giantswarm/mcp-prometheus/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/giantswarm/mcp-prometheus/compare/v0.0.11...v0.1.0
[0.0.11]: https://github.com/giantswarm/mcp-prometheus/compare/v0.0.0...v0.0.11
