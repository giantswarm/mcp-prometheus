package prometheus

import (
	"context"
	"sync"
	"testing"

	"github.com/giantswarm/mcp-toolkit/tooldesc"
	"github.com/giantswarm/mcp-toolkit/tooldesc/tooldesctest"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

// toolsDocHeader heads the generated docs/tools.md.
const toolsDocHeader = "# Tools\n\nGenerated from the registered tools by `TestToolsDocIsCurrent`; " +
	"regenerate with `make tools-doc`."

// docsServer is an MCP server with every tool registered, built once.
var docsServer = sync.OnceValues(func() (*mcpserver.MCPServer, error) {
	sc, err := server.NewServerContext(context.Background(),
		server.WithPrometheusConfig(server.PrometheusConfig{URL: "http://localhost:9090"}),
		server.WithSlogLogger(discardLogger()),
	)
	if err != nil {
		return nil, err
	}
	s := mcpserver.NewMCPServer("test", "1.0.0", mcpserver.WithToolCapabilities(true))
	if err := RegisterPrometheusTools(s, sc); err != nil {
		return nil, err
	}
	return s, nil
})

func newToolServer(t *testing.T) *mcpserver.MCPServer {
	t.Helper()
	s, err := docsServer()
	if err != nil {
		t.Fatalf("build the tool server: %v", err)
	}
	return s
}

// registeredTool is the named tool as the server lists it.
func registeredTool(t *testing.T, name string) mcp.Tool {
	t.Helper()
	st := newToolServer(t).GetTool(name)
	if st == nil {
		t.Fatalf("no tool %s", name)
	}
	return st.Tool
}

func TestToolDescriptionBudgets(t *testing.T) {
	if err := tooldesc.CheckServer(newToolServer(t), tooldesc.DefaultBudget); err != nil {
		t.Error(err)
	}
}

func TestToolsDocIsCurrent(t *testing.T) {
	tooldesctest.AssertServerDoc(t, newToolServer(t), toolsDocHeader, "../../../docs/tools.md")
}

func TestToolAnnotationsReadOnly(t *testing.T) {
	for name, st := range newToolServer(t).ListTools() {
		a := st.Tool.Annotations
		if a.ReadOnlyHint == nil || !*a.ReadOnlyHint {
			t.Errorf("%s: readOnlyHint is not true", name)
		}
		if a.DestructiveHint == nil || *a.DestructiveHint {
			t.Errorf("%s: destructiveHint is not false", name)
		}
		if a.IdempotentHint == nil || !*a.IdempotentHint {
			t.Errorf("%s: idempotentHint is not true", name)
		}
	}
}
