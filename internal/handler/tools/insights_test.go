package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
)

// readOnlyAPI answers the read-only GETs and POSTs like ByLonis, by path, and records them.
func readOnlyAPI(answers map[string]string) (*bylonisclient.MockClient, *[]string, *[]string) {
	calls, bodies := []string{}, []string{}
	answer := func(path string, query url.Values) (json.RawMessage, error) {
		calls = append(calls, path+"?"+query.Encode())
		data, ok := answers[path]
		if !ok {
			return nil, bylonisclient.ErrNotInThisVersion{Path: path}
		}
		return json.RawMessage(`{"status":"success","data":` + data + `}`), nil
	}
	return &bylonisclient.MockClient{
		GetReadOnlyFn: func(_ context.Context, path string, query url.Values) (json.RawMessage, error) {
			return answer(path, query)
		},
		PostReadOnlyFn: func(_ context.Context, path string, query url.Values, body []byte) (json.RawMessage, error) {
			bodies = append(bodies, string(body))
			return answer(path, query)
		},
		ListAlertRulesFn: func(context.Context) (json.RawMessage, error) {
			return json.RawMessage(`{"status":"success","data":[{"id":"r1","alert":"Disco lleno"},{"id":"r2","alert":"CPU alta"}]}`), nil
		},
	}, &calls, &bodies
}

// The answer of lumina on 2026-11-17.
const flappingLumina = `{"days":7,"minAlerts":10,"shortMinutes":5,"minShortPercent":50,"rules":[{"ruleId":"01a169cd","ruleName":"Lumina: pérdida de paquetes en la salida a internet","alerts":21,"shortAlerts":13,"shortPercent":61.9,"medianMinutes":4,"holdMinutes":0,"fleet":false,"holdOptions":[{"minutes":5,"avoided":13,"percent":61.9},{"minutes":10,"avoided":20,"percent":95.2}],"suggestions":[{"kind":"hold","minutes":10},{"kind":"sql"}]}]}`

