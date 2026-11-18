package tools

// Ventanas de mantenimiento (silencios) y reconocimientos de alertas (bylonis#1052): para que un agente (p. ej. de PAIC) silencie las alertas
// de una intervención o diga que una alerta ya está atendida. Llevan anotaciones de escritura para que el cliente
// MCP las trate como tales (en PAIC, «Ask first» con aprobación humana).
//
// Límites propios del MCP, más estrictos que la API:
//   - un silencio siempre nombra sus reglas (la API, sin reglas, silencia TODAS), dura como mucho
//     maxDowntimeDuration, es una ventana fija y lleva un motivo;
//   - un reconocimiento lleva una nota, y la serie y el inicio salen de la alerta que dispara.
// El origen de la llamada (X-ByLonis-Client-Source) queda en la descripción o en la nota, para la auditoría.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
	"github.com/bylonis/bylonis-mcp-server/pkg/types"
	"github.com/bylonis/bylonis-mcp-server/pkg/util"
)

const (
	maxDowntimeDuration = 24 * time.Hour
	minReasonLength     = 10
)

func (h *Handler) RegisterMaintenanceHandlers(s *server.MCPServer) {
	h.logger.Debug("Registering maintenance window and acknowledgement handlers")

	addTool(s, mcp.NewTool("bylonis_create_maintenance_window",
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithDescription("Creates a maintenance window: silences the notifications of some alert rules for a fixed time, e.g. during a planned intervention (POST /api/v1/downtime_schedules). "+
			"While it is active the rules still evaluate and keep their history, but their channels are not notified.\n\n"+
			"LIMITS: name the rules (ruleIds, from bylonis_list_alert_rules): a silence of every rule is not allowed here. "+
			"At most 24h. The window starts now, or at 'start' if given (never in the past). A reason is required and is saved with the silence, with the origin of the call.\n\n"+
			"Needs the editor role in ByLonis: with a viewer key it fails with 403. Confirm the rules and the window with the user before calling. "+
			"Check existing windows first with bylonis_list_maintenance_windows (to avoid a duplicate); undo it with bylonis_delete_maintenance_window."),
		mcp.WithString("ruleIds", mcp.Required(), mcp.Description("Comma-separated alert rule IDs (UUIDv7) to silence.")),
		mcp.WithString("duration", mcp.Required(), mcp.Description("How long, as <number><unit> with unit m or h, e.g. '30m', '2h'. Max 24h.")),
		mcp.WithString("reason", mcp.Required(), mcp.Description("Why, in a sentence (at least 10 characters): e.g. 'Reinicio planificado del nodo de base de datos (cambio CHG-123)'.")),
		mcp.WithString("start", mcp.Description("Optional start, RFC 3339 (e.g. 2026-11-19T22:00:00Z). Default: now.")),
		mcp.WithString("name", mcp.Description("Optional short name. Default: built from the reason.")),
	), h.handleCreateMaintenanceWindow)

	addTool(s, mcp.NewTool("bylonis_delete_maintenance_window",
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithDescription("Removes a maintenance window (DELETE /api/v1/downtime_schedules/{id}): its rules notify again. Needs the editor role in ByLonis. Get the id with bylonis_list_maintenance_windows."),
		mcp.WithString("id", mcp.Required(), mcp.Description("ID of the maintenance window.")),
	), h.handleDeleteMaintenanceWindow)

	addTool(s, mcp.NewTool("bylonis_acknowledge_alert",
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("Acknowledges a firing alert: says that someone works on it (POST /api/v2/alert_acknowledgements). "+
			"While acknowledged, its channel is not notified again unless another alert fires in its group or one resolves. An alert already acknowledged keeps its first acknowledgement.\n\n"+
			"Give the ruleId and a note (who works on it and what is being done). If the rule has several firing alerts, also give series and firedAt of the one to acknowledge, as bylonis_list_alerts returns them; "+
			"without them, the tool acknowledges the only firing alert of the rule or lists the candidates. The origin of the call is added to the note."),
		mcp.WithString("ruleId", mcp.Required(), mcp.Description("Alert rule ID (UUIDv7).")),
		mcp.WithString("note", mcp.Required(), mcp.Description("Who works on it and what is being done (at least 10 characters).")),
		mcp.WithString("series", mcp.Description("Series of the alert (field 'series' of bylonis_list_alerts). Needed when the rule has several firing alerts.")),
		mcp.WithString("firedAt", mcp.Description("Start of the alert in milliseconds (field 'firedAt' of bylonis_list_alerts). Needed with series.")),
	), h.handleAcknowledgeAlert)
}

