# MCP Prometheus

A comprehensive [Model Context Protocol][mcp] (MCP) server for Prometheus, written in Go.

This provides complete access to your Prometheus metrics, queries, and system information through standardized MCP interfaces, allowing AI assistants to execute PromQL queries, discover metrics, explore labels, and analyze your monitoring infrastructure.

[mcp]: https://modelcontextprotocol.io

## Features

### 🔍 **Query Execution**
- [x] **Instant queries** with optional timestamp and enhanced parameters
- [x] **Range queries** with time bounds, step intervals, and performance options
- [x] **Query optimization** with timeout, limit, and stats parameters
- [x] **Result truncation** with intelligent user guidance for large datasets

### 📊 **Metrics Discovery**
- [x] **List all available metrics** with filtering and time-based selection
- [x] **Get detailed metadata** for specific metrics
- [x] **Metric exploration** with enhanced filtering options

### 🏷️ **Label & Series Discovery**
- [x] **List all label names** with filtering and limits
- [x] **Get label values** for specific labels with advanced filtering
- [x] **Find series** by label matchers with time bounds
- [x] **Advanced label matching** with complex selectors

### 🎯 **Target & System Information**
- [x] **Scrape target information** and health status
- [x] **Build information** and version details
- [x] **Runtime information** and system status
- [x] **Configuration and flags** inspection
- [x] **TSDB statistics** and cardinality information

### 🚨 **Alerting & Rules**
- [x] **Active alerts** monitoring
- [x] **AlertManager discovery** and status
- [x] **Recording and alerting rules** inspection

### 🔬 **Advanced Features**
- [x] **Exemplar queries** for trace correlation
- [x] **Target metadata** exploration
- [x] **Multi-tenant support** with dynamic organization IDs
- [x] **Dynamic client configuration** per-query

### 🔐 **Authentication & Transport**
- [x] **Multiple authentication methods** (Basic, Bearer token)
- [x] **Multi-tenant organization headers** (Cortex, Mimir, Thanos)
- [x] **Multiple transport protocols** (stdio, SSE, HTTP)
- [x] **Cross-platform binary distribution**

## Installation

### Pre-built Binaries

Download the latest binary for your platform from the [releases page](../../releases).

### From Source

```bash
git clone https://github.com/giantswarm/mcp-prometheus.git
cd mcp-prometheus
go build -o mcp-prometheus
```

## Configuration

Configure the MCP server through environment variables (all optional):

```bash
# Optional: Default Prometheus server configuration
export PROMETHEUS_URL=http://your-prometheus-server:9090

# Optional: Authentication credentials (choose one)
# For basic auth
export PROMETHEUS_USERNAME=your_username
export PROMETHEUS_PASSWORD=your_password

# For bearer token auth  
export PROMETHEUS_TOKEN=your_token

# Optional: Default organization ID for multi-tenant setups
export PROMETHEUS_ORGID=your_organization_id
```

**Dynamic Configuration**: All MCP tools support `prometheus_url` and `org_id` parameters for per-query configuration, allowing you to query multiple Prometheus instances and organizations dynamically.

## Usage

### Command Line

Start the server with stdio transport (default):

```bash
./mcp-prometheus
```

Start with HTTP transport for web-based clients:

```bash
./mcp-prometheus serve --transport sse --http-addr :8080
```

### MCP Client Configuration

Add the server configuration to your MCP client. For example, with Claude Desktop:

```json
{
  "mcpServers": {
    "prometheus": {
      "command": "/path/to/mcp-prometheus",
      "args": ["serve"],
      "env": {
        "PROMETHEUS_URL": "http://your-prometheus-server:9090",
        "PROMETHEUS_ORGID": "your-default-org"
      }
    }
  }
}
```

## Available Tools

### 🔍 Query Execution Tools

| Tool | Description | Required Parameters | Optional Parameters |
|------|-------------|-------------------|-------------------|
| `mcp_prometheus_execute_query` | Execute PromQL instant query | `query` | `prometheus_url`, `org_id`, `time`, `timeout`, `limit`, `stats`, `lookback_delta`, `unlimited` |
| `mcp_prometheus_execute_range_query` | Execute PromQL range query | `query`, `start`, `end`, `step` | `prometheus_url`, `org_id`, `timeout`, `limit`, `stats`, `lookback_delta`, `unlimited` |

