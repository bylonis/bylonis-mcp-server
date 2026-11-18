package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
	"github.com/bylonis/bylonis-mcp-server/pkg/alert"
	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
	"github.com/bylonis/bylonis-mcp-server/pkg/types"
)

// What ByLonis already knows about the alerts, the changes and the logs (bylonis#1024): the rules that flap, who
// acknowledged the alerts, the maintenance windows, the change events, the patterns of the logs and the replay of a
// rule. All read only: acknowledging an alert or creating a maintenance window is for a person in ByLonis.

const (
	// The limits of ByLonis: the days of the rules that flap and of the acknowledgements, the range of the change
	// events, the patterns of the logs and the shift of their comparison.
	flappingMaxDays        = 90
	acknowledgementMaxDays = 90
	changeEventsMaxSpan    = 31 * 24 * time.Hour
	logPatternsDefault     = 50
	logPatternsMax         = 1000
	logPatternsMaxShift    = 30 * 24 * time.Hour
	backtestMaxDays        = 14
	// logPatternSampleChars keeps a long line of a pattern from filling the answer.
	logPatternSampleChars = 300
)

func (h *Handler) RegisterInsightHandlers(s *server.MCPServer) {
	h.logger.Debug("Registering alert quality, change and log pattern handlers")

	searchContext := mcp.WithString("searchContext", mcp.Description("The user's original question or search text that triggered this tool call. Always include the user's raw query here for better results."))
	readOnly := func(name, description string, options ...mcp.ToolOption) mcp.Tool {
		return mcp.NewTool(name, append([]mcp.ToolOption{mcp.WithReadOnlyHintAnnotation(true), mcp.WithDestructiveHintAnnotation(false),
			searchContext, mcp.WithDescription(description)}, options...)...)
	}
	timeOptions := func(fallback string) []mcp.ToolOption {
		return []mcp.ToolOption{
			mcp.WithString("timeRange", mcp.Description("How far back from end: '<n>m', '<n>h' or '<n>d'. Default '"+fallback+"'. Ignored with start.")),
			mcp.WithString("start", mcp.Description("Start, RFC3339 or milliseconds since epoch.")),
			mcp.WithString("end", mcp.Description("End, RFC3339 or milliseconds since epoch. Default now.")),
		}
	}

	addTool(s, readOnly("bylonis_list_flapping_rules",
		"Lists the alert rules that flap: in the last days, at least 10 alerts and more than half of them shorter than 5 minutes. Use it for 'which rules make noise?' or 'there are too many alerts'.\n\n"+
			"Per rule: alerts, shortAlerts and shortPercent, medianMinutes (how long its alerts last), holdMinutes (the holdDuration it has now), fleet (true for a rule of a fleet), "+
			"holdOptions (for each wait before firing, how many of its alerts it would have avoided) and suggestions (kind 'hold' with the minutes ByLonis proposes, 'sql' when the query itself should change).\n\n"+
			"To apply a hold, read the rule with bylonis_get_alert and send it whole to bylonis_update_alert with holdDuration (e.g. '10m'); a field left out is lost. Change a rule only when the user asks. "+
			"A rule with fleet=true is managed by its fleet and ByLonis refuses to change it: say that it is changed in Inventario → Alertas.",
		mcp.WithString("days", mcp.Description("Days to look at, 1 to 90. Default 7."))),
		h.handleListFlappingRules)

	addTool(s, readOnly("bylonis_list_alert_acknowledgements",
		"Lists who acknowledged the alerts that started firing in the last days, newest first: the rule, the series and when it started firing (firedAt, ms), who acknowledged it, when and with what note. "+
			"It covers the alerts already resolved; for the alerts firing now, bylonis_list_alerts gives the same fields.\n\n"+
			"An acknowledged alert is not notified again while its group does not change. Acknowledging is for a person in ByLonis: this MCP cannot do it.",
		mcp.WithString("ruleId", mcp.Description("Id of a rule; all the rules if omitted.")),
		mcp.WithString("days", mcp.Description("Days to look at, 1 to 90. Default 30."))),
		h.handleListAlertAcknowledgements)

	addTool(s, readOnly("bylonis_list_maintenance_windows",
		"Lists the planned maintenance windows: while one is active, the rules it covers are evaluated but do not notify. Use it for 'why did this rule not notify?' or 'is there a maintenance now?'.\n\n"+
			"Per window: name, description, status (active, upcoming or expired), kind (fixed or recurring), schedule (timezone, start and end, or the recurrence: duration, repeatType, repeatOn), "+
			"the rules it covers by name (allRules=true when it covers every rule) and who created it. Expired windows are left out unless includeExpired is 'true'.\n\n"+
			"Creating or changing a window is for a person in ByLonis.",
		mcp.WithString("ruleId", mcp.Description("Keep the windows that cover this rule.")),
		mcp.WithString("status", mcp.Description("active, upcoming or expired.")),
		mcp.WithString("includeExpired", mcp.Description("'true' to include the expired windows."))),
		h.handleListMaintenanceWindows)

	addTool(s, readOnly("bylonis_list_change_events", changeEventsDescription,
		append([]mcp.ToolOption{
			mcp.WithString("service", mcp.Description("service.name of a service.")),
			mcp.WithString("environment", mcp.Description("deployment.environment of the service.")),
			mcp.WithString("kinds", mcp.Description("Some of deploy, alert_firing, alert_resolved and fleet_config, comma separated. All if omitted.")),
		}, timeOptions("24h")...)...),
		h.handleListChangeEvents)

	addTool(s, readOnly("bylonis_get_log_patterns",
		"Groups the logs of a range by message template, with the variable parts masked (<ts>, <uuid>, <ip>, <hex>, <n>), most frequent first. Use it before bylonis_search_logs for 'what errors are there?' or 'what is new in the logs?': one call summarises thousands of lines.\n\n"+
			"Per pattern: the template, count, levels (lines by severity), a sample line, firstSeen and lastSeen (ms), and regex, an RE2 expression for the filter 'body REGEXP' of bylonis_search_logs to read its lines.\n\n"+
			"'compareShift' (e.g. '24h', at most '30d') also counts each pattern in the range that far back: prevCount. A pattern without prevCount was not among the most frequent of that range, so it is new or much more frequent now; 'prevLinesTotal' is the lines of that range.\n\n"+
			"'exact' false (the default) lets ByLonis sample a range with many lines: then method is 'sample' and the counts are estimates (sampleRate). 'exact' true reads every line, slower.",
		append([]mcp.ToolOption{
			mcp.WithString("filter", mcp.Description("Filter expression of the logs explorer, e.g. \"service.name = 'api' AND severity_text IN ('ERROR', 'FATAL')\". Prefer resource attributes.")),
			mcp.WithString("limit", mcp.Description("Maximum patterns. Default 50, max 1000.")),
			mcp.WithString("compareShift", mcp.Description("Compare with the range this far back: '<n>m', '<n>h' or '<n>d', at most '30d'.")),
			mcp.WithString("exact", mcp.Description("'true' for exact counts, without sampling.")),
		}, timeOptions("1h")...)...),
		h.handleGetLogPatterns)

	backtestTool := mcp.NewTool("bylonis_backtest_alert",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithDescription("Replays an alert rule over the last days (7 by default, up to 14) and says how often it would have fired, without creating it or notifying anyone. "+
			"Use it before bylonis_create_alert to judge a proposed rule, or before bylonis_update_alert to judge a change (a threshold, a holdDuration).\n\n"+
			"Takes the same fields as bylonis_create_alert (read bylonis://alert/instructions); preferredChannels are not needed. To test a change of a saved rule, read it with bylonis_get_alert and pass it whole with the change.\n\n"+
			"Returns: supported (false, with reason, when the rule cannot be replayed: a PromQL rule, or a SQL rule that uses now()), evaluations, series, firings, shortFirings and shortPercent (shorter than 5 minutes: flapping), "+
			"seriesFiring, firingMinutes, firingAtEnd, truncated (the replay hit its limits: 200 series) and latest (the last firings with their labels, start, end, minutes and value).\n\n"+
			"ByLonis asks for the editor role for this, although it saves nothing: with a viewer key it answers 403."),
		mcp.WithInputSchema[types.BacktestAlertInput](),
	)
	addTool(s, backtestTool, h.handleBacktestAlert)

	h.registerFleetUpgradeHandlers(s, readOnly)
}

