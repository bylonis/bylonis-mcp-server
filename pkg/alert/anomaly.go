package alert

import (
	"fmt"
	"time"
)

// Reglas de anomalía con el kind anomaly (bylonis#901; ADR-001 de Anomaly Detection en el repo bylonis,
// docs/anomaly-detection/adr-001-anomaly-rule.md). Es el formato v2alpha1 de anomaly_rule: evaluation rolling,
// condition.thresholds.kind = anomaly con z, recoveryZ, direction y minAnomalous por severidad, y
// condition.anomaly con las opciones del modelo. El formato v1 (condition.op/matchType/target y evalWindow
// arriba) se sigue aceptando.

const (
	anomalyMinZ          = 0.5
	anomalyMaxZ          = 10.0
	anomalyMaxWindow     = 288
	anomalyMinLevelShift = time.Hour
	anomalyMaxLevelShift = 7 * 24 * time.Hour
)

var (
	validAnomalyDirections  = map[string]bool{"up": true, "down": true, "both": true}
	validAnomalySeasonality = map[string]bool{"none": true, "hourly": true, "daily": true, "weekly": true}
	anomalyResolutions      = map[string]time.Duration{"5m": 5 * time.Minute, "1h": time.Hour}
)

// isAnomalyKind dice si la regla es una anomaly_rule con el kind anomaly (v2alpha1).
func isAnomalyKind(rule map[string]any) bool {
	if strVal(rule, "ruleType") != "anomaly_rule" {
		return false
	}
	cond := mapVal(rule, "condition")
	if cond == nil {
		return false
	}
	return strVal(mapVal(cond, "thresholds"), "kind") == "anomaly"
}

