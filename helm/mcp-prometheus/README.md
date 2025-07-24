# mcp-prometheus

A comprehensive Model Context Protocol (MCP) server for Prometheus, written in Go.

This Helm chart deploys mcp-prometheus to provide complete access to your Prometheus metrics, queries, and system information through standardized MCP interfaces, allowing AI assistants to execute PromQL queries, discover metrics, explore labels, and analyze your monitoring infrastructure.

## Prerequisites

- Kubernetes 1.25+
- Helm 3.8+
- Access to a Prometheus server for the MCP server to connect to

## Installation

### Install from Giant Swarm Catalog

```bash
# Add the Giant Swarm catalog
helm repo add giantswarm-catalog https://giantswarm.github.io/giantswarm-catalog/
helm repo update

# Install the chart
helm install mcp-prometheus giantswarm-catalog/mcp-prometheus \
  --namespace mcp-prometheus \
  --create-namespace
```

### Install from Source

```bash
# Clone the repository
git clone https://github.com/giantswarm/mcp-prometheus.git
cd mcp-prometheus

# Install the chart
helm install mcp-prometheus ./helm/mcp-prometheus \
  --namespace mcp-prometheus \
  --create-namespace
```

## Configuration

### Basic Configuration

Configure the MCP server through Helm values:

```yaml
app:
  env:
    - name: PROMETHEUS_URL
      value: "http://prometheus-server:9090"
    - name: PROMETHEUS_ORGID
      value: "default"
```

### Authentication Configuration

#### Basic Authentication

```yaml
app:
  env:
    - name: PROMETHEUS_URL
      value: "http://prometheus-server:9090"
    - name: PROMETHEUS_USERNAME
      valueFrom:
        secretKeyRef:
          name: prometheus-auth
          key: username
    - name: PROMETHEUS_PASSWORD
      valueFrom:
        secretKeyRef:
          name: prometheus-auth
          key: password
```

#### Bearer Token Authentication

```yaml
app:
  env:
    - name: PROMETHEUS_URL
      value: "http://prometheus-server:9090"
    - name: PROMETHEUS_TOKEN
      valueFrom:
        secretKeyRef:
          name: prometheus-auth
          key: token
```

### Multi-tenant Setup (Cortex/Mimir/Thanos)

```yaml
app:
  env:
    - name: PROMETHEUS_URL
      value: "http://cortex-gateway:8080/prometheus"
    - name: PROMETHEUS_ORGID
      value: "team-platform"
    - name: PROMETHEUS_TOKEN
      valueFrom:
        secretKeyRef:
          name: cortex-auth
          key: token
```

### Transport Configuration

The MCP server supports multiple transport protocols:

#### Streamable HTTP (Recommended for Kubernetes)

```yaml
app:
  server:
    transport: "streamable-http"
    httpAddr: ":8080"
    httpEndpoint: "/mcp"

ingress:
  enabled: true
  hosts:
    - host: mcp-prometheus.example.com
      paths:
        - path: /mcp
          pathType: Prefix
```

#### Server-Sent Events (SSE)

```yaml
app:
  server:
    transport: "sse"
    httpAddr: ":8080"
    sseEndpoint: "/sse"
    messageEndpoint: "/message"
```

#### Standard I/O (for direct pod access)

```yaml
app:
  server:
    transport: "stdio"
```

## Values Reference

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |
| `image.registry` | Container image registry | `gsoci.azurecr.io` |
| `image.repository` | Container image repository | `giantswarm/mcp-prometheus` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `image.tag` | Image tag (defaults to chart appVersion) | `""` |

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.automount` | Auto-mount service account token | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `serviceAccount.name` | Service account name | `""` |

### Security

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podSecurityContext.runAsUser` | User ID to run containers | `1000` |
| `podSecurityContext.runAsGroup` | Group ID to run containers | `1000` |
| `podSecurityContext.runAsNonRoot` | Run as non-root user | `true` |
| `podSecurityContext.fsGroup` | Filesystem group ID | `1000` |
| `securityContext.readOnlyRootFilesystem` | Read-only root filesystem | `true` |
| `securityContext.allowPrivilegeEscalation` | Allow privilege escalation | `false` |
| `securityContext.runAsNonRoot` | Run as non-root user | `true` |
| `securityContext.capabilities.drop` | Dropped capabilities | `["ALL"]` |

### Service & Ingress

| Parameter | Description | Default |
|-----------|-------------|---------|
| `service.type` | Kubernetes service type | `ClusterIP` |
| `service.port` | Service port | `8080` |
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.className` | Ingress class name | `""` |
| `ingress.hosts` | Ingress hosts configuration | See values.yaml |

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `512Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |

### Health Probes

| Parameter | Description | Default |
|-----------|-------------|---------|
| `livenessProbe.initialDelaySeconds` | Liveness probe initial delay | `10` |
| `livenessProbe.periodSeconds` | Liveness probe period | `30` |
| `livenessProbe.timeoutSeconds` | Liveness probe timeout | `5` |
| `livenessProbe.failureThreshold` | Liveness probe failure threshold | `3` |
| `readinessProbe.initialDelaySeconds` | Readiness probe initial delay | `5` |
| `readinessProbe.periodSeconds` | Readiness probe period | `10` |
| `readinessProbe.timeoutSeconds` | Readiness probe timeout | `5` |
| `readinessProbe.failureThreshold` | Readiness probe failure threshold | `3` |

**Note:** The chart includes intelligent MCP-specific health probes that test actual MCP functionality:
- **Liveness Probe**: Verifies the MCP server responds to JSON-RPC calls (for HTTP transports) or SSE streams (for SSE transport)
- **Readiness Probe**: Ensures the MCP server can successfully list tools and handle requests
- **stdio transport**: Health probes are automatically disabled for stdio transport mode

