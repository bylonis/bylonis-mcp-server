package mcp_server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bylonis/bylonis-mcp-server/internal/config"
	"github.com/bylonis/bylonis-mcp-server/internal/handler/tools"
	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
	otelpkg "github.com/bylonis/bylonis-mcp-server/pkg/otel"
	"github.com/bylonis/bylonis-mcp-server/pkg/util"
	"github.com/bylonis/bylonis-mcp-server/pkg/version"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestToolCallsRequireAuth(t *testing.T) {
	handler := newHTTPHandler(t, logpkg.New("error"), httpTestConfig())

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bylonis_list_alerts","arguments":{}}}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusUnauthorized, resp.Code)
	require.Contains(t, resp.Body.String(), "Authorization or BYLONIS-API-KEY header required")
}

func TestReadinessAndHealthEndpoints(t *testing.T) {
	handler := newHTTPHandler(t, logpkg.New("error"), httpTestConfig())
	for _, path := range []string{"/readyz", "/healthz"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			require.Equal(t, http.StatusOK, rr.Code)
			require.Equal(t, "ok", rr.Body.String())
		})
	}
}

func TestLivenessEndpointDoesNotRequireReadiness(t *testing.T) {
	handler := newHTTPHandler(t, logpkg.New("error"), httpTestConfig())

	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "ok", rr.Body.String())
}

// TestStatelessTransportIssuesNoSessionID is the transport-level regression for
// WithStateLess(true): a POST initialize must not return an Mcp-Session-Id, and a
// follow-up request carrying no session id must still be served (not rejected as a
// missing/invalid session). Auth is satisfied via env credentials on cfg so the
// requests reach the MCP layer.
func TestStatelessTransportIssuesNoSessionID(t *testing.T) {
	cfg := httpTestConfig()
	cfg.URL = "https://example.bylonis.test"
	cfg.APIKey = "test-key"
	handler := newHTTPHandler(t, logpkg.New("error"), cfg)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2025-06-18")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	// POST initialize — stateless server must NOT issue a session id.
	initRR := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"itest","version":"1"}}}`)
	require.Equal(t, http.StatusOK, initRR.Code, "initialize body: %s", initRR.Body.String())
	require.Empty(t, initRR.Header().Get(server.HeaderKeySessionID), "stateless server must not issue an Mcp-Session-Id")

	// POST a follow-up request with NO session id — must be served, not rejected
	// as a missing/invalid/terminated session (HTTP 400/404).
	listRR := post(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, listRR.Code, "tools/list without a session id should succeed; body: %s", listRR.Body.String())
}

func TestE2EAuthFailureTelemetry(t *testing.T) {
	traceExporter := tracetest.NewInMemoryExporter()
	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExporter))
	prevTracerProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(traceProvider)
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTracerProvider)
		require.NoError(t, traceProvider.Shutdown(context.Background()))
	})

	var logBuf lockedBuffer
	cfg := httpTestConfig()
	cfg.OAuthEnabled = true
	cfg.OAuthIssuerURL = "https://mcp.example.com"
	handler := newHTTPHandler(t, newBufferedLogger(&logBuf, slog.LevelDebug), cfg)
	testSrv := httptest.NewServer(handler)
	t.Cleanup(testSrv.Close)

	req, err := http.NewRequest(http.MethodPost, testSrv.URL+"/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-code/2.1.133 (cli)")
	req.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.2")
	req.Header.Set(util.HeaderMCPSessionID, "mcp-session-e2e")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	records := parseJSONLogLines(t, &logBuf)
	require.NotEmpty(t, records)
	rec := records[len(records)-1]
	require.Equal(t, "No API key found in headers or environment", rec["msg"])
	require.Equal(t, authFailureMissingCredential, rec["mcp.auth.failure_reason"])
	require.Equal(t, authModeNone, rec["mcp.auth.mode"])
	require.Equal(t, "198.51.100.9", rec["client.address"])
	require.Equal(t, "claude-code/2.1.133 (cli)", rec["user_agent.original"])
	require.Equal(t, "mcp-session-e2e", rec["mcp.session.id"])

	var httpAttrs []attribute.KeyValue
	for _, span := range traceExporter.GetSpans() {
		if span.Name == "HTTP POST /mcp" {
			httpAttrs = span.Attributes
			break
		}
	}
	require.NotNil(t, httpAttrs, "expected otelhttp root span")
	for key, want := range map[attribute.Key]string{
		"mcp.auth.failure_reason": authFailureMissingCredential,
		"mcp.auth.mode":           authModeNone,
		"client.address":          "198.51.100.9",
		"user_agent.original":     "claude-code/2.1.133 (cli)",
		otelpkg.MCPSessionIDKey:   "mcp-session-e2e",
	} {
		got, ok := spanAttrValue(httpAttrs, key)
		require.True(t, ok, "missing span attr %s", key)
		require.Equal(t, want, got.AsString(), "span attr %s", key)
	}
}

func httpTestConfig() *config.Config {
	return &config.Config{
		TransportMode:   "http",
		Port:            "0",
		ClientCacheSize: 8,
		ClientCacheTTL:  time.Minute,
	}
}

// newHTTPHandler builds the HTTP handler of a server with every tool,
// resource and prompt registered.
func newHTTPHandler(t *testing.T, logger *slog.Logger, cfg *config.Config) http.Handler {
	t.Helper()
	h := tools.NewHandler(logger, cfg)
	m := NewMCPServer(logger, h, cfg, nil, nil)
	s := server.NewMCPServer("BylonisMCP", version.Version, server.WithToolCapabilities(false), server.WithRecovery())
	m.registerHandlers(s)
	return m.buildHTTP(s).Handler
}
