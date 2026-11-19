package alert

import (
	"encoding/json"
	"strings"
	"testing"
)

// anomalyKindRule es una anomaly_rule con el kind anomaly (bylonis#901): ventana de 15 min a 5 min (M = 3).
func anomalyKindRule() map[string]any {
	return map[string]any{
		"alert":     "Anomalous calls to checkout",
		"alertType": "METRIC_BASED_ALERT",
		"ruleType":  "anomaly_rule",
		"evaluation": map[string]any{
			"kind": "rolling",
			"spec": map[string]any{"evalWindow": "15m", "frequency": "5m"},
		},
		"condition": map[string]any{
			"compositeQuery": map[string]any{
				"queryType": "builder",
				"queries": []any{
					map[string]any{
						"type": "builder_query",
						"spec": map[string]any{
							"name":   "A",
							"signal": "metrics",
							"aggregations": []any{
								map[string]any{"metricName": "bylonis_calls_total", "timeAggregation": "rate", "spaceAggregation": "sum"},
							},
						},
					},
				},
			},
			"seasonality": "daily",
			"anomaly":     map[string]any{"resolution": "5m", "minExpected": 0.5},
			"thresholds": map[string]any{
				"kind": "anomaly",
				"spec": []any{
					map[string]any{"name": "critical", "z": float64(4), "direction": "both", "minAnomalous": float64(1)},
					map[string]any{"name": "warning", "z": float64(3), "recoveryZ": float64(2), "direction": "down", "minAnomalous": float64(2)},
				},
			},
		},
	}
}

func TestValidate_AnomalyKind_Accepted(t *testing.T) {
	out, err := ValidateFromMap(anomalyKindRule())
	if err != nil {
		t.Fatalf("expected the anomaly kind to validate, got: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}
	if parsed["schemaVersion"] != "v2alpha1" {
		t.Errorf("the anomaly kind is v2alpha1; got schemaVersion %v", parsed["schemaVersion"])
	}
	thresholds := parsed["condition"].(map[string]any)["thresholds"].(map[string]any)
	if thresholds["kind"] != "anomaly" || len(thresholds["spec"].([]any)) != 2 {
		t.Errorf("thresholds must pass through as they are; got %v", thresholds)
	}
}

func TestValidate_AnomalyKind_DefaultEvaluation(t *testing.T) {
	rule := anomalyKindRule()
	delete(rule, "evaluation")
	out, err := ValidateFromMap(rule)
	if err != nil {
		t.Fatalf("expected the anomaly kind without evaluation to validate, got: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(out, &parsed)
	spec := parsed["evaluation"].(map[string]any)["spec"].(map[string]any)
	if spec["evalWindow"] != "15m0s" || spec["frequency"] != "5m0s" {
		t.Errorf("an anomaly rule defaults to 15m every 5m (3 points); got %v", spec)
	}

	hourly := anomalyKindRule()
	delete(hourly, "evaluation")
	hourly["condition"].(map[string]any)["anomaly"] = map[string]any{"resolution": "1h"}
	out, err = ValidateFromMap(hourly)
	if err != nil {
		t.Fatalf("expected the 1h anomaly kind to validate, got: %v", err)
	}
	_ = json.Unmarshal(out, &parsed)
	spec = parsed["evaluation"].(map[string]any)["spec"].(map[string]any)
	if spec["evalWindow"] != "3h0m0s" || spec["frequency"] != "1h0m0s" {
		t.Errorf("at 1h the default is 3h every 1h; got %v", spec)
	}
}

func TestValidate_AnomalyKind_LogsAndTraces(t *testing.T) {
	rule := anomalyKindRule()
	rule["alertType"] = "LOGS_BASED_ALERT"
	q := rule["condition"].(map[string]any)["compositeQuery"].(map[string]any)["queries"].([]any)[0].(map[string]any)
	q["spec"] = map[string]any{"name": "A", "signal": "logs", "aggregations": []any{map[string]any{"expression": "count()"}}}
	if _, err := ValidateFromMap(rule); err != nil {
		t.Fatalf("the anomaly kind works on logs; got: %v", err)
	}
}

func TestValidate_AnomalyKind_Rejects(t *testing.T) {
	cases := map[string]struct {
		mutate func(map[string]any)
		want   string
	}{
		"z too high": {func(r map[string]any) {
			spec(r)[0].(map[string]any)["z"] = float64(11)
		}, "z"},
		"no z": {func(r map[string]any) {
			delete(spec(r)[0].(map[string]any), "z")
		}, "z"},
		"recovery not under z": {func(r map[string]any) {
			spec(r)[1].(map[string]any)["recoveryZ"] = float64(3)
		}, "recoveryZ"},
		"bad direction": {func(r map[string]any) {
			spec(r)[0].(map[string]any)["direction"] = "above"
		}, "direction"},
		"N over M": {func(r map[string]any) {
			spec(r)[1].(map[string]any)["minAnomalous"] = float64(4)
		}, "minAnomalous"},
		"repeated name": {func(r map[string]any) {
			spec(r)[1].(map[string]any)["name"] = "critical"
		}, "repeated"},
		"v1 fields in the spec": {func(r map[string]any) {
			spec(r)[0].(map[string]any)["target"] = float64(3)
		}, "target"},
		"window not a multiple": {func(r map[string]any) {
			r["evaluation"].(map[string]any)["spec"].(map[string]any)["evalWindow"] = "12m"
		}, "evalWindow"},
		"frequency over the resolution": {func(r map[string]any) {
			r["evaluation"].(map[string]any)["spec"].(map[string]any)["frequency"] = "10m"
		}, "frequency"},
		"cumulative": {func(r map[string]any) {
			r["evaluation"].(map[string]any)["kind"] = "cumulative"
		}, "rolling"},
		"hourly at 1h": {func(r map[string]any) {
			cond(r)["anomaly"] = map[string]any{"resolution": "1h"}
			cond(r)["seasonality"] = "hourly"
			r["evaluation"].(map[string]any)["spec"] = map[string]any{"evalWindow": "3h", "frequency": "1h"}
		}, "seasonality"},
		"bad resolution": {func(r map[string]any) {
			cond(r)["anomaly"] = map[string]any{"resolution": "15m"}
		}, "resolution"},
		"level shift too long": {func(r map[string]any) {
			cond(r)["anomaly"] = map[string]any{"acceptLevelShiftAfter": "200h"}
		}, "acceptLevelShiftAfter"},
		"promql": {func(r map[string]any) {
			cond(r)["compositeQuery"].(map[string]any)["queryType"] = "promql"
		}, "builder"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rule := anomalyKindRule()
			c.mutate(rule)
			_, err := ValidateFromMap(rule)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("expected an error about %q, got: %v", c.want, err)
			}
		})
	}
}

func TestValidate_AnomalyRuleV1_ResolutionLimits(t *testing.T) {
	rule := minimalValidAnomalyRule()
	rule["frequency"] = "3h"
	if _, err := ValidateFromMap(rule); err == nil || !strings.Contains(err.Error(), "frequency") {
		t.Errorf("a v1 anomaly rule is evaluated at most every 5m; got: %v", err)
	}
	rule = minimalValidAnomalyRule()
	rule["evalWindow"] = "12m"
	if _, err := ValidateFromMap(rule); err == nil || !strings.Contains(err.Error(), "evalWindow") {
		t.Errorf("the v1 window must be a multiple of 5m; got: %v", err)
	}
}

func cond(r map[string]any) map[string]any { return r["condition"].(map[string]any) }

func spec(r map[string]any) []any {
	return cond(r)["thresholds"].(map[string]any)["spec"].([]any)
}
