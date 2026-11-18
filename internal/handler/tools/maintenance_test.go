package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bylonis/bylonis-mcp-server/internal/client"
	"github.com/bylonis/bylonis-mcp-server/pkg/types"
	"github.com/bylonis/bylonis-mcp-server/pkg/util"
	"github.com/mark3labs/mcp-go/mcp"
)

const otherRuleUUIDv7 = "0196634d-5d66-75c4-b778-e317f49dab7b"

func rulesJSON() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"status":"success","data":[{"id":%q,"alert":"Latencia alta"},{"id":%q,"alert":"Errores 5xx"}]}`, validRuleUUIDv7, otherRuleUUIDv7))
}

func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].(mcp.TextContent).Text
}

func TestCreateMaintenanceWindow(t *testing.T) {
	var sent map[string]any
	mock := &client.MockClient{
		ListAlertRulesFn: func(ctx context.Context) (json.RawMessage, error) { return rulesJSON(), nil },
		CreateDowntimeScheduleFn: func(ctx context.Context, body []byte) (json.RawMessage, error) {
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Fatal(err)
			}
			return json.RawMessage(`{"id":"mw1"}`), nil
		},
	}
	h := newTestHandler(mock)
	ctx := util.SetClientSource(testCtx(), "paperclip-lum")
	before := time.Now().UTC().Add(-time.Second)
	r, err := h.handleCreateMaintenanceWindow(ctx, makeToolRequest("bylonis_create_maintenance_window", map[string]any{
		"ruleIds":  validRuleUUIDv7 + ", " + otherRuleUUIDv7,
		"duration": "2h",
		"reason":   "Reinicio planificado del nodo de base de datos",
	}))
	if err != nil || r.IsError {
		t.Fatalf("unexpected error: %v %s", err, resultText(t, r))
	}
	ids := sent["alertIds"].([]any)
	if len(ids) != 2 || ids[0] != validRuleUUIDv7 || ids[1] != otherRuleUUIDv7 {
		t.Errorf("alertIds = %v", ids)
	}
	if d := sent["description"].(string); !strings.Contains(d, "Reinicio planificado") || !strings.Contains(d, "origen paperclip-lum") {
		t.Errorf("description = %q", d)
	}
	if n := sent["name"].(string); !strings.HasPrefix(n, "MCP · Reinicio") {
		t.Errorf("name = %q", n)
	}
	sch := sent["schedule"].(map[string]any)
	start, _ := time.Parse(time.RFC3339, sch["startTime"].(string))
	end, _ := time.Parse(time.RFC3339, sch["endTime"].(string))
	if start.Before(before.Truncate(time.Second)) || end.Sub(start) != 2*time.Hour || sch["timezone"] != "UTC" {
		t.Errorf("schedule = %v", sch)
	}
	if out := resultText(t, r); !strings.Contains(out, "Latencia alta") || !strings.Contains(out, "Errores 5xx") {
		t.Errorf("result should name the rules: %s", out)
	}
}

func TestCreateMaintenanceWindow_Limits(t *testing.T) {
	called := false
	mock := &client.MockClient{
		ListAlertRulesFn: func(ctx context.Context) (json.RawMessage, error) { return rulesJSON(), nil },
		CreateDowntimeScheduleFn: func(ctx context.Context, body []byte) (json.RawMessage, error) {
			called = true
			return json.RawMessage(`{}`), nil
		},
	}
	h := newTestHandler(mock)
	ok := map[string]any{"ruleIds": validRuleUUIDv7, "duration": "30m", "reason": "Intervención planificada en el nodo"}
	cases := map[string]map[string]any{
		"sin reglas (silenciaría todas)": {"ruleIds": " , ", "duration": "30m", "reason": ok["reason"]},
		"regla no UUIDv7":                {"ruleIds": "abc", "duration": "30m", "reason": ok["reason"]},
		"regla que no existe":            {"ruleIds": "0196634d-5d66-75c4-b778-e317f49dab7c", "duration": "30m", "reason": ok["reason"]},
		"más de 24h":                     {"ruleIds": validRuleUUIDv7, "duration": "25h", "reason": ok["reason"]},
		"duración sin unidad":            {"ruleIds": validRuleUUIDv7, "duration": "30", "reason": ok["reason"]},
		"sin motivo":                     {"ruleIds": validRuleUUIDv7, "duration": "30m", "reason": "corto"},
		"inicio en el pasado":            {"ruleIds": validRuleUUIDv7, "duration": "30m", "reason": ok["reason"], "start": "2020-01-01T00:00:00Z"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := h.handleCreateMaintenanceWindow(testCtx(), makeToolRequest("bylonis_create_maintenance_window", args))
			if err != nil || !r.IsError {
				t.Fatalf("expected an error result, got %v %s", err, resultText(t, r))
			}
		})
	}
	if called {
		t.Error("no window should have been created")
	}
}

func TestDeleteMaintenanceWindow(t *testing.T) {
	var got string
	h := newTestHandler(&client.MockClient{DeleteDowntimeScheduleFn: func(ctx context.Context, id string) error { got = id; return nil }})
	r, err := h.handleDeleteMaintenanceWindow(testCtx(), makeToolRequest("bylonis_delete_maintenance_window", map[string]any{"id": "mw1"}))
	if err != nil || r.IsError || got != "mw1" {
		t.Fatalf("unexpected: %v %s %q", err, resultText(t, r), got)
	}
	r, _ = h.handleDeleteMaintenanceWindow(testCtx(), makeToolRequest("bylonis_delete_maintenance_window", map[string]any{}))
	if !r.IsError {
		t.Error("expected an error without id")
	}
}

func alertsJSON(alerts ...[2]string) json.RawMessage {
	data := make([]map[string]any, 0, len(alerts))
	for _, a := range alerts {
		data = append(data, map[string]any{
			"labels":      map[string]string{"alertname": "Latencia alta", "ruleId": validRuleUUIDv7},
			"annotations": map[string]string{"bylonis_series": a[0], "bylonis_fired_at": a[1]},
			"status":      map[string]string{"state": "active"},
			"startsAt":    "2026-11-18T22:00:00Z",
		})
	}
	b, _ := json.Marshal(map[string]any{"status": "success", "data": data})
	return b
}

func TestAcknowledgeAlert_SingleFiringAlert(t *testing.T) {
	var sent map[string]any
	var filter []string
	mock := &client.MockClient{
		ListAlertsFn: func(ctx context.Context, p types.ListAlertsParams) (json.RawMessage, error) {
			filter = p.Filter
			return alertsJSON([2]string{"123456", "1795039072000"}), nil
		},
		AcknowledgeAlertFn: func(ctx context.Context, body []byte) (json.RawMessage, error) {
			_ = json.Unmarshal(body, &sent)
			return json.RawMessage(`{"status":"success","data":{"id":"ack1"}}`), nil
		},
	}
	h := newTestHandler(mock)
	ctx := util.SetClientSource(testCtx(), "paperclip-lum")
	r, err := h.handleAcknowledgeAlert(ctx, makeToolRequest("bylonis_acknowledge_alert", map[string]any{"ruleId": validRuleUUIDv7, "note": "Cluster investiga la latencia de checkout"}))
	if err != nil || r.IsError {
		t.Fatalf("unexpected: %v %s", err, resultText(t, r))
	}
	if len(filter) != 1 || filter[0] != fmt.Sprintf("ruleId=%q", validRuleUUIDv7) {
		t.Errorf("filter = %v", filter)
	}
	if sent["series"] != "123456" || sent["firedAt"] != float64(1795039072000) || sent["ruleId"] != validRuleUUIDv7 {
		t.Errorf("sent = %v", sent)
	}
	if n := sent["note"].(string); !strings.Contains(n, "Cluster investiga") || !strings.Contains(n, "origen paperclip-lum") {
		t.Errorf("note = %q", n)
	}
}

func TestAcknowledgeAlert_SeveralFiringAlertsListsCandidates(t *testing.T) {
	called := false
	mock := &client.MockClient{
		ListAlertsFn: func(ctx context.Context, p types.ListAlertsParams) (json.RawMessage, error) {
			return alertsJSON([2]string{"1", "1000"}, [2]string{"2", "2000"}), nil
		},
		AcknowledgeAlertFn: func(ctx context.Context, body []byte) (json.RawMessage, error) { called = true; return nil, nil },
	}
	h := newTestHandler(mock)
	r, _ := h.handleAcknowledgeAlert(testCtx(), makeToolRequest("bylonis_acknowledge_alert", map[string]any{"ruleId": validRuleUUIDv7, "note": "Cluster investiga la latencia"}))
	if !r.IsError || !strings.Contains(resultText(t, r), "2 firing alerts") || called {
		t.Fatalf("expected the candidates, got %s (called=%v)", resultText(t, r), called)
	}
	// Con series y firedAt no hace falta buscar.
	mock.ListAlertsFn = func(ctx context.Context, p types.ListAlertsParams) (json.RawMessage, error) {
		t.Error("should not list alerts")
		return nil, nil
	}
	r, _ = h.handleAcknowledgeAlert(testCtx(), makeToolRequest("bylonis_acknowledge_alert", map[string]any{"ruleId": validRuleUUIDv7, "note": "Cluster investiga la latencia", "series": "2", "firedAt": "2000"}))
	if r.IsError || !called {
		t.Fatalf("unexpected: %s", resultText(t, r))
	}
}

func TestAcknowledgeAlert_Validation(t *testing.T) {
	h := newTestHandler(&client.MockClient{
		ListAlertsFn: func(ctx context.Context, p types.ListAlertsParams) (json.RawMessage, error) { return alertsJSON(), nil },
	})
	for name, args := range map[string]map[string]any{
		"regla no UUIDv7":        {"ruleId": "x", "note": "Cluster investiga la latencia"},
		"sin nota":               {"ruleId": validRuleUUIDv7, "note": "ok"},
		"series sin firedAt":     {"ruleId": validRuleUUIDv7, "note": "Cluster investiga la latencia", "series": "1"},
		"ninguna alerta dispara": {"ruleId": validRuleUUIDv7, "note": "Cluster investiga la latencia"},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := h.handleAcknowledgeAlert(testCtx(), makeToolRequest("bylonis_acknowledge_alert", args))
			if err != nil || !r.IsError {
				t.Fatalf("expected an error result, got %v %s", err, resultText(t, r))
			}
		})
	}
}
