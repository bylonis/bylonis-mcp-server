package tools

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
)

const capacityAnswer = `{"status":"success","data":{"kinds":[{"kind":"filesystem","count":3}],"resources":[
 {"key":"a","kind":"node_filesystem","labels":{"k8s.node.name":"n1"},"usage":95,"limit":100,"ratio":0.95,"trendPerDay":2,"daysToFull":0.5,"fullAt":"2026-11-20T00:00:00Z","horizonDays":90,"scale":"short","valid":true,"informative":false},
 {"key":"b","kind":"filesystem","labels":{"host.name":"db1","mountpoint":"/data"},"usage":90,"limit":100,"ratio":0.9,"trendPerDay":1,"daysToFull":10,"horizonDays":90,"scale":"short","valid":true,"informative":false},
 {"key":"c","kind":"host_memory","labels":{"host.name":"db3"},"usage":7,"limit":10,"ratio":0.7,"trendPerDay":0,"daysToFull":2,"horizonDays":90,"scale":"short","valid":false,"reason":"level_shift","informative":false},
 {"key":"d","kind":"workload_cpu","labels":{"k8s.pod.name":"p"},"usage":2,"limit":1,"ratio":2,"trendPerDay":0,"daysToFull":0,"horizonDays":90,"scale":"short","valid":true,"informative":true},
 {"key":"e","kind":"filesystem","labels":{"host.name":"db2","mountpoint":"/"},"usage":50,"limit":100,"ratio":0.5,"trendPerDay":0,"horizonDays":90,"scale":"short","valid":true,"informative":false}]}}`

func capacityAPI(queries *[]url.Values) *bylonisclient.MockClient {
	return &bylonisclient.MockClient{
		GetCapacityResourcesFn: func(_ context.Context, query url.Values) (json.RawMessage, error) {
			*queries = append(*queries, query)
			return json.RawMessage(capacityAnswer), nil
		},
	}
}

func TestListCapacityResources(t *testing.T) {
	var queries []url.Values
	h := newTestHandler(capacityAPI(&queries))

	result, err := h.handleListCapacityResources(testCtx(), makeToolRequest("bylonis_list_capacity_resources", map[string]any{
		"kind": "filesystem", "search": "db", "limit": "2",
	}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.Equal(t, url.Values{"kind": {"filesystem"}, "q": {"db"}}, queries[0])
	assert.Len(t, out["resources"], 2)
	assert.EqualValues(t, 5, out["total"])

	// withinDays keeps only reliable forecasts full within that many days: not the unreliable one, nor the CPU.
	result, err = h.handleListCapacityResources(testCtx(), makeToolRequest("bylonis_list_capacity_resources", map[string]any{"withinDays": "14"}))
	require.NoError(t, err)
	out = resultJSON(t, result)
	resources := out["resources"].([]any)
	require.Len(t, resources, 2)
	assert.Equal(t, "node_filesystem", resources[0].(map[string]any)["kind"])
	assert.Equal(t, "/data", resources[1].(map[string]any)["labels"].(map[string]any)["mountpoint"])
}

func TestListCapacityResourcesRefusesBadArguments(t *testing.T) {
	var queries []url.Values
	h := newTestHandler(capacityAPI(&queries))
	for _, args := range []map[string]any{{"kind": "gpu"}, {"limit": "0"}, {"withinDays": "-1"}} {
		result, err := h.handleListCapacityResources(testCtx(), makeToolRequest("bylonis_list_capacity_resources", args))
		require.NoError(t, err)
		assert.True(t, result.IsError, "%v", args)
	}
	assert.Empty(t, queries)
}