// validateAnomalyKind comprueba una anomaly_rule con el kind anomaly como lo hace el backend (ADR-001 D3, D4, D10).
func validateAnomalyKind(rule, cond map[string]any, errs *ValidationError) {
	if qt := strVal(mapVal(cond, "compositeQuery"), "queryType"); qt != "" && qt != "builder" {
		errs.Addf("condition.compositeQuery.queryType", "must be builder for an anomaly rule; got %q", qt)
	}
	if alg := strVal(cond, "algorithm"); alg != "" && alg != "standard" {
		errs.Addf("condition.algorithm", "must be standard or omitted; got %q", alg)
	}

	resolution := 5 * time.Minute
	if opts := mapVal(cond, "anomaly"); opts != nil {
		if r := strVal(opts, "resolution"); r != "" {
			if d, ok := anomalyResolutions[r]; ok {
				resolution = d
			} else {
				errs.Addf("condition.anomaly.resolution", "must be 5m or 1h; got %q", r)
			}
		}
		if v, ok := floatVal(opts, "minExpected"); ok && v < 0 {
			errs.Add("condition.anomaly.minExpected", "must be 0 or more (minimum traffic, in the unit of the query)")
		}
		if v, ok := floatVal(opts, "baselineFrom"); ok && v < 0 {
			errs.Add("condition.anomaly.baselineFrom", "must be a unix time in milliseconds")
		}
		if text := strVal(opts, "acceptLevelShiftAfter"); text != "" {
			d, err := time.ParseDuration(text)
			if err != nil || d < anomalyMinLevelShift || d > anomalyMaxLevelShift {
				errs.Addf("condition.anomaly.acceptLevelShiftAfter", "must be a Go duration between 1h and 168h; got %q", text)
			}
		}
	}

	seasonality := strVal(cond, "seasonality")
	if seasonality != "" && !validAnomalySeasonality[seasonality] {
		errs.Addf("condition.seasonality", "must be none, hourly, daily or weekly; got %q", seasonality)
	}
	if resolution >= time.Hour && (seasonality == "" || seasonality == "hourly") {
		errs.Add("condition.seasonality", "hourly (the default) needs the 5m resolution; use none, daily or weekly at 1h")
	}

	// Ventana y frecuencia: rolling, la ventana múltiplo de la resolución y la frecuencia entre 1m y la resolución.
	windowPoints := 0
	if eval := mapVal(rule, "evaluation"); eval != nil {
		spec := mapVal(eval, "spec")
		if w := strVal(spec, "evalWindow"); w != "" {
			if d, err := time.ParseDuration(w); err == nil {
				if d%resolution != 0 || d/resolution < 1 || d/resolution > anomalyMaxWindow {
					errs.Addf("evaluation.spec.evalWindow", "must be a multiple of the resolution (%s), at most %d points; got %q", resolution, anomalyMaxWindow, w)
				} else {
					windowPoints = int(d / resolution)
				}
			}
		}
		if f := strVal(spec, "frequency"); f != "" {
			if d, err := time.ParseDuration(f); err == nil && (d < time.Minute || d > resolution) {
				errs.Addf("evaluation.spec.frequency", "must be between 1m and the resolution (%s); got %q", resolution, f)
			}
		}
	}

	specs := sliceVal(mapVal(cond, "thresholds"), "spec")
	if len(specs) == 0 {
		errs.Add("condition.thresholds.spec", "must contain at least one threshold")
	}
	seen := map[string]bool{}
	for i, s := range specs {
		prefix := fmt.Sprintf("condition.thresholds.spec[%d]", i)
		sm, ok := s.(map[string]any)
		if !ok {
			errs.Add(prefix, "must be an object with name and z")
			continue
		}
		name := strVal(sm, "name")
		if name == "" {
			errs.Add(prefix+".name", "is required (critical, error, warning, or info)")
		} else if seen[name] {
			errs.Addf(prefix+".name", "%q is repeated", name)
		}
		seen[name] = true
		z, hasZ := floatVal(sm, "z")
		if !hasZ || z < anomalyMinZ || z > anomalyMaxZ {
			errs.Addf(prefix+".z", "is required: the deviations, between %g and %g (2 high sensitivity, 3 medium, 4 low)", anomalyMinZ, anomalyMaxZ)
		}
		if rz, ok := floatVal(sm, "recoveryZ"); ok && (rz <= 0 || (hasZ && rz >= z)) {
			errs.Add(prefix+".recoveryZ", "must be greater than 0 and lower than z")
		}
		if dir := strVal(sm, "direction"); dir != "" && !validAnomalyDirections[dir] {
			errs.Addf(prefix+".direction", "must be up, down or both; got %q", dir)
		}
		if n, ok := floatVal(sm, "minAnomalous"); ok {
			if n < 1 || n != float64(int(n)) {
				errs.Add(prefix+".minAnomalous", "must be a whole number of points, 1 or more")
			} else if windowPoints > 0 && int(n) > windowPoints {
				errs.Addf(prefix+".minAnomalous", "must be at most %d, the points of the evaluation window", windowPoints)
			}
		}
		for _, v1 := range []string{"target", "op", "matchType"} {
			if _, ok := sm[v1]; ok {
				errs.Addf(prefix+"."+v1, "is not used with kind anomaly; use z, direction and minAnomalous")
			}
		}
	}
}

// applyAnomalyDefaults: la ventana por defecto de una regla de anomalía son 15 min evaluados cada 5 min (M = 3),
// en lugar de los 5m/1m del umbral, que darían un solo punto.
func applyAnomalyDefaults(rule map[string]any) {
	if rule["evaluation"] != nil {
		return
	}
	frequency := "5m0s"
	window := "15m0s"
	if opts := mapVal(mapVal(rule, "condition"), "anomaly"); strVal(opts, "resolution") == "1h" {
		frequency, window = "1h0m0s", "3h0m0s"
	}
	rule["evaluation"] = map[string]any{
		"kind": "rolling",
		"spec": map[string]any{"evalWindow": window, "frequency": frequency},
	}
}