### 📊 Metrics Discovery Tools

| Tool | Description | Required Parameters | Optional Parameters |
|------|-------------|-------------------|-------------------|
| `mcp_prometheus_list_metrics` | List all available metrics | - | `prometheus_url`, `org_id`, `start_time`, `end_time`, `matches` |
| `mcp_prometheus_get_metric_metadata` | Get metadata for specific metric | `metric` | `prometheus_url`, `org_id`, `limit` |

### 🏷️ Label & Series Discovery Tools

| Tool | Description | Required Parameters | Optional Parameters |
|------|-------------|-------------------|-------------------|
| `mcp_prometheus_list_label_names` | Get all available label names | - | `prometheus_url`, `org_id`, `start_time`, `end_time`, `matches`, `limit` |
| `mcp_prometheus_list_label_values` | Get values for specific label | `label` | `prometheus_url`, `org_id`, `start_time`, `end_time`, `matches`, `limit` |
| `mcp_prometheus_find_series` | Find series by label matchers | `matches` | `prometheus_url`, `org_id`, `start_time`, `end_time`, `limit` |

### 🎯 Target & System Information Tools

| Tool | Description | Required Parameters | Optional Parameters |
|------|-------------|-------------------|-------------------|
| `mcp_prometheus_get_targets` | Get scrape target information | - | `prometheus_url`, `org_id` |
| `mcp_prometheus_get_build_info` | Get Prometheus build information | - | `prometheus_url`, `org_id` |
| `mcp_prometheus_get_runtime_info` | Get Prometheus runtime information | - | `prometheus_url`, `org_id` |
| `mcp_prometheus_get_flags` | Get Prometheus runtime flags | - | `prometheus_url`, `org_id` |
| `mcp_prometheus_get_config` | Get Prometheus configuration | - | `prometheus_url`, `org_id` |
| `mcp_prometheus_get_tsdb_stats` | Get TSDB cardinality statistics | - | `prometheus_url`, `org_id`, `limit` |

### 🚨 Alerting & Rules Tools

| Tool | Description | Required Parameters | Optional Parameters |
|------|-------------|-------------------|-------------------|
| `mcp_prometheus_get_alerts` | Get active alerts | - | `prometheus_url`, `org_id` |
| `mcp_prometheus_get_alertmanagers` | Get AlertManager discovery info | - | `prometheus_url`, `org_id` |
| `mcp_prometheus_get_rules` | Get recording and alerting rules | - | `prometheus_url`, `org_id` |

### 🔬 Advanced Tools

| Tool | Description | Required Parameters | Optional Parameters |
|------|-------------|-------------------|-------------------|
| `mcp_prometheus_query_exemplars` | Query exemplars for traces | `query`, `start`, `end` | `prometheus_url`, `org_id` |
| `mcp_prometheus_get_targets_metadata` | Get metadata from specific targets | - | `prometheus_url`, `org_id`, `match_target`, `metric`, `limit` |

### 🔧 Enhanced Parameters

**Connection Parameters** (available on all tools):
- `prometheus_url`: Prometheus server URL (e.g., 'http://localhost:9090')
- `org_id`: Organization ID for multi-tenant setups (e.g., 'tenant-123')

**Query Enhancement Parameters**:
- `timeout`: Query timeout (e.g., '30s', '1m', '5m')
- `limit`: Maximum number of returned entries
- `stats`: Include query statistics ('all')
- `lookback_delta`: Query lookback delta (e.g., '5m')
- `unlimited`: Set to 'true' for unlimited output (WARNING: may impact performance)

**Time Filtering Parameters**:
- `start_time`, `end_time`: RFC3339 timestamps for filtering
- `matches`: Array of label matchers (e.g., `['{job="prometheus"}', '{__name__=~"http_.*"}']`)

## Example Usage

### Basic Query Execution

