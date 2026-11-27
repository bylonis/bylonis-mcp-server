package tools

// The AI Gateway of ByLonis (bylonis#1348): how much each team, application and model uses, what each budget has
// spent in its window, and the state of the pool of provider accounts, read only, so IA OPS and other agents answer «who spends the most on AI this week?» or «is
// any account of the pool down?» without the operator opening Ajustes → AI Gateway. Never a credential: the pool
// tool leaves out even the last four characters the API shows.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
)

const (
	aiGatewayUsageDefaultLimit = 20
	aiGatewayUsageMaxLimit     = 200
	aiGatewaySpans             = "bylonis_traces.distributed_bylonis_index_v3"
	aiGatewayService           = "bylonis-aigateway"
)

// aiGatewayGroups are the dimensions the usage can be grouped by, each with its column in the gateway's spans
// (#334): only these, so the SQL never takes a column from the caller.
var aiGatewayGroups = map[string]string{
	"team":     "attributes_string['bylonis.team']",
	"app":      "attributes_string['bylonis.app']",
	"model":    "attributes_string['gen_ai.request.model']",
	"provider": "attributes_string['gen_ai.system']",
}

// aiGatewayFilterValue is what a filter value may be: ids, application names and model names. Anything else is
// refused, so a value never breaks out of its quotes in the SQL.
var aiGatewayFilterValue = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,128}$`)

func (h *Handler) RegisterAIGatewayHandlers(s *server.MCPServer) {
	h.logger.Debug("Registering AI Gateway handlers")
	addTool(s, mcp.NewTool("bylonis_ai_gateway_usage",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("searchContext", mcp.Description("The user's original question or search text that triggered this tool call. Always include the user's raw query here for better results.")),
		mcp.WithDescription("How much the AI Gateway of ByLonis was used in a time range, grouped by team, application (the app of the virtual key), model and/or provider: "+
			"requests, rejected requests, input and output tokens, and cost in USD (the tariff of the model, from the gateway's own traces; a request of a model without tariff "+
			"counts as unpriced, never as free). Sorted by cost, the highest first. Use it for «who spends the most on AI this week?», «how much did team X spend today?» or "+
			"«which model is used the most?». Teams come with their name when the caller can read the teams. Read only."),
		mcp.WithString("groupBy", mcp.Description("Comma-separated dimensions among team, app, model and provider. Default 'team,app,model'.")),
		mcp.WithString("team", mcp.Description("Only this team (its id or its name).")),
		mcp.WithString("app", mcp.Description("Only this application (the app of the virtual key, e.g. 'ia-ops').")),
		mcp.WithString("model", mcp.Description("Only this model (e.g. 'claude-sonnet-5-5').")),
		mcp.WithString("timeRange", mcp.Description("How far back: '<n>m', '<n>h' or '<n>d'. Default '24h'. Ignored with start.")),
		mcp.WithString("start", mcp.Description("Start timestamp in milliseconds (optional).")),
		mcp.WithString("end", mcp.Description("End timestamp in milliseconds (optional, defaults to now).")),
		mcp.WithString("limit", mcp.Description(fmt.Sprintf("How many rows. Default %d, at most %d.", aiGatewayUsageDefaultLimit, aiGatewayUsageMaxLimit))),
	), h.handleAIGatewayUsage)

	addTool(s, mcp.NewTool("bylonis_ai_gateway_pool",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("searchContext", mcp.Description("The user's original question or search text that triggered this tool call. Always include the user's raw query here for better results.")),
		mcp.WithDescription("The pool of provider accounts of the AI Gateway (Anthropic, OpenAI, Gemini, xAI, on-prem…) of the organization and its teams: for each account its provider, "+
			"scope (organization or team, with the team's name), status (active, draining or disabled), endpoint, the models it serves, its requests-per-minute limit and its last "+
			"connection check (ok, rejected or unavailable, and when). Use it for «is any AI account down?», «which accounts serve Claude?» or to explain a 503 of the gateway. "+
			"Never returns a credential. Read only; needs the permission to read the AI accounts."),
		mcp.WithString("provider", mcp.Description("Only accounts of this provider (anthropic, openai, gemini, xai, onprem).")),
		mcp.WithString("status", mcp.Description("Only accounts in this status (active, draining, disabled).")),
	), h.handleAIGatewayPool)

	addTool(s, mcp.NewTool("bylonis_ai_gateway_budgets",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("searchContext", mcp.Description("The user's original question or search text that triggered this tool call. Always include the user's raw query here for better results.")),
		mcp.WithDescription("The budgets of the AI Gateway (organization, teams, users, keys and projects) with what each has spent in its current window, as the gateway applies it: "+
			"limit, spent and reserved USD, what is left, percent, when the window ends, the policy when it runs out (block, degrade, onprem, warn) and three warnings: "+
			"exhausted, stopsBeforeLimit (what is left is less than the last request reserved, so the next one like it is refused before 100 %) and stale (the gateway has "+
			"not reported for a while: the figures are the last known). Sorted by percent, the highest first. Use it for «which budget is about to run out?», "+
			"«how much is left for team X this month?» or to explain a 429 budget_exhausted. Read only; needs the permission to read the AI Gateway administration."),
		mcp.WithString("level", mcp.Description("Only budgets of this level (org, team, user, key, project).")),
		mcp.WithString("name", mcp.Description("Only budgets whose name contains this text (case-insensitive).")),
		mcp.WithString("minPercent", mcp.Description("Only budgets at or above this percent of their limit (e.g. '80').")),
	), h.handleAIGatewayBudgets)
}

// aiTeams maps team ids to names and names (lower case) to ids. Empty when the caller cannot read the teams.
func (h *Handler) aiTeams(ctx context.Context, read func(string) (json.RawMessage, error)) (byID, byName map[string]string) {
	byID, byName = map[string]string{}, map[string]string{}
	raw, err := read("/api/v1/ai/teams")
	if err != nil {
		return byID, byName
	}
	var envelope struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return byID, byName
	}
	for _, t := range envelope.Data {
		byID[t.ID], byName[strings.ToLower(t.Name)] = t.Name, t.ID
	}
	return byID, byName
}

func (h *Handler) handleAIGatewayUsage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	if args == nil {
		args = map[string]any{}
	}
	text := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(v) }
	limit := aiGatewayUsageDefaultLimit
	if v := text("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return mcp.NewToolResultError(`Parameter validation failed: "limit" must be a positive integer`), nil
		}
		limit = min(n, aiGatewayUsageMaxLimit)
	}
	groups := []string{"team", "app", "model"}
	if g := text("groupBy"); g != "" {
		groups = nil
		for _, part := range strings.Split(g, ",") {
			part = strings.TrimSpace(strings.ToLower(part))
			if _, ok := aiGatewayGroups[part]; !ok {
				return mcp.NewToolResultError(`Parameter validation failed: "groupBy" takes team, app, model and provider`), nil
			}
			groups = append(groups, part)
		}
	}
	start, end, err := resolveTimestamps(args, "24h")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	read := func(path string) (json.RawMessage, error) { return client.GetReadOnly(ctx, path, nil) }
	teamNames, teamIDs := h.aiTeams(ctx, read)

	var where []string
	for _, f := range []string{"team", "app", "model"} {
		value := text(f)
		if value == "" {
			continue
		}
		if f == "team" {
			if id, ok := teamIDs[strings.ToLower(value)]; ok {
				value = id
			}
		}
		if !aiGatewayFilterValue.MatchString(value) {
			return mcp.NewToolResultError(fmt.Sprintf(`Parameter validation failed: %q may only have letters, digits and . _ : / -`, f)), nil
		}
		where = append(where, fmt.Sprintf("AND %s = '%s'", aiGatewayGroups[f], value))
	}
	selects := make([]string, 0, len(groups))
	for _, g := range groups {
		selects = append(selects, fmt.Sprintf("%s AS %s", aiGatewayGroups[g], g))
	}
	sql := fmt.Sprintf("SELECT %s, count() AS requests, countIf(attributes_string['bylonis.rejected_by'] != '') AS rejected, "+
		"countIf(attributes_string['bylonis.rejected_by'] = '' AND NOT mapContains(attributes_number, 'gen_ai.usage.cost')) AS unpriced, "+
		"sum(attributes_number['gen_ai.usage.input_tokens']) AS input_tokens, sum(attributes_number['gen_ai.usage.output_tokens']) AS output_tokens, "+
		"round(sum(attributes_number['gen_ai.usage.cost']), 6) AS cost_usd\n"+
		"FROM %s\nWHERE resource_string_service$$name = '%s' AND name = 'gateway.request'\n"+
		"  AND timestamp >= fromUnixTimestamp64Nano({{.start_timestamp_nano}}) AND timestamp <= fromUnixTimestamp64Nano({{.end_timestamp_nano}})\n"+
		"  AND ts_bucket_start >= {{.start_timestamp}} - 1800 AND ts_bucket_start <= {{.end_timestamp}} %s\n"+
		"GROUP BY %s ORDER BY cost_usd DESC, requests DESC LIMIT %d",
		strings.Join(selects, ", "), aiGatewaySpans, aiGatewayService, strings.Join(where, " "), strings.Join(groups, ", "), limit)
	body, _ := json.Marshal(map[string]any{
		"schemaVersion": "v1", "start": start, "end": end, "requestType": "raw",
		"compositeQuery": map[string]any{"queries": []any{map[string]any{"type": "clickhouse_sql", "spec": map[string]any{"name": "A", "query": sql}}}},
	})
	h.logger.InfoContext(ctx, "Tool called: bylonis_ai_gateway_usage", slog.String("groupBy", strings.Join(groups, ",")))
	raw, err := client.QueryBuilderV5(ctx, body)
	if err != nil {
		h.logger.ErrorContext(ctx, "AI Gateway usage failed", logpkg.ErrAttr(err))
		return mcp.NewToolResultError(err.Error()), nil
	}
	var envelope struct {
		Data struct {
			Data struct {
				Results []struct {
					Rows []struct {
						Data map[string]any `json:"data"`
					} `json:"rows"`
				} `json:"results"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return mcp.NewToolResultError("failed to parse response: " + err.Error()), nil
	}
	rows := []map[string]any{}
	total := 0.0
	for _, result := range envelope.Data.Data.Results {
		for _, row := range result.Rows {
			if id, ok := row.Data["team"].(string); ok {
				if name := teamNames[id]; name != "" {
					row.Data["teamName"] = name
				} else if id == "" {
					row.Data["teamName"] = "(sin equipo)"
				}
			}
			if cost, ok := row.Data["cost_usd"].(float64); ok {
				total += cost
			}
			rows = append(rows, row.Data)
		}
	}
	return fleetResult(map[string]any{"rows": rows, "groupBy": groups, "start": start, "end": end, "totalCostUsd": total})
}

