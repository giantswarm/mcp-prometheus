package prometheus

import (
	"context"
	"testing"

	"github.com/giantswarm/mcp-toolkit/tooldesc"
	"github.com/giantswarm/mcp-toolkit/tooldesc/tooldesctest"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/mcp-prometheus/internal/server"
)

// toolsDocHeader heads the generated docs/tools.md.
const toolsDocHeader = "# Tools\n\nGenerated from the registered tools by `TestToolsDocIsCurrent`; " +
	"regenerate with `TOOLS_DOC_UPDATE=1 go test ./internal/tools/prometheus/ -run TestToolsDocIsCurrent`."

// newToolServer is an MCP server with every tool registered.
func newToolServer(t *testing.T) *mcpserver.MCPServer {
	t.Helper()
	sc, err := server.NewServerContext(context.Background(),
		server.WithPrometheusConfig(server.PrometheusConfig{URL: "http://localhost:9090"}),
		server.WithSlogLogger(discardLogger()),
	)
	if err != nil {
		t.Fatalf("NewServerContext: %v", err)
	}
	t.Cleanup(func() { _ = sc.Shutdown() })

	s := mcpserver.NewMCPServer("test", "1.0.0", mcpserver.WithToolCapabilities(true))
	if err := RegisterPrometheusTools(s, sc); err != nil {
		t.Fatalf("RegisterPrometheusTools: %v", err)
	}
	return s
}

func TestToolDescriptions(t *testing.T) {
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
