package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
	"github.com/bylonis/bylonis-mcp-server/pkg/types"
)

// alertHistorySummaryConcurrency bounds the history requests in flight: the server has one history
// endpoint per rule, and a tenant can have dozens of rules.
const alertHistorySummaryConcurrency = 8

// alertFirings is how often one rule fired in the range.
type alertFirings struct {
	RuleID          string            `json:"ruleId"`
	Alert           string            `json:"alert"`
	Severity        string            `json:"severity,omitempty"`
	State           string            `json:"state,omitempty"`
	Firings         uint64            `json:"firings"`
	LastFiredAt     string            `json:"lastFiredAt,omitempty"`
	LastFiredLabels map[string]string `json:"lastFiredLabels,omitempty"`
}

type alertHistoryError struct {
	RuleID string `json:"ruleId"`
	Alert  string `json:"alert"`
	Error  string `json:"error"`
}

// alertHistorySummary answers "which alerts fired, and how many times" in one call.
type alertHistorySummary struct {
	Start        string              `json:"start"`
	End          string              `json:"end"`
	RulesChecked int                 `json:"rulesChecked"`
	Fired        []alertFirings      `json:"fired"`
	NotFired     []string            `json:"notFired"`
	Errors       []alertHistoryError `json:"errors,omitempty"`
}

// timelineResponse is the part of /api/v1/rules/{id}/history/timeline the summary reads.
type timelineResponse struct {
	Data struct {
		Items []struct {
			UnixMilli int64           `json:"unixMilli"`
			Labels    json.RawMessage `json:"labels"`
		} `json:"items"`
		Total uint64 `json:"total"`
	} `json:"data"`
}

// alertHistorySummary queries every rule's firing transitions in [start, end) and returns, per rule, how
// many there were and the last one. Each row of the history is a state change, so the firing rows are the
// times the rule fired: the count the alert's page shows.
func (h *Handler) alertHistorySummary(ctx context.Context, start, end int64) (*mcp.CallToolResult, error) {
	h.logger.DebugContext(ctx, "Tool called: bylonis_get_alert_history (summary of all rules)",
		slog.Int64("start", start), slog.Int64("end", end))

	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	rulesJSON, err := client.ListAlertRules(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "Failed to list alert rules for the history summary", logpkg.ErrAttr(err))
		return mcp.NewToolResultError(err.Error()), nil
	}
	var rules types.APIAlertRulesResponse
	if err := json.Unmarshal(rulesJSON, &rules); err != nil {
		h.logger.ErrorContext(ctx, "Failed to parse alert rules response", logpkg.ErrAttr(err), slog.String("response", logpkg.TruncBody(rulesJSON)))
		return mcp.NewToolResultError("failed to parse alert rules response: " + err.Error()), nil
	}

	results := make([]alertFirings, len(rules.Data))
	failures := make([]string, len(rules.Data))
	sem := make(chan struct{}, alertHistorySummaryConcurrency)
	var wg sync.WaitGroup
	for i, rule := range rules.Data {
		results[i] = alertFirings{RuleID: rule.ID, Alert: rule.Alert, Severity: rule.Labels["severity"], State: rule.State}
		wg.Add(1)
		go func(i int, ruleID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			body, err := client.GetAlertHistory(ctx, ruleID, types.AlertHistoryRequest{
				Start: start, End: end, State: "firing", Offset: 0, Limit: 1, Order: "desc",
				Filters: types.AlertHistoryFilters{Items: []interface{}{}, Op: "AND"},
			})
			if err != nil {
				failures[i] = err.Error()
				return
			}
			var timeline timelineResponse
			if err := json.Unmarshal(body, &timeline); err != nil {
				failures[i] = "failed to parse alert history response: " + err.Error()
				return
			}
			results[i].Firings = timeline.Data.Total
			if len(timeline.Data.Items) > 0 {
				last := timeline.Data.Items[0]
				results[i].LastFiredAt = time.UnixMilli(last.UnixMilli).UTC().Format(time.RFC3339)
				results[i].LastFiredLabels = seriesLabels(last.Labels)
			}
		}(i, rule.ID)
	}
	wg.Wait()

	summary := alertHistorySummary{
		Start:        time.UnixMilli(start).UTC().Format(time.RFC3339),
		End:          time.UnixMilli(end).UTC().Format(time.RFC3339),
		RulesChecked: len(rules.Data),
		Fired:        []alertFirings{},
		NotFired:     []string{},
	}
	for i, r := range results {
		switch {
		case failures[i] != "":
			h.logger.WarnContext(ctx, "Failed to get alert history for the summary", slog.String("ruleId", r.RuleID), slog.String("error", failures[i]))
			summary.Errors = append(summary.Errors, alertHistoryError{RuleID: r.RuleID, Alert: r.Alert, Error: failures[i]})
		case r.Firings > 0:
			summary.Fired = append(summary.Fired, r)
		default:
			summary.NotFired = append(summary.NotFired, r.Alert)
		}
	}
	sort.SliceStable(summary.Fired, func(a, b int) bool {
		if summary.Fired[a].Firings != summary.Fired[b].Firings {
			return summary.Fired[a].Firings > summary.Fired[b].Firings
		}
		return summary.Fired[a].Alert < summary.Fired[b].Alert
	})
	sort.Strings(summary.NotFired)

	out, err := json.Marshal(summary)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

// seriesLabels are the labels of the series that fired, without the rule's own ones: the history returns
// them as an object, or as a JSON string in older servers.
func seriesLabels(raw json.RawMessage) map[string]string {
	labels := map[string]string{}
	if err := json.Unmarshal(raw, &labels); err != nil {
		var asString string
		if json.Unmarshal(raw, &asString) != nil || json.Unmarshal([]byte(asString), &labels) != nil {
			return nil
		}
	}
	for k := range labels {
		if strings.HasPrefix(k, "__") || k == "alertname" || k == "ruleId" || k == "ruleSource" {
			delete(labels, k)
		}
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}
