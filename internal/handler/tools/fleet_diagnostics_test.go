package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
)

const fleetDiagnosticDG2 = `{"id":"dg2","name":"compute-n07","nodeId":"n7","status":"ready","fromHost":true,"note":"sin métricas","error":"",
"createdAt":"2026-11-02T21:00:00Z","createdBy":"admin@bylonis.ai","expiresAt":"2026-11-09T21:00:00Z","size":2048,
"sectionInfo":[{"name":"agent","source":"server","size":20,"truncated":false},{"name":"supervisor_log","source":"host","size":40,"truncated":false}],
"sections":[{"name":"agent","source":"server","content":"{\"status\": \"connected\"}","truncated":false},
{"name":"supervisor_log","source":"host","content":"old line\nold line\nerror connecting: 401 Unauthorized","truncated":true},
{"name":"connectivity","source":"host","content":"ByLonis (https://lumina.bylonis.ai): 200 en 0.2 s","truncated":false}]}`

func TestListFleetDiagnostics(t *testing.T) {
	mock, calls := fleetAPI(t, map[string]string{
		"/api/v1/inventory/nodes":   `{"nodes":[{"id":"n7","hostname":"compute-n07"}],"total":1}`,
		"/api/v1/fleet/diagnostics": `[` + fleetDiagnosticDG2 + `]`,
	})
	h := newTestHandler(mock)
	result, err := h.handleListFleetDiagnostics(testCtx(), makeToolRequest("bylonis_list_fleet_diagnostics", map[string]any{"hostname": "compute-n07"}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.EqualValues(t, 1, out["total"])
	item := out["diagnostics"].([]any)[0].(map[string]any)
	assert.Equal(t, "dg2", item["id"])
	assert.Equal(t, []any{"agent", "supervisor_log"}, item["sections"])
	assert.NotContains(t, item, "error", "an empty error is left out")
	assert.Contains(t, *calls, "/api/v1/fleet/diagnostics?nodeId=n7")

	mock, calls = fleetAPI(t, map[string]string{"/api/v1/fleet/diagnostics": `[]`})
	h = newTestHandler(mock)
	result, err = h.handleListFleetDiagnostics(testCtx(), makeToolRequest("bylonis_list_fleet_diagnostics", map[string]any{"agentId": "a9"}))
	require.NoError(t, err)
	assert.EqualValues(t, 0, resultJSON(t, result)["total"])
	assert.Equal(t, []string{"/api/v1/fleet/diagnostics?agentId=a9"}, *calls)
}

func TestGetFleetDiagnostic(t *testing.T) {
	mock, calls := fleetAPI(t, map[string]string{
		"/api/v1/inventory/nodes":       `{"nodes":[{"id":"n7","hostname":"compute-n07"}],"total":1}`,
		"/api/v1/fleet/diagnostics":     `[{"id":"dg2"},{"id":"dg1"}]`,
		"/api/v1/fleet/diagnostics/dg2": fleetDiagnosticDG2,
	})
	h := newTestHandler(mock)

	// The newest of the node, only some sections, cut: a log keeps its end.
	result, err := h.handleGetFleetDiagnostic(testCtx(), makeToolRequest("bylonis_get_fleet_diagnostic", map[string]any{
		"hostname": "compute-n07", "sections": "supervisor_log, connectivity", "maxChars": "30",
	}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.Equal(t, "dg2", out["id"])
	assert.Equal(t, []any{"agent", "supervisor_log", "connectivity"}, out["availableSections"])
	sections := out["sections"].([]any)
	require.Len(t, sections, 2)
	log := sections[0].(map[string]any)
	assert.Equal(t, "supervisor_log", log["name"])
	assert.Equal(t, "error connecting: 401 Unauthorized"[4:], log["content"], "the last 30 characters")
	assert.Equal(t, true, log["truncated"])
	assert.EqualValues(t, len("old line\nold line\nerror connecting: 401 Unauthorized")-30, log["omittedChars"])
	connectivity := sections[1].(map[string]any)
	assert.Equal(t, "ByLonis (https://lumina.bylonis.ai): 200 en 0.2 s"[:30], connectivity["content"], "the start of the rest")
	assert.Contains(t, *calls, "/api/v1/fleet/diagnostics/dg2?")

	// By id, everything.
	result, err = h.handleGetFleetDiagnostic(testCtx(), makeToolRequest("bylonis_get_fleet_diagnostic", map[string]any{"diagnosticId": "dg2"}))
	require.NoError(t, err)
	assert.Len(t, resultJSON(t, result)["sections"], 3)
}

func TestFleetDiagnosticErrors(t *testing.T) {
	mock, _ := fleetAPI(t, map[string]string{
		"/api/v1/inventory/nodes":   `{"nodes":[{"id":"n7","hostname":"compute-n07"}],"total":1}`,
		"/api/v1/fleet/diagnostics": `[]`,
	})
	h := newTestHandler(mock)
	result, err := h.handleGetFleetDiagnostic(testCtx(), makeToolRequest("bylonis_get_fleet_diagnostic", map[string]any{"hostname": "compute-n07"}))
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, toolText(result), "tab Diagnóstico", "no diagnostic: a person asks for one")

	denied := &bylonisclient.MockClient{GetFleetFn: func(_ context.Context, path string, _ url.Values) (json.RawMessage, error) {
		return nil, errors.New("unexpected status 403: you need the permission diagnose")
	}}
	h = newTestHandler(denied)
	result, err = h.handleGetFleetDiagnostic(testCtx(), makeToolRequest("bylonis_get_fleet_diagnostic", map[string]any{"diagnosticId": "dg2"}))
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, toolText(result), "Inventario → Permisos")
}

func toolText(result *mcp.CallToolResult) string {
	text, _ := result.Content[0].(mcp.TextContent)
	return strings.TrimSpace(text.Text)
}

func TestCutDiagnosticSection(t *testing.T) {
	cut, omitted := cutDiagnosticSection("collector_log", "ñandú y más", 4)
	assert.Equal(t, "más", cut[len(cut)-4:])
	assert.Equal(t, 7, omitted)
	cut, omitted = cutDiagnosticSection("base_config", "ñandú y más", 5)
	assert.Equal(t, "ñandú", cut)
	assert.Equal(t, 6, omitted)
	cut, omitted = cutDiagnosticSection("base_config", "corto", 10)
	assert.Equal(t, "corto", cut)
	assert.Zero(t, omitted)
}