const changeEventsDescription = "Lists the events of a range that the charts of ByLonis mark, oldest first: deploys (posted by CI/CD, or seen in the telemetry when a service starts reporting a new service.version or a new ReplicaSet), alerts that fired or resolved, and deployments of a configuration of a fleet. " +
	"Use it for 'what changed before the incident?' of a service, with a range that covers the incident; for a machine, bylonis_get_fleet_node_history.\n\n" +
	"Per event: kind (deploy, alert_firing, alert_resolved, fleet_config), source, timestamp (ms), title, and service, environment, version, url and actor when it has them. 'byKind' counts them. " +
	"At most the latest 500 of the range, which is at most 31 days."

// insightRange reads the range of a call: start and end, or timeRange back from end.
func insightRange(args fleetArgs, fallback string, maxSpan time.Duration) (time.Time, time.Time, error) {
	end := time.Now().UTC()
	var err error
	if text := args.text("end"); text != "" {
		if end, err = fleetTime(text); err != nil {
			return time.Time{}, time.Time{}, errors.New(`Invalid "end": use RFC3339 (2026-10-29T10:00:00Z) or milliseconds since epoch`)
		}
	}
	var start time.Time
	if text := args.text("start"); text != "" {
		if start, err = fleetTime(text); err != nil {
			return time.Time{}, time.Time{}, errors.New(`Invalid "start": use RFC3339 (2026-10-29T10:00:00Z) or milliseconds since epoch`)
		}
	} else {
		timeRange := args.text("timeRange")
		if timeRange == "" {
			timeRange = fallback
		}
		span, err := fleetDuration(timeRange)
		if err != nil || span <= 0 {
			return time.Time{}, time.Time{}, fmt.Errorf(`Invalid "timeRange": use '<n>m', '<n>h' or '<n>d', e.g. '%s'`, fallback)
		}
		start = end.Add(-span)
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, errors.New(`Invalid range: "end" must be after "start"`)
	}
	if maxSpan > 0 && end.Sub(start) > maxSpan {
		return time.Time{}, time.Time{}, fmt.Errorf("Invalid range: at most %d days", int(maxSpan.Hours()/24))
	}
	return start, end, nil
}