// callerOrigin describe de dónde viene la llamada, para dejarlo en lo que se crea.
func callerOrigin(ctx context.Context) string {
	source, _ := util.GetClientSource(ctx)
	if source == "" {
		source = util.ClientSourceUserClient
	}
	return "vía MCP, origen " + source
}

func trimmedArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

// parseDowntimeDuration acepta <número><m|h>, de 1 minuto a maxDowntimeDuration.
func parseDowntimeDuration(s string) (time.Duration, error) {
	if len(s) < 2 || (s[len(s)-1] != 'm' && s[len(s)-1] != 'h') {
		return 0, fmt.Errorf("use <number><m|h>, e.g. '30m' or '2h'")
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("use <number><m|h>, e.g. '30m' or '2h'")
	}
	d := time.Duration(n) * time.Minute
	if s[len(s)-1] == 'h' {
		d = time.Duration(n) * time.Hour
	}
	if d > maxDowntimeDuration {
		return 0, fmt.Errorf("at most %s", maxDowntimeDuration)
	}
	return d, nil
}

func (h *Handler) handleCreateMaintenanceWindow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(`Parameter validation failed: expected an arguments object with "ruleIds", "duration" and "reason".`), nil
	}
	var ruleIDs []string
	for _, id := range strings.Split(trimmedArg(args, "ruleIds"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			if !util.IsUUIDv7(id) {
				return mcp.NewToolResultError(fmt.Sprintf(`Invalid "ruleIds": %q is not a UUIDv7.`, id)), nil
			}
			ruleIDs = append(ruleIDs, id)
		}
	}
	if len(ruleIDs) == 0 {
		return mcp.NewToolResultError(`"ruleIds" is required: name the rules to silence. A silence of every rule is not allowed with this MCP.`), nil
	}
	duration, err := parseDowntimeDuration(trimmedArg(args, "duration"))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(`Invalid "duration": %s.`, err.Error())), nil
	}
	reason := trimmedArg(args, "reason")
	if len([]rune(reason)) < minReasonLength {
		return mcp.NewToolResultError(fmt.Sprintf(`"reason" is required (at least %d characters): say why the alerts are silenced.`, minReasonLength)), nil
	}
	now := time.Now().UTC()
	start := now
	if raw := trimmedArg(args, "start"); raw != "" {
		start, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return mcp.NewToolResultError(`Invalid "start": use RFC 3339, e.g. 2026-11-19T22:00:00Z.`), nil
		}
		start = start.UTC()
		if start.Before(now.Add(-time.Minute)) {
			return mcp.NewToolResultError(`Invalid "start": it is in the past.`), nil
		}
	}
	name := trimmedArg(args, "name")
	if name == "" {
		name = reason
		if r := []rune(name); len(r) > 60 {
			name = string(r[:60]) + "…"
		}
		name = "MCP · " + name
	}

	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	// Las reglas tienen que existir: la API no lo comprueba y un id equivocado silenciaría nada.
	rulesBody, err := client.ListAlertRules(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("ByLonis API Error: %s", err.Error())), nil
	}
	var rules types.APIAlertRulesResponse
	if err := json.Unmarshal(rulesBody, &rules); err != nil {
		return mcp.NewToolResultError("failed to parse alert rules response: " + err.Error()), nil
	}
	known := map[string]string{}
	for _, r := range rules.Data {
		known[r.ID] = r.Alert
	}
	names := make([]string, 0, len(ruleIDs))
	for _, id := range ruleIDs {
		n, ok := known[id]
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf(`Rule %s does not exist: check it with bylonis_list_alert_rules.`, id)), nil
		}
		names = append(names, n)
	}

	payload := map[string]any{
		"name":        name,
		"description": fmt.Sprintf("%s (%s)", reason, callerOrigin(ctx)),
		"schedule": map[string]any{
			"timezone":  "UTC",
			"startTime": start.Format(time.RFC3339),
			"endTime":   start.Add(duration).Format(time.RFC3339),
		},
		"alertIds": ruleIDs,
	}
	body, _ := json.Marshal(payload)
	h.logger.InfoContext(ctx, "Tool called: bylonis_create_maintenance_window", slog.Any("ruleIds", ruleIDs), slog.Duration("duration", duration))
	resp, err := client.CreateDowntimeSchedule(ctx, body)
	if err != nil {
		h.logger.ErrorContext(ctx, "Failed to create downtime schedule", logpkg.ErrAttr(err))
		return mcp.NewToolResultError(fmt.Sprintf("ByLonis API Error: %s", err.Error())), nil
	}
	out, _ := json.Marshal(map[string]any{
		"status":            "success",
		"rules":             names,
		"startTime":         start.Format(time.RFC3339),
		"endTime":           start.Add(duration).Format(time.RFC3339),
		"maintenanceWindow": json.RawMessage(resp),
	})
	return mcp.NewToolResultText(string(out)), nil
}

