package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
)

// fleetAPI answers the GETs of the fleet like ByLonis, by path, and records them.
func fleetAPI(t *testing.T, answers map[string]string) (*bylonisclient.MockClient, *[]string) {
	t.Helper()
	calls := []string{}
	return &bylonisclient.MockClient{
		GetFleetFn: func(_ context.Context, path string, query url.Values) (json.RawMessage, error) {
			calls = append(calls, path+"?"+query.Encode())
			answer, ok := answers[path]
			if !ok {
				return nil, errors.New("unexpected status 404")
			}
			return json.RawMessage(`{"status":"success","data":` + answer + `}`), nil
		},
	}, &calls
}

func resultJSON(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	require.False(t, result.IsError, "%v", result.Content)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	out := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	return out
}

const fleetSites = `[{"id":"s1","name":"lumina"}]`

const fleetNodeN07 = `{"id":"n7","hostname":"compute-n07","displayName":"","siteId":"s1","kind":"k8s_node","status":"down",
"statusChangedAt":"2026-10-29T08:00:00Z","lastSeenAt":"2026-10-29T07:58:00Z","primaryIp":"10.0.0.7","livenessMethod":"agent",
"agentId":"uid-7","linkState":"linked","credentialId":"c1","labels":{"site":"lumina","class":"nodo k3s"},"attributes":{"rack":"r2"}}`

