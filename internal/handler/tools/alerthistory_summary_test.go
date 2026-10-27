package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bylonis/bylonis-mcp-server/internal/client"
	"github.com/bylonis/bylonis-mcp-server/pkg/types"
)

const summaryRules = `{"status":"success","data":[
	{"id":"rule-quiet","alert":"Disk full","state":"inactive","labels":{"severity":"warning"}},
	{"id":"rule-errors","alert":"High error rate","state":"firing","labels":{"severity":"critical"}},
	{"id":"rule-broken","alert":"Broken","state":"inactive","labels":{}},
	{"id":"rule-once","alert":"Bench regression","state":"inactive","labels":{"severity":"warning"}}
]}`

func summaryMock(calls *[]string, reqs *[]types.AlertHistoryRequest) *client.MockClient {
	var mu sync.Mutex
	return &client.MockClient{
		ListAlertRulesFn: func(ctx context.Context) (json.RawMessage, error) {
			return json.RawMessage(summaryRules), nil
		},
		GetAlertHistoryFn: func(ctx context.Context, ruleID string, req types.AlertHistoryRequest) (json.RawMessage, error) {
			mu.Lock()
			*calls = append(*calls, ruleID)
			*reqs = append(*reqs, req)
			mu.Unlock()
			switch ruleID {
			case "rule-errors":
				return json.RawMessage(`{"status":"success","data":{"total":10,"items":[
					{"unixMilli":1792724400000,"state":"firing","labels":{"alertname":"High error rate","ruleId":"rule-errors","service.name":"accounting","__name__":"x"}}]}}`), nil
			case "rule-once":
				// Older servers give the labels as a JSON string.
				return json.RawMessage(`{"status":"success","data":{"total":1,"items":[
					{"unixMilli":1792728000000,"state":"firing","labels":"{\"mode\":\"local\",\"alertname\":\"Bench regression\"}"}]}}`), nil
			case "rule-broken":
				return nil, errors.New("upstream 500")
			default:
				return json.RawMessage(`{"status":"success","data":{"total":0,"items":[]}}`), nil
			}
		},
	}
}

func TestHandleGetAlertHistory_WithoutRuleIdSummarisesEveryRule(t *testing.T) {
	var calls []string
	var reqs []types.AlertHistoryRequest
	h := newTestHandler(summaryMock(&calls, &reqs))
	req := makeToolRequest("bylonis_get_alert_history", map[string]any{
		"start": "1792652400000",
		"end":   "1792738800000",
		"state": "inactive", // ignored without ruleId
		"limit": "500",
	})

	result, err := h.handleGetAlertHistory(testCtx(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("handler returned error result: %v", result.Content)
	}
	if len(calls) != 4 {
		t.Fatalf("expected one history request per rule, got %v", calls)
	}
	for _, r := range reqs {
		if r.State != "firing" || r.Limit != 1 || r.Order != "desc" || r.Start != 1792652400000 || r.End != 1792738800000 {
			t.Fatalf("unexpected history request %+v", r)
		}
	}

	var got alertHistorySummary
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &got); err != nil {
		t.Fatalf("response is not the summary: %v", err)
	}
	if got.RulesChecked != 4 || got.Start != "2026-10-22T07:00:00Z" || got.End != "2026-10-23T07:00:00Z" {
		t.Fatalf("unexpected header %+v", got)
	}
	if len(got.Fired) != 2 || got.Fired[0].RuleID != "rule-errors" || got.Fired[0].Firings != 10 || got.Fired[1].RuleID != "rule-once" {
		t.Fatalf("fired rules not sorted by firings: %+v", got.Fired)
	}
	first := got.Fired[0]
	if first.Severity != "critical" || first.State != "firing" || first.LastFiredAt != "2026-10-23T03:00:00Z" {
		t.Fatalf("unexpected first rule %+v", first)
	}
	if len(first.LastFiredLabels) != 1 || first.LastFiredLabels["service.name"] != "accounting" {
		t.Fatalf("labels should keep only the series ones: %v", first.LastFiredLabels)
	}
	if got.Fired[1].LastFiredLabels["mode"] != "local" {
		t.Fatalf("labels given as a JSON string were not read: %v", got.Fired[1].LastFiredLabels)
	}
	if len(got.NotFired) != 1 || got.NotFired[0] != "Disk full" {
		t.Fatalf("unexpected notFired %v", got.NotFired)
	}
	if len(got.Errors) != 1 || got.Errors[0].RuleID != "rule-broken" {
		t.Fatalf("a rule whose history failed must be reported, not dropped: %v", got.Errors)
	}
}

func TestHandleGetAlertHistory_EmptyRuleIdSummarises(t *testing.T) {
	var calls []string
	var reqs []types.AlertHistoryRequest
	h := newTestHandler(summaryMock(&calls, &reqs))
	req := makeToolRequest("bylonis_get_alert_history", map[string]any{"ruleId": "", "timeRange": "24h"})

	result, err := h.handleGetAlertHistory(testCtx(), req)
	if err != nil || result.IsError {
		t.Fatalf("expected the summary, got %v %v", err, result.Content)
	}
	if len(calls) != 4 {
		t.Fatalf("expected one history request per rule, got %v", calls)
	}
}

func TestHandleGetAlertHistory_SummaryWhenListingRulesFails(t *testing.T) {
	h := newTestHandler(&client.MockClient{
		ListAlertRulesFn: func(ctx context.Context) (json.RawMessage, error) {
			return nil, errors.New("upstream down")
		},
	})
	result, err := h.handleGetAlertHistory(testCtx(), makeToolRequest("bylonis_get_alert_history", map[string]any{"timeRange": "24h"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected an error result when the rules cannot be listed")
	}
}