func (h *Handler) handleDeleteMaintenanceWindow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	id := trimmedArg(args, "id")
	if id == "" {
		return mcp.NewToolResultError(`Parameter validation failed: "id" is required (see bylonis_list_maintenance_windows).`), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	h.logger.InfoContext(ctx, "Tool called: bylonis_delete_maintenance_window", slog.String("id", id))
	if err := client.DeleteDowntimeSchedule(ctx, id); err != nil {
		h.logger.ErrorContext(ctx, "Failed to delete downtime schedule", slog.String("id", id), logpkg.ErrAttr(err))
		return mcp.NewToolResultError(fmt.Sprintf("ByLonis API Error: %s", err.Error())), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(`{"status":"success","id":%q}`, id)), nil
}

type firingAlert struct {
	Series   string `json:"series"`
	FiredAt  string `json:"firedAt"`
	StartsAt string `json:"startsAt"`
	Name     string `json:"alertname"`
	AckedBy  string `json:"acknowledgedBy,omitempty"`
}

func (h *Handler) handleAcknowledgeAlert(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError(`Parameter validation failed: expected an arguments object with "ruleId" and "note".`), nil
	}
	ruleID := trimmedArg(args, "ruleId")
	if !util.IsUUIDv7(ruleID) {
		return mcp.NewToolResultError(`Invalid "ruleId": a UUIDv7 is required.`), nil
	}
	note := trimmedArg(args, "note")
	if len([]rune(note)) < minReasonLength {
		return mcp.NewToolResultError(fmt.Sprintf(`"note" is required (at least %d characters): who works on it and what is being done.`, minReasonLength)), nil
	}
	series, firedAtRaw := trimmedArg(args, "series"), trimmedArg(args, "firedAt")
	if (series == "") != (firedAtRaw == "") {
		return mcp.NewToolResultError(`Give both "series" and "firedAt", or neither.`), nil
	}

	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if series == "" {
		active, silenced, inhibited := true, true, true
		body, err := client.ListAlerts(ctx, types.ListAlertsParams{Active: &active, Silenced: &silenced, Inhibited: &inhibited, Filter: []string{fmt.Sprintf("ruleId=%q", ruleID)}})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("ByLonis API Error: %s", err.Error())), nil
		}
		var resp types.APIAlertsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return mcp.NewToolResultError("failed to parse alerts response: " + err.Error()), nil
		}
		var firing []firingAlert
		for _, a := range resp.Data {
			if a.Labels.RuleID != ruleID || a.Annotations["bylonis_series"] == "" || a.Annotations["bylonis_fired_at"] == "" {
				continue
			}
			firing = append(firing, firingAlert{Series: a.Annotations["bylonis_series"], FiredAt: a.Annotations["bylonis_fired_at"], StartsAt: a.StartsAt, Name: a.Labels.Alertname, AckedBy: a.Annotations["bylonis_acknowledged_by"]})
		}
		switch len(firing) {
		case 0:
			return mcp.NewToolResultError("The rule has no firing alert to acknowledge (check it with bylonis_list_alerts)."), nil
		case 1:
			series, firedAtRaw = firing[0].Series, firing[0].FiredAt
		default:
			candidates, _ := json.Marshal(firing)
			return mcp.NewToolResultError(fmt.Sprintf("The rule has %d firing alerts: call again with the series and firedAt of the one to acknowledge. Candidates: %s", len(firing), candidates)), nil
		}
	}
	firedAt, err := strconv.ParseInt(firedAtRaw, 10, 64)
	if err != nil {
		return mcp.NewToolResultError(`Invalid "firedAt": milliseconds expected.`), nil
	}
	body, _ := json.Marshal(map[string]any{
		"ruleId":  ruleID,
		"series":  series,
		"firedAt": firedAt,
		"note":    fmt.Sprintf("%s (%s)", note, callerOrigin(ctx)),
	})
	h.logger.InfoContext(ctx, "Tool called: bylonis_acknowledge_alert", slog.String("ruleId", ruleID), slog.String("series", series))
	resp, err := client.AcknowledgeAlert(ctx, body)
	if err != nil {
		h.logger.ErrorContext(ctx, "Failed to acknowledge alert", slog.String("ruleId", ruleID), logpkg.ErrAttr(err))
		return mcp.NewToolResultError(fmt.Sprintf("ByLonis API Error: %s", err.Error())), nil
	}
	return mcp.NewToolResultText(string(resp)), nil
}