func TestListFleetNodes(t *testing.T) {
	mock, calls := fleetAPI(t, map[string]string{
		"/api/v1/inventory/sites": fleetSites,
		"/api/v1/inventory/nodes": `{"nodes":[` + fleetNodeN07 + `],"total":3}`,
	})
	h := newTestHandler(mock)
	result, err := h.handleListFleetNodes(testCtx(), makeToolRequest("bylonis_list_fleet_nodes", map[string]any{
		"site": "Lumina", "status": "down", "selector": "class=nodo k3s", "limit": "1",
	}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.EqualValues(t, 3, out["total"])
	node := out["nodes"].([]any)[0].(map[string]any)
	assert.Equal(t, "compute-n07", node["hostname"])
	assert.Equal(t, "lumina", node["site"], "the site by name")
	assert.NotContains(t, node, "credentialId")
	assert.Equal(t, true, out["pagination"].(map[string]any)["hasMore"])
	assert.Contains(t, *calls, "/api/v1/inventory/nodes?limit=1&offset=0&selector=class%3Dnodo+k3s&siteId=s1&status=down")

	result, err = h.handleListFleetNodes(testCtx(), makeToolRequest("bylonis_list_fleet_nodes", map[string]any{"site": "tampa"}))
	require.NoError(t, err)
	assert.True(t, result.IsError, "an unknown site")
}

func TestGetFleetNode(t *testing.T) {
	mock, calls := fleetAPI(t, map[string]string{
		"/api/v1/inventory/sites":                 fleetSites,
		"/api/v1/inventory/nodes":                 `{"nodes":[{"id":"n70","hostname":"compute-n070"},{"id":"n7","hostname":"compute-n07"}],"total":2}`,
		"/api/v1/inventory/nodes/n7":              fleetNodeN07,
		"/api/v1/inventory/nodes/n7/labels":       `{"local":{},"remote":{"class":"nodo k3s"},"system":{"site":"lumina"},"effective":{}}`,
		"/api/v1/inventory/nodes/n7/groups":       `[{"id":"g1","name":"Nodos k3s"}]`,
		"/api/v1/fleet/agents":                    `{"agents":[{"id":"a7","instanceUid":"uid-7","status":"disconnected","disconnectedAt":"2026-10-29T07:58:00Z","tokenId":"t1"}],"total":1}`,
		"/api/v1/inventory/nodes/n7/agent_health": `{"window":"1h","step":"1m","nodeId":"n7","hostname":"compute-n07","effectiveConfig":"receivers: {}","problems":["exporter_failing"],"agents":[{"instanceId":"uid-7","series":[{"t":1}],"problems":["exporter_failing"]}]}`,
		"/api/v1/fleet/jobs":                      `{"jobs":[{"kind":"ssh_check","status":"failed","error":"no se puede conectar","params":{"x":1}}],"total":1}`,
	})
	h := newTestHandler(mock)
	result, err := h.handleGetFleetNode(testCtx(), makeToolRequest("bylonis_get_fleet_node", map[string]any{"hostname": "COMPUTE-N07"}))
	require.NoError(t, err)
	out := resultJSON(t, result)

	node := out["node"].(map[string]any)
	assert.Equal(t, "down", node["status"])
	assert.Equal(t, map[string]any{"rack": "r2"}, node["attributes"])
	assert.NotContains(t, node, "credentialId", "not even the id of the credential")
	assert.Equal(t, []any{"Nodos k3s"}, out["groups"])
	agent := out["agent"].(map[string]any)
	assert.Equal(t, "disconnected", agent["status"])
	assert.NotContains(t, agent, "tokenId")
	health := out["agentHealth"].(map[string]any)
	assert.NotContains(t, health, "effectiveConfig")
	assert.NotContains(t, health["agents"].([]any)[0].(map[string]any), "series")
	assert.Equal(t, []any{"exporter_failing"}, health["problems"])
	job := out["recentJobs"].([]any)[0].(map[string]any)
	assert.Equal(t, "no se puede conectar", job["error"])
	assert.NotContains(t, job, "params")
	assert.Contains(t, *calls, "/api/v1/inventory/nodes/n7/agent_health?window=1h")
	assert.Contains(t, *calls, "/api/v1/fleet/jobs?limit=10&node_id=n7")

	// A hostname that does not exist names the similar ones.
	result, err = h.handleGetFleetNode(testCtx(), makeToolRequest("bylonis_get_fleet_node", map[string]any{"hostname": "compute-n7"}))
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "compute-n070")
}

func TestGetFleetNodeWithoutAgentOrTelemetry(t *testing.T) {
	mock, _ := fleetAPI(t, map[string]string{
		"/api/v1/inventory/sites":    fleetSites,
		"/api/v1/inventory/nodes/sw": `{"id":"sw","hostname":"sw-01","kind":"network_device","status":"up","agentId":null}`,
		"/api/v1/fleet/jobs":         `{"jobs":[],"total":0}`,
	})
	result, err := newTestHandler(mock).handleGetFleetNode(testCtx(), makeToolRequest("bylonis_get_fleet_node", map[string]any{"nodeId": "sw"}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.Equal(t, "the node has no OpAMP agent linked", out["agent"])
	assert.Contains(t, out["agentHealth"].(map[string]any)["error"], "404", "a part that fails is told with it")
}

func TestGetFleetNodeHistory(t *testing.T) {
	mock, calls := fleetAPI(t, map[string]string{
		"/api/v1/inventory/nodes/n7/changes": `{"changes":[{"id":"c1","orgId":"o1","nodeId":"n7","operation":"ansible","field":"playbook","newValue":"chrony v2: ok=3 changed=1 failed=0 unreachable=0"}],"total":1}`,
	})
	h := newTestHandler(mock)
	result, err := h.handleGetFleetNodeHistory(testCtx(), makeToolRequest("bylonis_get_fleet_node_history", map[string]any{
		"nodeId": "n7", "start": "2026-10-28T00:00:00Z", "end": "1793232000000",
	}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.EqualValues(t, 1, out["total"])
	assert.NotContains(t, out["changes"].([]any)[0].(map[string]any), "orgId")
	assert.Equal(t, "2026-10-28T00:00:00Z", out["from"])
	assert.Contains(t, (*calls)[0], "from=2026-10-28T00%3A00%3A00Z")

	result, err = h.handleGetFleetNodeHistory(testCtx(), makeToolRequest("bylonis_get_fleet_node_history", map[string]any{"nodeId": "n7", "timeRange": "ayer"}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	result, err = h.handleGetFleetNodeHistory(testCtx(), makeToolRequest("bylonis_get_fleet_node_history", map[string]any{}))
	require.NoError(t, err)
	assert.True(t, result.IsError, "a node is needed")
}

func TestGetFleetCoverageAndAgentHealth(t *testing.T) {
	mock, calls := fleetAPI(t, map[string]string{
		"/api/v1/inventory/sites":        fleetSites,
		"/api/v1/inventory/coverage":     `{"window":"24h","untracked":[{"machine":"lumina-collector"}],"silent":[` + fleetNodeN07 + `],"gaps":[],"sites":[]}`,
		"/api/v1/inventory/agent_health": `{"window":"15m","nodes":[{"hostname":"a","problems":[]},{"hostname":"b","problems":["queue_full"]}]}`,
	})
	h := newTestHandler(mock)
	result, err := h.handleGetFleetCoverage(testCtx(), makeToolRequest("bylonis_get_fleet_coverage", map[string]any{"site": "lumina", "window": "24h"}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	silent := out["silent"].([]any)[0].(map[string]any)
	assert.Equal(t, "compute-n07", silent["hostname"])
	assert.NotContains(t, silent, "credentialId")
	assert.Contains(t, *calls, "/api/v1/inventory/coverage?siteId=s1&window=24h")

	result, err = h.handleListFleetAgentHealth(testCtx(), makeToolRequest("bylonis_list_fleet_agent_health", map[string]any{"problemsOnly": "true"}))
	require.NoError(t, err)
	out = resultJSON(t, result)
	assert.EqualValues(t, 1, out["total"])
	assert.Equal(t, "b", out["nodes"].([]any)[0].(map[string]any)["hostname"])
}
