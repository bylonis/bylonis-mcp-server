package tools

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
)

const aiTeamsAnswer = `{"status":"success","data":[{"id":"t-ia","name":"QA Team IA"}]}`

const aiAccountsAnswer = `{"status":"success","data":[
 {"id":"a1","name":"Gemini gratuito","provider":"gemini","scopeKind":"org","teamId":null,"status":"active","endpointKind":"direct","baseUrl":"https://g","models":{"gemini-3.5-flash-lite":"gemini-3.5-flash-lite"},"rpm":10,"priority":0,"credentialLast4":"ZUEw","lastProbe":{"status":"ok","detail":"Google accepts the credential","checkedAt":"2026-11-26T12:00:00Z"}},
 {"id":"a2","name":"Gemini equipo","provider":"gemini","scopeKind":"team","teamId":"t-ia","status":"draining","endpointKind":"direct","models":{"gemini-3.5-flash-lite":"x"},"rpm":0,"credentialLast4":"ZUEw","lastProbe":null},
 {"id":"a3","name":"On-prem","provider":"onprem","scopeKind":"org","teamId":null,"status":"disabled","endpointKind":"onprem","models":{"qwen3-30b-a3b":"qwen3-30b-a3b"},"credentialLast4":"b99a","adminKeyLast4":"zzzz"}]}`

func aiGatewayAPI(paths *[]string, bodies *[]string) *bylonisclient.MockClient {
	return &bylonisclient.MockClient{
		GetReadOnlyFn: func(_ context.Context, path string, _ url.Values) (json.RawMessage, error) {
			*paths = append(*paths, path)
			if path == "/api/v1/ai/teams" {
				return json.RawMessage(aiTeamsAnswer), nil
			}
			return json.RawMessage(aiAccountsAnswer), nil
		},
		QueryBuilderV5Fn: func(_ context.Context, body []byte) (json.RawMessage, error) {
			*bodies = append(*bodies, string(body))
			return json.RawMessage(`{"status":"success","data":{"type":"raw","data":{"results":[{"queryName":"A","rows":[
				{"data":{"team":"t-ia","app":"qa","model":"gemini-3.5-flash-lite","requests":3,"rejected":1,"unpriced":0,"input_tokens":30,"output_tokens":9,"cost_usd":0.5}},
				{"data":{"team":"","app":"ia-ops","model":"claude-sonnet-5-5","requests":2,"rejected":0,"unpriced":0,"input_tokens":20,"output_tokens":6,"cost_usd":0.25}}]}]}}}`), nil
		},
	}
}

func TestAIGatewayPoolNeverReturnsACredential(t *testing.T) {
	var paths, bodies []string
	h := newTestHandler(aiGatewayAPI(&paths, &bodies))
	result, err := h.handleAIGatewayPool(testCtx(), makeToolRequest("bylonis_ai_gateway_pool", map[string]any{}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.EqualValues(t, 3, out["total"])
	raw, _ := json.Marshal(out)
	for _, secret := range []string{"ZUEw", "b99a", "zzzz", "credentialLast4", "https://g"} {
		assert.NotContains(t, string(raw), secret, "nothing of the credential, nor the URL")
	}
	accounts := out["accounts"].([]any)
	team, org := accounts[0].(map[string]any), accounts[1].(map[string]any) // by provider, then name
	assert.Equal(t, "Gemini equipo", team["name"])
	assert.Equal(t, "QA Team IA", team["team"], "the team by its name")
	assert.Equal(t, "Gemini gratuito", org["name"])
	assert.Equal(t, "ok", org["lastCheck"])
	assert.EqualValues(t, 1, out["byProvider"].(map[string]any)["gemini"].(map[string]any)["draining"])

	result, _ = h.handleAIGatewayPool(testCtx(), makeToolRequest("bylonis_ai_gateway_pool", map[string]any{"provider": "onprem"}))
	out = resultJSON(t, result)
	assert.EqualValues(t, 1, out["total"])
}

func TestAIGatewayUsageQueriesTheGatewaySpansSafely(t *testing.T) {
	var paths, bodies []string
	h := newTestHandler(aiGatewayAPI(&paths, &bodies))
	result, err := h.handleAIGatewayUsage(testCtx(), makeToolRequest("bylonis_ai_gateway_usage", map[string]any{"team": "QA Team IA", "timeRange": "7d"}))
	require.NoError(t, err)
	require.False(t, result.IsError)
	out := resultJSON(t, result)
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `attributes_string['bylonis.team'] = 't-ia'`, "the team by its name becomes its id")
	assert.Contains(t, bodies[0], `"type":"clickhouse_sql"`)
	assert.Contains(t, bodies[0], "name = 'gateway.request'")
	rows := out["rows"].([]any)
	assert.Equal(t, "QA Team IA", rows[0].(map[string]any)["teamName"])
	assert.Equal(t, "(sin equipo)", rows[1].(map[string]any)["teamName"])
	assert.InDelta(t, 0.75, out["totalCostUsd"], 1e-9)

	// Grouping by provider only; the columns come from a fixed set.
	_, err = h.handleAIGatewayUsage(testCtx(), makeToolRequest("bylonis_ai_gateway_usage", map[string]any{"groupBy": "provider"}))
	require.NoError(t, err)
	assert.Contains(t, bodies[1], "GROUP BY provider")
}

func TestAIGatewayUsageRefusesWhatCouldBreakTheQuery(t *testing.T) {
	var paths, bodies []string
	h := newTestHandler(aiGatewayAPI(&paths, &bodies))
	for _, args := range []map[string]any{
		{"app": "x' OR 1=1 --"}, {"model": "a b"}, {"groupBy": "team,password"}, {"limit": "0"}, {"team": strings.Repeat("a", 200)},
	} {
		result, err := h.handleAIGatewayUsage(testCtx(), makeToolRequest("bylonis_ai_gateway_usage", args))
		require.NoError(t, err)
		assert.True(t, result.IsError, "%v", args)
	}
	assert.Empty(t, bodies, "nothing reached ByLonis")
}
