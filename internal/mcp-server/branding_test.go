package mcp_server

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/bylonis/bylonis-mcp-server/internal/config"
	"github.com/bylonis/bylonis-mcp-server/internal/handler/tools"
	"github.com/bylonis/bylonis-mcp-server/pkg/analytics/noopanalytics"
	"github.com/bylonis/bylonis-mcp-server/pkg/instructions"
	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
	"github.com/bylonis/bylonis-mcp-server/pkg/version"
)

// brandingAllowed lists the "signoz" occurrences that may reach the assistant,
// each because it names something real that ByLonis still uses.
var brandingAllowed = []*regexp.Regexp{}

func newBrandingTestServer(t *testing.T, docsTools bool) *server.MCPServer {
	t.Helper()

	cfg := &config.Config{
		ClientCacheSize:  8,
		ClientCacheTTL:   5 * time.Minute,
		DocsToolsEnabled: docsTools,
	}
	log := logpkg.New("error")
	m := &MCPServer{logger: log, config: cfg, handler: tools.NewHandler(log, cfg), analytics: noopanalytics.New()}

	s := server.NewMCPServer("BylonisMCP", version.Version,
		server.WithToolCapabilities(false),
		server.WithInstructions(instructions.ServerInstructions),
	)
	m.registerHandlers(s)
	return s
}

func callMCP(t *testing.T, s *server.MCPServer, method string) string {
	t.Helper()

	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := json.Marshal(s.HandleMessage(context.Background(), req))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(resp, &envelope); err != nil || envelope.Error != nil {
		t.Fatalf("%s failed: %s", method, resp)
	}
	return string(resp)
}

// TestNothingTheAssistantReadsSaysSigNoz walks everything a client gets to show
// the assistant — instructions, tools, resources, resource templates and
// prompts — with the default configuration.
func TestNothingTheAssistantReadsSaysSigNoz(t *testing.T) {
	s := newBrandingTestServer(t, false)
	signoz := regexp.MustCompile(`(?i)signoz`)

	for _, method := range []string{"initialize", "tools/list", "resources/list", "resources/templates/list", "prompts/list"} {
		text := callMCP(t, s, method)
		for _, allowed := range brandingAllowed {
			text = allowed.ReplaceAllString(text, "")
		}
		for _, loc := range signoz.FindAllStringIndex(text, -1) {
			from, to := max(loc[0]-60, 0), min(loc[1]+60, len(text))
			t.Errorf("%s mentions signoz: …%s…", method, text[from:to])
		}
	}
}

func TestDocsToolsOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		tools := newBrandingTestServer(t, enabled).ListTools()
		for _, name := range []string{"bylonis_search_docs", "bylonis_fetch_doc"} {
			if _, ok := tools[name]; ok != enabled {
				t.Errorf("docs tools enabled=%v: %s registered=%v", enabled, name, ok)
			}
		}
	}
}