```json
{
  "query": "up",
  "prometheus_url": "http://prometheus:9090",
  "org_id": "production"
}
```

### Enhanced Query with Performance Options

```json
{
  "query": "rate(http_requests_total[5m])",
  "prometheus_url": "http://prometheus:9090", 
  "timeout": "30s",
  "limit": "100",
  "stats": "all"
}
```

### Range Query with Time Bounds

```json
{
  "query": "cpu_usage_percent",
  "start": "2025-01-27T00:00:00Z",
  "end": "2025-01-27T01:00:00Z",
  "step": "1m",
  "prometheus_url": "http://prometheus:9090"
}
```

### Label Discovery with Filtering

```json
{
  "prometheus_url": "http://prometheus:9090",
  "matches": ["up{job=\"kubernetes-nodes\"}"],
  "limit": "20"
}
```

### Series Discovery

```json
{
  "matches": ["{__name__=~\"http_.*\", job=\"api-server\"}"],
  "prometheus_url": "http://prometheus:9090",
  "start_time": "2025-01-27T00:00:00Z",
  "limit": "50"
}
```

### Multi-tenant Query

```json
{
  "query": "container_memory_usage_bytes",
  "prometheus_url": "http://cortex-gateway:8080/prometheus",
  "org_id": "team-platform"
}
```

## Query Result Optimization

The MCP server includes intelligent query result handling:

- **Automatic truncation** for results > 50k characters
- **User guidance** for query optimization when results are large
- **Performance tips** including aggregation functions and filtering
- **Unlimited output option** with performance warnings

Example truncation message:
```
⚠️  RESULT TRUNCATED: The query returned a very large result (>50k characters).

💡 To optimize your query and get less output, consider:
   • Adding more specific label filters: {app="specific-app", namespace="specific-ns"}
   • Using aggregation functions: sum(), avg(), count(), etc.
   • Using topk() or bottomk() to get only top/bottom N results

🔧 To get the full untruncated result, add "unlimited": "true" to your query parameters.
```

## Transport Options

The server supports multiple transport protocols:

### stdio (Default)
```bash
./mcp-prometheus serve --transport stdio
```

### SSE (Server-Sent Events)
```bash
./mcp-prometheus serve --transport sse --http-addr :8080
```

### Streamable HTTP
```bash
./mcp-prometheus serve --transport streamable-http --http-addr :8080
```

## Development

### Architecture

The server follows a modern, DRY architecture:

- **Modular tool registration** with parameter reuse
- **Dynamic client creation** for multi-instance support
- **Centralized error handling** and parameter validation
- **Helper functions** to reduce code repetition by 90%

### Project Structure

```
mcp-prometheus/
├── cmd/                    # CLI commands and version info
├── internal/
│   ├── server/            # Server context and configuration  
│   └── tools/prometheus/  # 18 comprehensive MCP tools
├── main.go                # Application entry point
├── go.mod                 # Go dependencies
└── README.md              # This documentation
```

### Code Quality Improvements

- **500+ lines reduced to ~85 lines** in main registration
- **Eliminated 255+ lines** of redundant boilerplate
- **Parameter helper functions** for DRY parameter definitions
- **Centralized client management** with dynamic configuration
- **Consistent error handling** across all tools

### Building & Testing

```bash
# Build the binary
go build -o mcp-prometheus

# Run tests
go test ./...
```

## Authentication

### Basic Authentication
```bash
export PROMETHEUS_USERNAME=myuser
export PROMETHEUS_PASSWORD=mypassword
```

### Bearer Token Authentication
```bash
export PROMETHEUS_TOKEN=my-bearer-token
```

### Multi-tenant Support
Perfect for Cortex, Mimir, Thanos, and other multi-tenant setups:
```bash
export PROMETHEUS_ORGID=tenant-123
```

## Error Handling

Comprehensive error handling with detailed messages:

- **Missing configuration** guidance
- **Authentication failure** details  
- **Network connectivity** troubleshooting
- **Invalid PromQL** query assistance
- **Prometheus API error** explanations
- **Dynamic client creation** error handling

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the same terms as the original Python implementation.

---

[mcp]: https://modelcontextprotocol.io 