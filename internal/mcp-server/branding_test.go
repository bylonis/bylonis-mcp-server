package mcp_server

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func newBrandingTestServer(t *testing.T) *server.MCPServer {
	t.Helper()

	cfg := &config.Config{
		ClientCacheSize: 8,
		ClientCacheTTL:  5 * time.Minute,
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
	s := newBrandingTestServer(t)
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

// TestNoUpstreamHeaderOrEnvNames fails if the server code reads or sends an
// upstream name again: the SIGNOZ-API-KEY / X-SigNoz-* headers, the SIGNOZ_*
// variables or the signoz_url OAuth field were removed in v1.0. Tests may
// still use them to prove they are rejected.
func TestNoUpstreamHeaderOrEnvNames(t *testing.T) {
	legacy := regexp.MustCompile(`(?i)"[^"\n]*(signoz-api-key|x-signoz-|signoz_url|signoz_[a-z])[^"\n]*"`)
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "tools" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".html") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range legacy.FindAllString(string(data), -1) {
			t.Errorf("%s uses an upstream name: %s", path, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