func TestListFlappingRules(t *testing.T) {
	mock, calls, _ := readOnlyAPI(map[string]string{"/api/v2/alert_quality/flapping": flappingLumina})
	h := newTestHandler(mock)

	result, err := h.handleListFlappingRules(testCtx(), makeToolRequest("bylonis_list_flapping_rules", map[string]any{"days": "14"}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.EqualValues(t, 1, out["total"])
	rule := out["rules"].([]any)[0].(map[string]any)
	assert.EqualValues(t, 10, rule["suggestions"].([]any)[0].(map[string]any)["minutes"])
	assert.Equal(t, []string{"/api/v2/alert_quality/flapping?days=14"}, *calls)

	for _, days := range []string{"0", "91", "siete"} {
		result, err = h.handleListFlappingRules(testCtx(), makeToolRequest("bylonis_list_flapping_rules", map[string]any{"days": days}))
		require.NoError(t, err)
		assert.Contains(t, errorText(t, result), "from 1 to 90", days)
	}
	assert.Len(t, *calls, 1, "a wrong days is not sent")
}

func TestListAlertAcknowledgements(t *testing.T) {
	mock, calls, _ := readOnlyAPI(map[string]string{"/api/v2/alert_acknowledgements": `[{"id":"a1","orgId":"o1","ruleId":"r1","series":"123","firedAt":1794870000000,"ruleName":"Disco lleno","note":"lo miro yo","createdBy":"guardia@example.com","createdAt":"2026-11-17T10:00:00Z"}]`})
	h := newTestHandler(mock)

	result, err := h.handleListAlertAcknowledgements(testCtx(), makeToolRequest("bylonis_list_alert_acknowledgements", map[string]any{"ruleId": "r1"}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	ack := out["acknowledgements"].([]any)[0].(map[string]any)
	assert.Equal(t, "guardia@example.com", ack["acknowledgedBy"], "the names of bylonis_list_alerts")
	assert.Equal(t, "lo miro yo", ack["acknowledgementNote"])
	assert.Equal(t, "2026-11-17T10:00:00Z", ack["acknowledgedAt"])
	assert.NotContains(t, ack, "orgId")
	assert.Equal(t, []string{"/api/v2/alert_acknowledgements?ruleId=r1"}, *calls, "ByLonis picks the days")
}

func TestListMaintenanceWindows(t *testing.T) {
	mock, _, _ := readOnlyAPI(map[string]string{"/api/v1/downtime_schedules": `[
		{"id":"m1","name":"Parche mensual","status":"active","kind":"recurring","alertIds":["r1"],"schedule":{"timezone":"America/New_York","recurrence":{"duration":"2h","repeatType":"monthly"}},"createdBy":"ops@example.com"},
		{"id":"m2","name":"Corte de red","status":"upcoming","kind":"fixed","alertIds":[],"schedule":{"timezone":"UTC"}},
		{"id":"m3","name":"Migración","status":"expired","kind":"fixed","alertIds":["r2"],"schedule":{"timezone":"UTC"}}]`})
	h := newTestHandler(mock)

	result, err := h.handleListMaintenanceWindows(testCtx(), makeToolRequest("bylonis_list_maintenance_windows", map[string]any{}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.EqualValues(t, 2, out["total"], "the expired one is left out")
	windows := out["windows"].([]any)
	first := windows[0].(map[string]any)
	assert.Equal(t, false, first["allRules"])
	assert.Equal(t, "Disco lleno", first["rules"].([]any)[0].(map[string]any)["name"], "the rules by name")
	assert.Equal(t, true, windows[1].(map[string]any)["allRules"], "no alertIds is every rule")

	// The windows that cover r2: the one of every rule, and the expired one when asked.
	result, err = h.handleListMaintenanceWindows(testCtx(), makeToolRequest("bylonis_list_maintenance_windows", map[string]any{"ruleId": "r2", "includeExpired": "true"}))
	require.NoError(t, err)
	out = resultJSON(t, result)
	names := []string{}
	for _, window := range out["windows"].([]any) {
		names = append(names, window.(map[string]any)["name"].(string))
	}
	assert.Equal(t, []string{"Corte de red", "Migración"}, names)

	result, err = h.handleListMaintenanceWindows(testCtx(), makeToolRequest("bylonis_list_maintenance_windows", map[string]any{"status": "ahora"}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestListChangeEvents(t *testing.T) {
	mock, calls, _ := readOnlyAPI(map[string]string{"/api/v1/events": `{"events":[
		{"id":"e1","kind":"deploy","source":"api","timestamp":1794870000000,"title":"api 2.4.1","service":"api","version":"2.4.1","actor":"ci"},
		{"id":"rule:r1:1","kind":"alert_firing","source":"alerts","timestamp":1794870060000,"title":"Disco lleno"}]}`})
	h := newTestHandler(mock)

	result, err := h.handleListChangeEvents(testCtx(), makeToolRequest("bylonis_list_change_events", map[string]any{
		"service": "api", "kinds": "deploy, alert_firing", "start": "1794866400000", "end": "1794873600000",
	}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.EqualValues(t, 2, out["total"])
	assert.EqualValues(t, map[string]any{"deploy": 1.0, "alert_firing": 1.0}, out["byKind"])
	assert.NotContains(t, out["events"].([]any)[0], "id")
	assert.Equal(t, []string{"/api/v1/events?end=1794873600000&kinds=deploy%2Calert_firing&service=api&start=1794866400000"}, *calls)

	result, err = h.handleListChangeEvents(testCtx(), makeToolRequest("bylonis_list_change_events", map[string]any{"timeRange": "40d"}))
	require.NoError(t, err)
	assert.Contains(t, errorText(t, result), "at most 31 days")
}

// lumina (rc.98) does not have the change events nor the log patterns: the tool says so, it does not fail on HTML.
func TestInsightNotInThisVersion(t *testing.T) {
	mock, _, _ := readOnlyAPI(map[string]string{})
	h := newTestHandler(mock)

	result, err := h.handleListChangeEvents(testCtx(), makeToolRequest("bylonis_list_change_events", map[string]any{}))
	require.NoError(t, err)
	text := errorText(t, result)
	assert.Contains(t, text, "does not have /api/v1/events yet")
	assert.Contains(t, text, "tenant has to be upgraded")
}

func TestGetLogPatterns(t *testing.T) {
	long := strings.Repeat("x", 400)
	mock, _, bodies := readOnlyAPI(map[string]string{"/api/v5/logs/patterns": `{"patterns":[
		{"pattern":"timeout after <n>ms to <ip>","regex":"^timeout after \\d+ms","count":120,"levels":{"error":120},"sample":"` + long + `","firstSeen":1,"lastSeen":2}],
		"totalPatterns":7,"linesScanned":500,"linesTotal":500,"exact":true,"method":"exact","sampleRate":1,"prevLinesTotal":300}`})
	h := newTestHandler(mock)

	result, err := h.handleGetLogPatterns(testCtx(), makeToolRequest("bylonis_get_log_patterns", map[string]any{
		"filter": "service.name = 'api'", "compareShift": "24h", "limit": "20", "start": "1794866400000", "end": "1794870000000",
	}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	pattern := out["patterns"].([]any)[0].(map[string]any)
	assert.Equal(t, 301, len([]rune(pattern["sample"].(string))), "a long sample is cut")
	assert.EqualValues(t, 300, out["prevLinesTotal"])

	sent := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte((*bodies)[0]), &sent))
	assert.EqualValues(t, 86400, sent["compareShift"], "ByLonis takes the shift in seconds")
	assert.EqualValues(t, map[string]any{"expression": "service.name = 'api'"}, sent["filter"])
	assert.EqualValues(t, 20, sent["limit"])
	assert.EqualValues(t, 1794866400000, sent["start"])
	assert.NotContains(t, sent, "exact")

	result, err = h.handleGetLogPatterns(testCtx(), makeToolRequest("bylonis_get_log_patterns", map[string]any{"compareShift": "31d"}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func backtestRule() map[string]any {
	return map[string]any{
		"alert": "Errores de la API", "alertType": "LOGS_BASED_ALERT", "ruleType": "threshold_rule", "version": "v5", "schemaVersion": "v2alpha1",
		"holdDuration": "10m",
		"condition": map[string]any{
			"compositeQuery": map[string]any{"queryType": "builder", "panelType": "graph", "queries": []any{map[string]any{
				"type": "builder_query", "spec": map[string]any{"name": "A", "signal": "logs", "stepInterval": 60,
					"aggregations": []any{map[string]any{"expression": "count()"}}, "filter": map[string]any{"expression": "service.name = 'api'"}},
			}}},
			"selectedQueryName": "A",
			"thresholds":        map[string]any{"kind": "basic", "spec": []any{map[string]any{"name": "critical", "target": 10, "op": "1", "matchType": "1"}}},
		},
		"evaluation": map[string]any{"kind": "rolling", "spec": map[string]any{"evalWindow": "5m", "frequency": "1m"}},
	}
}

func TestBacktestAlert(t *testing.T) {
	mock, calls, bodies := readOnlyAPI(map[string]string{"/api/v1/rules/backtest": `{"supported":true,"evaluations":10080,"series":1,"firings":4,"shortFirings":1,"shortPercent":25,"seriesFiring":1,"firingMinutes":95,"firingAtEnd":0,"truncated":false,"latest":[]}`})
	h := newTestHandler(mock)
	created := false
	mock.CreateAlertRuleFn = func(context.Context, []byte) (json.RawMessage, error) {
		created = true
		return nil, errors.New("the backtest must not create the rule")
	}

	rule := backtestRule()
	rule["days"] = 14.0
	rule["searchContext"] = "¿cuántas veces saltaría?"
	result, err := h.handleBacktestAlert(testCtx(), makeToolRequest("bylonis_backtest_alert", rule))
	require.NoError(t, err)
	out := resultJSON(t, result)
	assert.EqualValues(t, 4, out["firings"])
	assert.False(t, created)
	assert.Equal(t, []string{"/api/v1/rules/backtest?days=14"}, *calls)
	sent := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte((*bodies)[0]), &sent))
	assert.Equal(t, "10m", sent["holdDuration"])
	assert.NotContains(t, sent, "days")
	assert.NotContains(t, sent, "searchContext")

	// Without channels it still replays: nobody is notified.
	assert.NotContains(t, sent, "preferredChannels")

	rule = backtestRule()
	rule["days"] = 30.0
	result, err = h.handleBacktestAlert(testCtx(), makeToolRequest("bylonis_backtest_alert", rule))
	require.NoError(t, err)
	assert.Contains(t, errorText(t, result), "from 1 to 14")

	rule = backtestRule()
	rule["holdDuration"] = "1d"
	result, err = h.handleBacktestAlert(testCtx(), makeToolRequest("bylonis_backtest_alert", rule))
	require.NoError(t, err)
	assert.Contains(t, errorText(t, result), "holdDuration", "the validation of create")
	assert.Len(t, *calls, 1)
}

func TestBacktestAlertWithAViewerKey(t *testing.T) {
	mock := &bylonisclient.MockClient{
		PostReadOnlyFn: func(context.Context, string, url.Values, []byte) (json.RawMessage, error) {
			return nil, errors.New(`unexpected status 403: {"status":"error","error":"forbidden"}`)
		},
	}
	h := newTestHandler(mock)
	result, err := h.handleBacktestAlert(testCtx(), makeToolRequest("bylonis_backtest_alert", backtestRule()))
	require.NoError(t, err)
	assert.Contains(t, errorText(t, result), "does not have the role")
}
