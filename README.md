# MCP Prometheus (Go Implementation)

A [Model Context Protocol][mcp] (MCP) server for Prometheus, written in Go.

This provides access to your Prometheus metrics and queries through standardized MCP interfaces, allowing AI assistants to execute PromQL queries and analyze your metrics data.

[mcp]: https://modelcontextprotocol.io

## Features

- [x] Execute PromQL queries against Prometheus
  - [x] Instant queries with optional timestamp
  - [x] Range queries with time bounds and step intervals
- [x] Discover and explore metrics
  - [x] List available metrics
  - [x] Get metadata for specific metrics
  - [x] View scrape target information
- [x] Authentication support
  - [x] Basic auth from environment variables
  - [x] Bearer token auth from environment variables
  - [x] Multi-tenant organization ID headers
- [x] Multiple transport protocols
  - [x] Standard I/O (stdio) - default
  - [x] Server-Sent Events (SSE) over HTTP
  - [x] Streamable HTTP transport
- [x] Cross-platform binary distribution

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

Configure the MCP server through environment variables:

```bash
# Required: Prometheus server configuration
export PROMETHEUS_URL=http://your-prometheus-server:9090

# Optional: Authentication credentials (choose one)
# For basic auth
export PROMETHEUS_USERNAME=your_username
export PROMETHEUS_PASSWORD=your_password

# For bearer token auth  
export PROMETHEUS_TOKEN=your_token

# Optional: For multi-tenant setups like Cortex, Mimir or Thanos
export ORG_ID=your_organization_id
```

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
        "PROMETHEUS_USERNAME": "your_username",
        "PROMETHEUS_PASSWORD": "your_password"
      }
    }
  }
}
```

## Available Tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `execute_query` | Execute a PromQL instant query | `query` (required), `time` (optional) |
| `execute_range_query` | Execute a PromQL range query | `query`, `start`, `end`, `step` (all required) |
| `list_metrics` | List all available metrics | None |
| `get_metric_metadata` | Get metadata for a specific metric | `metric` (required) |
| `get_targets` | Get information about scrape targets | None |

### Example Tool Usage

#### Execute an instant query
```json
{
  "query": "up",
  "time": "2023-01-01T00:00:00Z"
}
```

#### Execute a range query
```json
{
  "query": "rate(http_requests_total[5m])",
  "start": "2023-01-01T00:00:00Z",
  "end": "2023-01-01T01:00:00Z", 
  "step": "1m"
}
```

#### Get metric metadata
```json
{
  "metric": "http_requests_total"
}
```

## Transport Options

The server supports multiple transport protocols:

### stdio (Default)
Standard input/output - suitable for MCP clients that spawn the server as a subprocess.

```bash
./mcp-prometheus serve --transport stdio
```

### SSE (Server-Sent Events)
HTTP-based transport using Server-Sent Events for real-time communication.

```bash
./mcp-prometheus serve --transport sse --http-addr :8080
```

Access endpoints:
- SSE: `http://localhost:8080/sse`
- Messages: `http://localhost:8080/message`

### Streamable HTTP
HTTP transport with streamable request/response handling.

```bash
./mcp-prometheus serve --transport streamable-http --http-addr :8080
```

Access endpoint: `http://localhost:8080/mcp`

## Development

### Requirements

- Go 1.24.4 or later
- Access to a Prometheus server for testing

### Building

```bash
go build -o mcp-prometheus
```

### Testing

```bash
go test ./...
```

### Project Structure

```
mcp-prometheus/
├── cmd/                    # CLI commands
│   ├── root.go            # Root command definition
│   ├── serve.go           # Server command implementation
│   └── version.go         # Version command
├── internal/
│   ├── server/            # Server infrastructure
│   │   ├── context.go     # Server context and configuration
│   │   └── doc.go         # Package documentation
│   └── tools/
│       └── prometheus/    # Prometheus MCP tools
│           ├── client.go  # Prometheus HTTP client
│           ├── tools.go   # Tool registration and handlers
│           └── doc.go     # Package documentation
├── main.go                # Application entry point
├── go.mod                 # Go module definition
└── README.md              # This file
```

### Architecture

The server follows a modular architecture:

- **cmd/**: Command-line interface using Cobra
- **internal/server/**: Core server infrastructure with context management
- **internal/tools/prometheus/**: Prometheus MCP tools
- **Transport Layer**: Pluggable transport protocols (stdio, SSE, HTTP)

### Adding New Features

1. Extend the Prometheus client in `internal/tools/prometheus/client.go`
2. Add new tool definitions in `internal/tools/prometheus/tools.go`
3. Register tools with the MCP server
4. Update documentation

## Authentication

The server supports multiple authentication methods:

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
For Prometheus setups with tenant isolation (Cortex, Mimir, Thanos):
```bash
export ORG_ID=tenant-123
```

## Error Handling

The server provides detailed error messages for common issues:

- Missing required configuration (PROMETHEUS_URL)
- Authentication failures
- Network connectivity issues
- Invalid PromQL queries
- Prometheus API errors

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