// aiAccount is what the model reads of an account of the pool: no credential, not even its last characters.
type aiAccount struct {
	Name      string   `json:"name"`
	Provider  string   `json:"provider"`
	Scope     string   `json:"scope"`
	Team      string   `json:"team,omitempty"`
	Status    string   `json:"status"`
	Endpoint  string   `json:"endpoint"`
	Region    string   `json:"region,omitempty"`
	Models    []string `json:"models"`
	RPM       int      `json:"rpm,omitempty"`
	Priority  int      `json:"priority,omitempty"`
	LastCheck string   `json:"lastCheck,omitempty"`
	CheckedAt string   `json:"checkedAt,omitempty"`
	Detail    string   `json:"lastCheckDetail,omitempty"`
}

func (h *Handler) handleAIGatewayPool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	text := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(strings.ToLower(v)) }
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	read := func(path string) (json.RawMessage, error) { return client.GetReadOnly(ctx, path, nil) }
	raw, err := read("/api/v1/ai/accounts")
	if err != nil {
		h.logger.ErrorContext(ctx, "AI Gateway pool failed", logpkg.ErrAttr(err))
		return mcp.NewToolResultError(err.Error()), nil
	}
	var envelope struct {
		Data []struct {
			Name         string            `json:"name"`
			Provider     string            `json:"provider"`
			ScopeKind    string            `json:"scopeKind"`
			TeamID       *string           `json:"teamId"`
			Status       string            `json:"status"`
			EndpointKind string            `json:"endpointKind"`
			Region       string            `json:"region"`
			Models       map[string]string `json:"models"`
			RPM          int               `json:"rpm"`
			Priority     int               `json:"priority"`
			LastProbe    *struct {
				Status    string `json:"status"`
				Detail    string `json:"detail"`
				CheckedAt string `json:"checkedAt"`
			} `json:"lastProbe"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return mcp.NewToolResultError("failed to parse response: " + err.Error()), nil
	}
	teamNames, _ := h.aiTeams(ctx, read)
	accounts := []aiAccount{}
	summary := map[string]map[string]int{} // provider -> status -> count
	for _, a := range envelope.Data {
		if p := text("provider"); p != "" && a.Provider != p {
			continue
		}
		if s := text("status"); s != "" && a.Status != s {
			continue
		}
		out := aiAccount{Name: a.Name, Provider: a.Provider, Scope: a.ScopeKind, Status: a.Status, Endpoint: a.EndpointKind, Region: a.Region,
			RPM: a.RPM, Priority: a.Priority, Models: []string{}}
		if a.TeamID != nil && *a.TeamID != "" {
			out.Team = teamNames[*a.TeamID]
			if out.Team == "" {
				out.Team = *a.TeamID
			}
		}
		for model := range a.Models {
			out.Models = append(out.Models, model)
		}
		sort.Strings(out.Models)
		if a.LastProbe != nil {
			out.LastCheck, out.CheckedAt, out.Detail = a.LastProbe.Status, a.LastProbe.CheckedAt, a.LastProbe.Detail
		}
		if summary[a.Provider] == nil {
			summary[a.Provider] = map[string]int{}
		}
		summary[a.Provider][a.Status]++
		accounts = append(accounts, out)
	}
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Provider != accounts[j].Provider {
			return accounts[i].Provider < accounts[j].Provider
		}
		return accounts[i].Name < accounts[j].Name
	})
	return fleetResult(map[string]any{"accounts": accounts, "byProvider": summary, "total": len(accounts)})
}

// aiBudget is what the budgets tool returns of a budget.
type aiBudget struct {
	Name             string   `json:"name"`
	Level            string   `json:"level"`
	Parent           string   `json:"parent,omitempty"`
	Period           string   `json:"period"`
	Policy           string   `json:"policy"`
	Unlimited        bool     `json:"unlimited"`
	LimitUSD         float64  `json:"limitUsd"`
	SpentUSD         *float64 `json:"spentUsd"`
	ReservedUSD      *float64 `json:"reservedUsd,omitempty"`
	RemainingUSD     *float64 `json:"remainingUsd,omitempty"`
	Percent          *float64 `json:"percent,omitempty"`
	PeriodEnd        string   `json:"periodEnd,omitempty"`
	ReportedAt       string   `json:"reportedAt,omitempty"`
	LastRequestUSD   *float64 `json:"lastRequestUsd,omitempty"`
	Exhausted        bool     `json:"exhausted"`
	StopsBeforeLimit bool     `json:"stopsBeforeLimit"`
	Stale            bool     `json:"stale"`
	NoReport         bool     `json:"noReport,omitempty"`
}

func (h *Handler) handleAIGatewayBudgets(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	text := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(strings.ToLower(v)) }
	minPercent := -1.0
	if v := text("minPercent"); v != "" {
		p, err := strconv.ParseFloat(v, 64)
		if err != nil || p < 0 {
			return mcp.NewToolResultError(`Parameter validation failed: "minPercent" must be a number, 0 or more`), nil
		}
		minPercent = p
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetReadOnly(ctx, "/api/v1/ai-gateway/budgets", nil)
	if err != nil {
		h.logger.ErrorContext(ctx, "AI Gateway budgets failed", logpkg.ErrAttr(err))
		return mcp.NewToolResultError(err.Error()), nil
	}
	var envelope struct {
		Data []struct {
			ID        string  `json:"id"`
			ParentID  string  `json:"parentId"`
			Name      string  `json:"name"`
			Level     string  `json:"level"`
			LimitUSD  float64 `json:"limitUsd"`
			Period    string  `json:"period"`
			Unlimited bool    `json:"unlimited"`
			Policy    string  `json:"policy"`
			Usage     *struct {
				SpentUSD         float64  `json:"spentUsd"`
				ReservedUSD      float64  `json:"reservedUsd"`
				RemainingUSD     *float64 `json:"remainingUsd"`
				Percent          *float64 `json:"percent"`
				PeriodEnd        string   `json:"periodEnd"`
				ReportedAt       string   `json:"reportedAt"`
				Stale            bool     `json:"stale"`
				LastRequestUSD   *float64 `json:"lastRequestUsd"`
				StopsBeforeLimit bool     `json:"stopsBeforeLimit"`
			} `json:"usage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return mcp.NewToolResultError("failed to parse response: " + err.Error()), nil
	}
	names := map[string]string{}
	for _, b := range envelope.Data {
		names[b.ID] = b.Name
	}
	budgets := []aiBudget{}
	warnings := map[string][]string{"exhausted": {}, "stopsBeforeLimit": {}, "stale": {}, "noReport": {}}
	for _, b := range envelope.Data {
		if l := text("level"); l != "" && b.Level != l {
			continue
		}
		if n := text("name"); n != "" && !strings.Contains(strings.ToLower(b.Name), n) {
			continue
		}
		out := aiBudget{Name: b.Name, Level: b.Level, Parent: names[b.ParentID], Period: b.Period, Policy: b.Policy,
			Unlimited: b.Unlimited, LimitUSD: b.LimitUSD}
		if u := b.Usage; u != nil {
			spent, reserved := u.SpentUSD, u.ReservedUSD
			out.SpentUSD, out.ReservedUSD, out.RemainingUSD, out.Percent = &spent, &reserved, u.RemainingUSD, u.Percent
			out.PeriodEnd, out.ReportedAt, out.LastRequestUSD = u.PeriodEnd, u.ReportedAt, u.LastRequestUSD
			out.Stale, out.StopsBeforeLimit = u.Stale, u.StopsBeforeLimit
			out.Exhausted = u.Percent != nil && *u.Percent >= 100
		} else {
			out.NoReport = true
		}
		if minPercent >= 0 && (out.Percent == nil || *out.Percent < minPercent) {
			continue
		}
		for key, on := range map[string]bool{"exhausted": out.Exhausted, "stopsBeforeLimit": out.StopsBeforeLimit, "stale": out.Stale, "noReport": out.NoReport} {
			if on {
				warnings[key] = append(warnings[key], out.Name)
			}
		}
		budgets = append(budgets, out)
	}
	percent := func(b aiBudget) float64 {
		if b.Percent == nil {
			return -1
		}
		return *b.Percent
	}
	sort.SliceStable(budgets, func(i, j int) bool {
		if percent(budgets[i]) != percent(budgets[j]) {
			return percent(budgets[i]) > percent(budgets[j])
		}
		return budgets[i].Name < budgets[j].Name
	})
	for _, list := range warnings {
		sort.Strings(list)
	}
	return fleetResult(map[string]any{"budgets": budgets, "warnings": warnings, "total": len(budgets)})
}