// days reads a number of days from 1 to max; 0 when it is not given, for the default of ByLonis.
func (args fleetArgs) days(max int) (int, error) {
	text := args.text("days")
	if text == "" {
		return 0, nil
	}
	days, err := strconv.Atoi(text)
	if err != nil || days < 1 || days > max {
		return 0, fmt.Errorf(`Parameter validation failed: "days" must be a number from 1 to %d`, max)
	}
	return days, nil
}

func (h *Handler) insightError(ctx context.Context, tool string, err error) (*mcp.CallToolResult, error) {
	h.logger.ErrorContext(ctx, "Tool failed", slog.String("tool", tool), logpkg.ErrAttr(err))
	message := err.Error()
	var notThere bylonisclient.ErrNotInThisVersion
	switch {
	case errors.As(err, &notThere):
		message += ". Tell the user that the tenant has to be upgraded for this; do not try another tool to fake it."
	case strings.Contains(message, "unexpected status 403"):
		message += " (the API key of this MCP does not have the role ByLonis asks for this)"
	}
	return mcp.NewToolResultError(message), nil
}

func (h *Handler) handleListFlappingRules(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	days, err := args.days(flappingMaxDays)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query := url.Values{}
	if days > 0 {
		query.Set("days", strconv.Itoa(days))
	}
	raw, err := client.GetReadOnly(ctx, "/api/v2/alert_quality/flapping", query)
	if err != nil {
		return h.insightError(ctx, "bylonis_list_flapping_rules", err)
	}
	flapping, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if rules, ok := flapping["rules"].([]any); ok {
		flapping["total"] = len(rules)
	}
	return fleetResult(flapping)
}