### Application Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `app.server.transport` | Transport protocol (stdio/sse/streamable-http) | `streamable-http` |
| `app.server.httpAddr` | HTTP server address | `:8080` |
| `app.server.httpEndpoint` | HTTP endpoint path | `/mcp` |
| `app.server.sseEndpoint` | SSE endpoint path | `/sse` |
| `app.server.messageEndpoint` | Message endpoint path | `/message` |
| `app.server.debug` | Enable debug logging | `false` |
| `app.env` | Environment variables | `[]` |

### Autoscaling

| Parameter | Description | Default |
|-----------|-------------|---------|
| `autoscaling.enabled` | Enable horizontal pod autoscaler | `false` |
| `autoscaling.minReplicas` | Minimum replicas | `1` |
| `autoscaling.maxReplicas` | Maximum replicas | `100` |
| `autoscaling.targetCPUUtilizationPercentage` | Target CPU utilization | `80` |

## Usage Examples

### Connecting via Ingress

When using streamable-http transport with ingress:

```bash
# Port-forward for testing
kubectl port-forward svc/mcp-prometheus 8080:8080 -n mcp-prometheus

# Test the MCP endpoint
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}'
```

### Connecting via Port-Forward

```bash
# Port-forward to the service
kubectl port-forward svc/mcp-prometheus 8080:8080 -n mcp-prometheus

# Connect your MCP client to http://localhost:8080/mcp
```

### Using with MCP Clients

Example configuration for Claude Desktop:

```json
{
  "mcpServers": {
    "prometheus": {
      "command": "kubectl",
      "args": ["exec", "-n", "mcp-prometheus", "deployment/mcp-prometheus", "--", "/mcp-prometheus", "serve"],
      "env": {
        "PROMETHEUS_URL": "http://prometheus-server:9090"
      }
    }
  }
}
```

## Available MCP Tools

The deployed server provides 18 comprehensive MCP tools:

### Query Execution
- `mcp_prometheus_execute_query` - Execute PromQL instant queries
- `mcp_prometheus_execute_range_query` - Execute PromQL range queries

### Metrics Discovery
- `mcp_prometheus_list_metrics` - List available metrics
- `mcp_prometheus_get_metric_metadata` - Get metric metadata

### Label & Series Discovery
- `mcp_prometheus_list_label_names` - List label names
- `mcp_prometheus_list_label_values` - List label values
- `mcp_prometheus_find_series` - Find series by matchers

### System Information
- `mcp_prometheus_get_targets` - Get scrape targets
- `mcp_prometheus_get_build_info` - Get build information
- `mcp_prometheus_get_runtime_info` - Get runtime information
- `mcp_prometheus_get_flags` - Get runtime flags
- `mcp_prometheus_get_config` - Get configuration
- `mcp_prometheus_get_tsdb_stats` - Get TSDB statistics

### Alerting & Rules
- `mcp_prometheus_get_alerts` - Get active alerts
- `mcp_prometheus_get_alertmanagers` - Get AlertManager info
- `mcp_prometheus_get_rules` - Get rules

### Advanced Features
- `mcp_prometheus_query_exemplars` - Query exemplars
- `mcp_prometheus_get_targets_metadata` - Get target metadata

## Troubleshooting

### Common Issues

#### Connection Refused

```bash
# Check if the pod is running
kubectl get pods -n mcp-prometheus

# Check pod logs
kubectl logs -n mcp-prometheus deployment/mcp-prometheus

# Check service endpoints
kubectl get endpoints -n mcp-prometheus
```

#### Authentication Errors

```bash
# Verify Prometheus connection from the pod
kubectl exec -n mcp-prometheus deployment/mcp-prometheus -- \
  curl -v http://prometheus-server:9090/api/v1/query?query=up

# Check environment variables
kubectl exec -n mcp-prometheus deployment/mcp-prometheus -- env | grep PROMETHEUS
```

#### Transport Issues

```bash
# Test the MCP endpoint
kubectl port-forward -n mcp-prometheus svc/mcp-prometheus 8080:8080

# In another terminal
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}'
```

#### Health Probe Issues

The chart uses MCP-specific health probes. If you see probe failures:

```bash
# Check the actual MCP response
kubectl exec -n mcp-prometheus deployment/mcp-prometheus -- \
  curl -s -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}'

# For SSE transport, check the SSE endpoint
kubectl exec -n mcp-prometheus deployment/mcp-prometheus -- \
  curl -s http://localhost:8080/sse | head -c 100
```

### Debug Mode

Enable debug logging:

```yaml
app:
  server:
    debug: true
```

Then check the logs:

```bash
kubectl logs -n mcp-prometheus deployment/mcp-prometheus -f
```

## Security

### Network Policies

The chart includes a CiliumNetworkPolicy that allows:
- Ingress on port 8080 from the same namespace
- Egress to DNS servers for name resolution
- Egress to common Prometheus ports (80, 443, 8080, 9090, etc.)
- Egress to Kubernetes API server

### Security Context

The deployment runs with strict security settings:
- Non-root user (UID 1000)
- Read-only root filesystem
- All capabilities dropped
- No privilege escalation

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](../../LICENSE) file for details.

## Links

- [Project Repository](https://github.com/giantswarm/mcp-prometheus)
- [Giant Swarm Documentation](https://docs.giantswarm.io/)
- [Model Context Protocol](https://modelcontextprotocol.io/)
- [Prometheus Documentation](https://prometheus.io/docs/) 
