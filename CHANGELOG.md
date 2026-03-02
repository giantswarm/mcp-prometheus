# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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

[Unreleased]: https://github.com/giantswarm/mcp-prometheus/compare/v0.0.11...HEAD
[0.0.11]: https://github.com/giantswarm/mcp-prometheus/compare/v0.0.0...v0.0.11