func (h *Handler) handleListAlertAcknowledgements(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	days, err := args.days(acknowledgementMaxDays)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query := url.Values{}
	if days > 0 {
		query.Set("days", strconv.Itoa(days))
	}
	if ruleID := args.text("ruleId"); ruleID != "" {
		query.Set("ruleId", ruleID)
	}
	raw, err := client.GetReadOnly(ctx, "/api/v2/alert_acknowledgements", query)
	if err != nil {
		return h.insightError(ctx, "bylonis_list_alert_acknowledgements", err)
	}
	acknowledgements, err := fleetData[[]map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	// The same names as bylonis_list_alerts gives an acknowledged alert.
	out := make([]map[string]any, 0, len(acknowledgements))
	for _, ack := range acknowledgements {
		out = append(out, map[string]any{
			"ruleId": ack["ruleId"], "ruleName": ack["ruleName"], "series": ack["series"], "firedAt": ack["firedAt"],
			"acknowledgedBy": ack["createdBy"], "acknowledgedAt": ack["createdAt"], "acknowledgementNote": ack["note"],
		})
	}
	return fleetResult(map[string]any{"acknowledgements": out, "total": len(out)})
}

func (h *Handler) handleListMaintenanceWindows(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	status := strings.ToLower(args.text("status"))
	if status != "" && status != "active" && status != "upcoming" && status != "expired" {
		return mcp.NewToolResultError(`Parameter validation failed: "status" is active, upcoming or expired`), nil
	}
	includeExpired := args.text("includeExpired") == "true" || status == "expired"
	ruleID := args.text("ruleId")
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetReadOnly(ctx, "/api/v1/downtime_schedules", nil)
	if err != nil {
		return h.insightError(ctx, "bylonis_list_maintenance_windows", err)
	}
	windows, err := fleetData[[]map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ruleNames := h.ruleNames(ctx, client)
	out := make([]map[string]any, 0, len(windows))
	for _, window := range windows {
		windowStatus, _ := window["status"].(string)
		if status != "" && windowStatus != status || !includeExpired && windowStatus == "expired" {
			continue
		}
		ids, _ := window["alertIds"].([]any)
		if ruleID != "" && len(ids) > 0 && !containsValue(ids, ruleID) {
			continue
		}
		row := map[string]any{"allRules": len(ids) == 0}
		for _, key := range []string{"id", "name", "description", "status", "kind", "schedule", "createdBy", "createdAt"} {
			if value, ok := window[key]; ok && value != nil && value != "" {
				row[key] = value
			}
		}
		if len(ids) > 0 {
			rules := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				id, _ := id.(string)
				rule := map[string]any{"ruleId": id}
				if name, ok := ruleNames[id]; ok {
					rule["name"] = name
				}
				rules = append(rules, rule)
			}
			row["rules"] = rules
		}
		out = append(out, row)
	}
	return fleetResult(map[string]any{"windows": out, "total": len(out)})
}

// ruleNames maps the ids of the rules to their names; empty when the rules cannot be read.
func (h *Handler) ruleNames(ctx context.Context, client bylonisclient.Client) map[string]string {
	names := map[string]string{}
	raw, err := client.ListAlertRules(ctx)
	if err != nil {
		return names
	}
	var rules types.APIAlertRulesResponse
	if err := json.Unmarshal(raw, &rules); err != nil {
		return names
	}
	for _, rule := range rules.Data {
		names[rule.ID] = rule.Alert
	}
	return names
}

func containsValue(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (h *Handler) handleListChangeEvents(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	start, end, err := insightRange(args, "24h", changeEventsMaxSpan)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query := url.Values{"start": {strconv.FormatInt(start.UnixMilli(), 10)}, "end": {strconv.FormatInt(end.UnixMilli(), 10)}}
	for _, key := range []string{"service", "environment"} {
		if value := args.text(key); value != "" {
			query.Set(key, value)
		}
	}
	if kinds := args.text("kinds"); kinds != "" {
		query.Set("kinds", strings.ReplaceAll(kinds, " ", ""))
	}
	raw, err := client.GetReadOnly(ctx, "/api/v1/events", query)
	if err != nil {
		return h.insightError(ctx, "bylonis_list_change_events", err)
	}
	page, err := fleetData[struct {
		Events []map[string]any `json:"events"`
	}](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	byKind := map[string]int{}
	for _, event := range page.Events {
		delete(event, "id")
		if kind, ok := event["kind"].(string); ok {
			byKind[kind]++
		}
	}
	if page.Events == nil {
		page.Events = []map[string]any{}
	}
	return fleetResult(map[string]any{
		"events": page.Events, "total": len(page.Events), "byKind": byKind,
		"start": start.UnixMilli(), "end": end.UnixMilli(),
	})
}

func (h *Handler) handleGetLogPatterns(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	start, end, err := insightRange(args, "1h", 0)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit, err := args.number("limit", logPatternsDefault, logPatternsMax)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	body := map[string]any{"start": start.UnixMilli(), "end": end.UnixMilli(), "limit": limit}
	if filter := args.text("filter"); filter != "" {
		body["filter"] = map[string]any{"expression": filter}
	}
	if text := args.text("compareShift"); text != "" {
		shift, err := fleetDuration(text)
		if err != nil || shift <= 0 || shift > logPatternsMaxShift {
			return mcp.NewToolResultError(`Invalid "compareShift": use '<n>m', '<n>h' or '<n>d', at most '30d', e.g. '24h'`), nil
		}
		// ByLonis takes the shift in seconds.
		body["compareShift"] = int64(shift.Seconds())
	}
	if args.text("exact") == "true" {
		body["exact"] = true
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	answer, err := client.PostReadOnly(ctx, "/api/v5/logs/patterns", nil, raw)
	if err != nil {
		return h.insightError(ctx, "bylonis_get_log_patterns", err)
	}
	patterns, err := fleetData[map[string]any](answer)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if list, ok := patterns["patterns"].([]any); ok {
		for _, pattern := range list {
			if pattern, ok := pattern.(map[string]any); ok {
				if sample, ok := pattern["sample"].(string); ok && utf8.RuneCountInString(sample) > logPatternSampleChars {
					pattern["sample"] = string([]rune(sample)[:logPatternSampleChars]) + "…"
				}
			}
		}
	}
	patterns["start"], patterns["end"] = start.UnixMilli(), end.UnixMilli()
	return fleetResult(patterns)
}

func (h *Handler) handleBacktestAlert(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rule, ok := req.Params.Arguments.(map[string]any)
	if !ok || len(rule) == 0 {
		return mcp.NewToolResultError(`Parameter validation failed: give the rule to replay, with the fields of bylonis_create_alert.`), nil
	}
	query := url.Values{}
	if value, ok := rule["days"]; ok {
		days, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		if err != nil || days < 1 || days > backtestMaxDays {
			return mcp.NewToolResultError(fmt.Sprintf(`Parameter validation failed: "days" must be a number from 1 to %d`, backtestMaxDays)), nil
		}
		query.Set("days", strconv.Itoa(days))
	}
	delete(rule, "days")
	delete(rule, "searchContext")
	// The same validation as bylonis_create_alert, without the channels: the replay notifies nobody.
	cleanJSON, err := alert.ValidateFromMap(rule)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Alert validation error: %s", err.Error())), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.PostReadOnly(ctx, "/api/v1/rules/backtest", query, cleanJSON)
	if err != nil {
		return h.insightError(ctx, "bylonis_backtest_alert", err)
	}
	result, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return fleetResult(result)
}
