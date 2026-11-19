package alert

// Instructions is the MCP resource content for bylonis://alert/instructions.
const Instructions = `# ByLonis Alert Rule — Instructions

## Overview
An alert rule monitors a signal (metrics, logs, traces, or exceptions) and fires when a condition is met.
The alert is created via POST /api/v2/rules.

Schemas supported:
- **v2alpha1** for threshold_rule and promql_rule — structured thresholds + evaluation + notificationSettings. Applied automatically.
- **v2alpha1** for anomaly_rule too, with thresholds.kind = anomaly (see the Anomaly Alerts section). The older v1 anomaly shape (top-level evalWindow/frequency with condition.op/matchType/target, no thresholds) is still accepted.

## CRITICAL: Before Creating an Alert
1. ALWAYS read bylonis://alert/examples for complete working payloads (metric, logs and traces thresholds, PromQL, anomaly, tiered thresholds, notification settings and a Cost Meter cumulative-budget example).
2. Use bylonis_get_alert on an existing alert to study the exact structure your ByLonis instance expects.
3. Use bylonis_get_field_keys to discover available attributes for filters and groupBy.
4. NOTIFICATION CHANNELS: If the user explicitly names a channel, use it directly. Otherwise, do NOT guess channel names — call bylonis_create_alert without channels first, it returns available channels. Present the list to the user, let them choose, then retry with their selection. If no suitable channel exists, use bylonis_create_notification_channel to create one first.

## Quick Workflow: From User Intent to Payload
A repeatable mental model for going from a user request ("alert me when login p99 > 2s") to a valid payload:
1. **Signal → alertType.** CPU, memory, latency histograms, request rate → METRIC_BASED_ALERT. Log lines or log volume → LOGS_BASED_ALERT. Span latency or span error rate → TRACES_BASED_ALERT. Exception counts → EXCEPTIONS_BASED_ALERT.
2. **Pick ruleType.** Default to threshold_rule. Use promql_rule only if the user provided a PromQL expression. Use anomaly_rule when the user wants to know when a series behaves unusually for that time of day or week, without a fixed number (traffic spike at 3 a.m., drop at peak); see the Anomaly Alerts section.
3. **Pick compositeQuery.queryType + matching envelope type.** See the "Query envelope type" table.
4. **Pick the aggregation shape.** Metrics → object {metricName, timeAggregation, spaceAggregation}. Logs/traces → {expression: "count()" | "p99(duration_nano)" | …}.
5. **Write the filter.** See "Filter & Having Expressions" for the operator set. Prefer resource attributes (service.name, deployment.environment, k8s.*) — the backend indexes them.
6. **Configure thresholds.** Tier name (critical | error | warning | info), op, matchType, target. Add channels only if the user named them — otherwise leave empty and let channel validation prompt the user.
7. **Evaluation.** Leave defaults (evalWindow=5m, frequency=1m) unless the user asked for a different window. If the user says the problem must last (e.g. "only if it stays down 10 minutes"), set holdDuration.
8. **Notification.** Always ask the user for channel names — never guess. Set notificationSettings.groupBy on high-cardinality queries to reduce noise.
9. **Replay it.** Before creating it, run bylonis_backtest_alert with the same payload and tell the user how often it would have fired in the last days, and how many of those firings were short (flapping). If it would fire too often, adjust the threshold or holdDuration first.

## Alert Types (alertType)
| Value | Signal | Use When |
|-------|--------|----------|
| METRIC_BASED_ALERT | metrics | Monitoring numeric metrics (CPU, memory, request rate, latency) |
| LOGS_BASED_ALERT | logs | Monitoring log patterns, error counts, log volume |
| TRACES_BASED_ALERT | traces | Monitoring span latency, error rates, throughput |
| EXCEPTIONS_BASED_ALERT | exceptions | Monitoring exception counts (typically uses clickhouse_sql) |

## Rule Types (ruleType)
| Value | Schema | Description | Constraints |
|-------|--------|-------------|-------------|
| threshold_rule | v2alpha1 | Compare series against a static threshold | Works with all alert types |
| promql_rule | v2alpha1 | Evaluate a PromQL expression | queryType must be "promql" |
| anomaly_rule | v2alpha1 | Fire when the value leaves its expected range | Builder queries (metrics, logs, traces); thresholds.kind anomaly; rolling evaluation, window a multiple of the resolution |

## Composite Query Structure

condition.compositeQuery uses the v5 query format:
- queryType: "builder" | "promql" | "clickhouse_sql"
- panelType: always "graph" for alerts (auto-set)
- unit: series unit (used for value formatting and target-unit conversion)
- queries: array of query objects

### Query envelope type (queries[].type)
The envelope type must match compositeQuery.queryType:
| queryType | Accepted envelope types |
|-----------|-------------------------|
| builder | builder_query, builder_formula, builder_trace_operator |
| promql | promql (or builder_formula when composing with another promql query) |
| clickhouse_sql | clickhouse_sql (or builder_formula) |

### Builder query spec (builder_query)
- name: query identifier (A, B, C, …)
- signal: "metrics" | "logs" | "traces" (must match alertType)
- source (metrics only): "meter" to alert on Cost Meter usage/billing metrics (e.g. bylonis.meter.log.size); omit otherwise. Works with either evaluation kind — cumulative for daily/monthly spend budgets, rolling for rate/over-time meter alerts.
- stepInterval: interval in seconds (60 for most alerts)
- aggregations: see "Aggregation shapes" below
- filter: {expression: "service.name = 'frontend' AND http.status_code >= 500"}
- groupBy: [{name, fieldContext: "resource" | "attribute", fieldDataType}]
- functions: post-query transforms. Not needed for anomaly_rule: the rule applies its own model.
- disabled: true when the query is used only as an input to a formula

### PromQL / ClickHouse query spec (envelope type=promql or clickhouse_sql)
- name: query identifier
- query: the PromQL or SQL string (required)
- legend: legend template
- disabled: false

### PromQL for OTel dotted metric names
Alerts with ruleType=promql_rule on OpenTelemetry metrics must use the Prometheus 3.x UTF-8 quoted-selector form for any name containing a dot. The forms that look natural but return no data in ByLonis:

- Underscored conversion: rate(payment_latency_ms_bucket[5m]) — ByLonis does NOT rename dots to underscores.
- __name__ selector: rate({__name__="payment_latency_ms.bucket"}[5m]) — dots are rejected inside that value.
- Bare dotted name: rate(payment_latency_ms.bucket[5m]) — dot is not a legal identifier char.

Correct form — quote the name inside braces:

  histogram_quantile(0.9, sum(rate({"payment_latency_ms.bucket"}[5m])) by (le))

The same rule applies to dotted label names in by(...) and in matchers:

  sum by (le, "service.name") (
    rate({"payment_latency_ms.bucket", "deployment.environment"="prod"}[5m])
  )

For the full guide (syntax, examples by metric type, anti-pattern table, pre-flight checklist for histogram alerts) read bylonis://promql/instructions. Always read it before writing a promql_rule query.

### Formula query spec (envelope type=builder_formula)
- name: formula identifier (F1, F2, …)
- expression: math expression referencing other query names (e.g. "(A / B) * 100"). Supports +, -, *, /, and functions like abs(), sqrt(), log(), exp()
- legend: legend template
- Set selectedQueryName to the formula name (e.g. "F1") so the alert triggers on the formula result

## Aggregation shapes

### Metrics signal — object shape
Use this shape when spec.signal = "metrics":

` + "```" + `json
"aggregations": [
  {"metricName": "k8s.pod.cpu_request_utilization", "timeAggregation": "avg", "spaceAggregation": "max"}
]
` + "```" + `

- metricName: the metric you're querying.
- timeAggregation: per-series time aggregation. Common: avg, max, min, sum, rate, increase, count, count_distinct, latest. Default by metric type: gauge→avg, cumulative counter→rate, delta counter→sum.
- spaceAggregation: cross-series aggregation. Common: sum, avg, min, max, count. For histograms: p50, p75, p90, p95, p99.

### Logs / traces signal — expression shape
Use this shape when spec.signal = "logs" or "traces":

` + "```" + `json
"aggregations": [{"expression": "count()"}]
` + "```" + `

Common expressions: count(), count_distinct(user_id), avg(duration), sum(bytes), min(x), max(x), p50/p75/p90/p95/p99(duration_nano).

## Filter & Having Expressions

builder_query spec.filter.expression (pre-aggregation) and spec.having.expression (post-aggregation) use the same syntax. Prefer resource attributes in filters — they are the fastest path through the storage backend.

### Operator reference
| Intent | Operator | Example |
|--------|----------|---------|
| Field exists | EXISTS | trace_id EXISTS |
| Field is absent | NOT EXISTS | k8s.pod.name NOT EXISTS |
| Exact match | = | service.name = 'frontend' |
| Not equal (field must exist) | EXISTS AND != | service.name EXISTS AND service.name != 'redis' |
| One of several values | IN | severity_text IN ('ERROR', 'WARN', 'FATAL') |
| Excluded from a set | NOT IN | deployment.environment NOT IN ('dev', 'staging') |
| Substring / pattern | LIKE | name LIKE '%payment%' |
| Case-insensitive pattern | ILIKE | body ILIKE '%timeout%' |
| Simple containment | CONTAINS | body CONTAINS 'timeout' |
| Regex | REGEXP | name REGEXP '^grpc\.' |
| Numeric comparison | >, >=, <, <= | http.status_code >= 500 |

### Data type guardrails
- **bool**: =, !=, EXISTS, NOT EXISTS
- **int / float**: =, !=, >, >=, <, <=, IN, EXISTS, NOT EXISTS
- **string**: =, !=, LIKE, ILIKE, CONTAINS, REGEXP, IN, NOT IN, EXISTS, NOT EXISTS

### Composition
Combine with AND / OR and parentheses for grouping:
- service.name = 'frontend' AND http.status_code >= 500
- (severity_text = 'ERROR' AND service.name = 'payments-api') OR service.name = 'billing-api'

Negative operators (!=, NOT IN, NOT LIKE) only match rows where the field is present. To also exclude rows that lack the field entirely, pair with EXISTS:
- k8s.namespace.name EXISTS AND k8s.namespace.name != 'kube-system'

## Units

Set compositeQuery.unit to specify the unit of the queried data (Y-axis). This drives value formatting in alert messages ({{$value}}) and unit conversion when threshold targetUnit differs.

Common units: percent, percentunit, ms, s, ns, bytes, kbytes, mbytes, gbytes, reqps, ops, cps.

## Threshold Configuration (v2alpha1 — threshold_rule & promql_rule)

condition.thresholds defines one or more routing tiers. Each tier can route to different channels:

` + "```" + `json
"thresholds": {
  "kind": "basic",
  "spec": [
    {
      "name": "critical",
      "target": 1000,
      "op": "above",
      "matchType": "all_the_times",
      "channels": ["pagerduty-oncall", "slack-alerts"]
    },
    {
      "name": "warning",
      "target": 100,
      "op": "above",
      "matchType": "at_least_once",
      "channels": ["slack-alerts"]
    }
  ]
}
` + "```" + `

### Threshold fields
- **name**: tier name — critical, error, warning, or info. Acts as the routing label: alerts carry threshold_name equal to this value. Set labels.severity to match your highest tier.
- **target**: numeric threshold value (required).
- **targetUnit**: unit of the target (e.g. ms, percent, s, bytes). Auto-converted to compositeQuery.unit during evaluation.
- **recoveryTarget**: hysteresis value to avoid flapping (e.g. target=80%, recoveryTarget=75%). null uses the target itself as the recovery point.
- **matchType**: canonical at_least_once, all_the_times, on_average, in_total, last. Aliases accepted: avg (=on_average), sum (=in_total).
- **op**: canonical above, below, equal, not_equal, above_or_equal, below_or_equal, outside_bounds. Short forms accepted: eq, not_eq, above_or_eq, below_or_eq. Symbolic accepted: >, <, =, !=, >=, <=.
- **channels**: notification channel names for this tier. Discover via bylonis_list_notification_channels. Ignored when notificationSettings.usePolicy is true.

### Choosing targetUnit
- Set targetUnit when the threshold value is in a different unit from the query series. Example: the series emits nanoseconds (compositeQuery.unit="ns") but you want to threshold at "5 seconds" — set target=5, targetUnit="s". ByLonis converts during evaluation.
- If compositeQuery.unit is empty and targetUnit is set, the validator propagates targetUnit onto compositeQuery.unit — you can therefore omit compositeQuery.unit on single-threshold rules.
- Formulas that compute ratios (e.g. (A/B) * 100) already emit percent — set compositeQuery.unit="percent" and give the threshold a bare numeric target (no targetUnit).

### Choosing recoveryTarget
- **null / unset**: the alert recovers as soon as the series crosses back through target. Simplest but can flap when the value oscillates at the threshold boundary.
- **non-null**: creates a dead-band between target and recoveryTarget. Example: fire at target=80, recover at recoveryTarget=70 — the series must drop below 70 before the alert clears. Recommended for signals that oscillate near the threshold.

## Evaluation (v2alpha1)

evaluation controls how the rule is evaluated:

` + "```" + `json
"evaluation": {
  "kind": "rolling",
  "spec": {"evalWindow": "5m", "frequency": "1m"}
}
` + "```" + `

- evalWindow: the window of data each evaluation looks at (e.g. 5m, 15m, 30m, 1h, 4h, 24h). For how long the condition must last before firing, use holdDuration (below).
- frequency: how often to evaluate (e.g. 1m, 5m, 15m).
- Auto-generated (5m window, 1m frequency) if omitted for threshold/promql rules.
- **Format**: Go duration strings. Both "5m" and "5m0s" are accepted; stick to one style per payload. Common values: 1m, 5m, 15m, 30m, 1h, 4h, 24h. Units go up to h: write 24h, not 1d.
- **Sizing tips**: keep evalWindow ≥ frequency. Short windows (1-5m) catch spikes; long windows (30m-1h) smooth noise. For infrequent signals (hourly batch jobs) set frequency to 5-15m to reduce evaluation cost.

### Wait before firing (holdDuration)

` + "```" + `json
"holdDuration": "10m"
` + "```" + `

- Top-level field, for every ruleType (threshold, promql, anomaly and ClickHouse SQL rules).
- A series whose condition is true stays **pending** and sends nothing until it has held for holdDuration; if it recovers before, it never fires. Like the for of Prometheus.
- Between 0s and 24h (Go duration string). Omitted or 0s fires at once.
- It is the lever against a rule that flaps (fires and resolves within minutes again and again). In a ClickHouse SQL rule, where the query decides the condition, it is the main one.
- PUT replaces the whole rule: on bylonis_update_alert, keep the holdDuration of the saved rule unless the user asks to change it.
- To choose the value, bylonis_list_flapping_rules gives, per rule that flaps, how many alerts each wait would have avoided and the one ByLonis proposes; bylonis_backtest_alert replays the rule with the new value before saving it.

### Cumulative window (daily/monthly totals)

A general evaluation kind, independent of signal/source — use it for any period-total alert (daily error budgets, monthly request counts, Cost Meter spend budgets, …). It accumulates from a fixed reset point instead of using a sliding window:

` + "```" + `json
"evaluation": {
  "kind": "cumulative",
  "spec": {"schedule": {"type": "daily", "minute": 0, "hour": 0}, "frequency": "1m", "timezone": "UTC"}
}
` + "```" + `

- kind "cumulative" sums since the schedule boundary (vs "rolling" sliding window); pair with threshold matchType "in_total".
- schedule.type: "daily" (shown; minute/hour set the reset, e.g. 00:00) or "monthly". timezone e.g. "UTC".

## Notification Settings (v2alpha1)

` + "```" + `json
"notificationSettings": {
  "groupBy": ["service.name", "deployment.environment"],
  "newGroupEvalDelay": "2m",
  "renotify": {
    "enabled": true,
    "interval": "30m",
    "alertStates": ["firing", "nodata"]
  },
  "usePolicy": false
}
` + "```" + `

- **groupBy**: fields to group notifications by (reduces noise).
- **newGroupEvalDelay**: Go duration string. Grace period during which a newly-appearing label group is excluded from evaluation. Helps avoid flapping when new pods/services come online.
- **renotify.enabled**: whether to re-send alerts at interval.
- **renotify.interval**: re-notify interval (e.g. 15m, 30m, 1h, 4h).
- **renotify.alertStates**: accepted values are firing and nodata. Any other value is rejected.
- **usePolicy**: routing mode. false (default) = deliver to the channels listed in each threshold entry. true = ignore per-threshold channels and route via the org-level notification policy matching on labels.

## Fleet rules (label fleet)

Rules with the label fleet are written by the inventory of Fleet Control (Inventario → Alertas). ByLonis rejects creating, updating or deleting one from here with 400 fleet_managed_rule, and also adding or removing that label. To change one, tell the user to do it in Inventario → Alertas.

## Labels & Routing

- labels.severity: MUST be one of critical, error, warning, info. When thresholds is used, threshold.name is the routing tier — set labels.severity to the highest tier the rule carries.
- Additional labels like team, service, environment enable routing policies.
- preferredChannels: fallback notification channel names (thresholds.channels takes priority).
- Set usePolicy: true in notificationSettings to delegate routing to org-level policies.

### Label sources available to routing policies
Routing policies evaluate expressions against three merged label sources:
1. **User static labels** from the labels object on the rule (severity, team, service, environment, …).
2. **Platform labels** auto-injected at fire time:
   - alertname — the rule's alert field
   - threshold.name — the tier that fired (critical | error | warning | info)
   - ruleSource, ruleId — rule metadata
3. **Dynamic labels** from groupBy fields in the query (service.name, k8s.pod.name, http.route, deployment.environment, topic, partition, …).

### Routing-policy matcher operators
Policy expressions use a reduced operator set (not identical to query filters):
- Comparison: =, !=
- Text matching: CONTAINS, REGEXP
- Set membership: IN, NOT IN
- Logical: AND, OR, parentheses

Example: deployment.environment = "production" AND threshold.name = "critical"

### Channel-routing modes
| notificationSettings.usePolicy | thresholds[].channels | Effective routing |
|--------------------------------|-----------------------|-------------------|
| false (default) | present | Send to the listed channels directly |
| false | absent | Fall back to rule-level preferredChannels |
| true | (ignored) | Match alert labels against the org-level routing policy; send to policy-matched channels |

## Annotations
- Use {{$value}} for the current metric value.
- Use {{$threshold}} for the threshold value.
- Use {{$labels.key}} for label values (e.g. {{$labels.k8s_pod_name}}).
- Common annotations: description, summary, runbook.

## Anomaly Alerts (ruleType: anomaly_rule)
For each series of the selected query, ByLonis predicts every point from its history (Holt-Winters, with the seasonality of the rule) and scores it with a signed z = (value − expected) / σ, where σ is a robust deviation of the past errors. A tier fires when N of the M points of the evaluation window pass its z in its direction. It needs no fixed threshold.

Fields (v2alpha1):
- ruleType: anomaly_rule; alertType: METRIC_BASED_ALERT, LOGS_BASED_ALERT or TRACES_BASED_ALERT. Builder queries or formulas only (no PromQL or SQL).
- condition.thresholds: {kind: "anomaly", spec: [{name, z, direction, minAnomalous, recoveryZ, channels}]}
  - z (required): deviations, 0.5–10.
  - direction: up (above the expected value), down (below) or both (default).
  - minAnomalous: N of «N of M»; M = evalWindow / resolution. 1 = at least once (default), M = always. 2 of 3 skips single-point noise.
  - recoveryZ: an active alert recovers only when no point passes it (hysteresis). Default max(z − 1, 0.5).
- condition.seasonality: none, hourly (5m resolution only), daily (business hours vs nights) or weekly (weekdays vs weekends).
- condition.algorithm: "standard" or omitted.
- condition.anomaly (optional): resolution "5m" (default) or "1h"; minExpected (minimum traffic in the unit of the query, e.g. 0.5 for calls/s: lower points are not evaluated); acceptLevelShiftAfter (Go duration 1h–168h, default 24h: a sustained level change becomes normal); baselineFrom (unix ms, to reset the baseline after a deploy).
- evaluation: rolling; evalWindow a multiple of the resolution (at most 288 points), frequency between 1m and the resolution. Default 15m every 5m.

### Anomaly tuning
| z | Sensitivity | Use case |
|---|-------------|----------|
| 4 | Low | Only strong anomalies — few false positives |
| 3 | Medium (default) | Most series |
| 2 | High | Noisy; for well-behaved series |

- Warning and critical are two tiers with different z (e.g. warning z=3 with 2 of 3 points, critical z=4 with 1).
- History: the rule needs 2 cycles (2 hours hourly, 2 days daily, 14 days weekly). Until then it compares without seasonality; its alerts carry the label anomaly_learning="true" and {{$learning}} says how far it is. Retention is 30 days, so weekly gets about 4 cycles.
- Low-volume series: set condition.anomaly.minExpected; one failed request on a tiny series is a huge z.
- Templates: {{$value}} real value, {{$expected}}, {{$lower}}, {{$upper}} (the band at the tier's z), {{$z}}, {{$seasonality}}, {{$learning}}.

The older v1 shape is still accepted: top-level evalWindow (a multiple of 5m) and frequency (1m–5m), condition.op/matchType/target (target = z, op below = down, outside_bounds = both; matchType at_least_once or all_the_times), algorithm and seasonality, no thresholds.

See bylonis://alert/examples → "metric_anomaly" (kind anomaly) and "metric_anomaly_v1" for complete payloads.

## Absent-data alerting
Set condition.alertOnAbsent=true to fire when no series is returned. condition.absentFor is in **minutes** (equivalent to consecutive evaluation cycles when frequency is 1m). Example: absentFor=15 fires after 15 minutes of no data.

## Auto-Applied Defaults (threshold_rule / promql_rule; anomaly_rule with kind anomaly gets evaluation 15m every 5m, or 3h every 1h at the 1h resolution)
- version → "v5"
- schemaVersion → "v2alpha1"
- evaluation → {kind: "rolling", spec: {evalWindow: "5m0s", frequency: "1m0s"}}
- notificationSettings → {renotify: {enabled: false, interval: "30m"}}
- panelType → "graph"
- selectedQueryName → first query name
- source → "mcp"
- labels.severity → "warning" (if not set)
- annotations → default description and summary templates

anomaly_rule: none of the above defaults are applied automatically — you must supply evalWindow, frequency, and the condition fields yourself.

`

// Examples is the MCP resource content for bylonis://alert/examples.
// The examples below mirror the canonical payloads in upstream PR #11023, plus a Cost Meter example
// (pkg/apiserver/bylonisapiserver/ruler_examples.go). Keep this list in sync
// with upstream when that file changes.
const Examples = `# ByLonis Alert Rule — Examples

These examples mirror the canonical payloads of ByLonis
(pkg/apiserver/bylonisapiserver/ruler_examples.go). Threshold and PromQL rules
use v2alpha1; the anomaly example uses the v1 shape. Any of them takes a
top-level holdDuration (e.g. "10m") to fire only when the condition lasts.

## 1. metric_threshold_single — metric threshold, single builder query
Fires when a pod consumes more than 80% of its requested CPU for the whole evaluation window.

` + "```" + `json
{
  "alert": "Pod CPU above 80% of request",
  "alertType": "METRIC_BASED_ALERT",
  "description": "CPU usage for api-service pods exceeds 80% of the requested CPU",
  "ruleType": "threshold_rule",
  "version": "v5",
  "schemaVersion": "v2alpha1",
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "panelType": "graph",
      "unit": "percentunit",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A",
            "signal": "metrics",
            "stepInterval": 60,
            "aggregations": [{"metricName": "k8s.pod.cpu_request_utilization", "timeAggregation": "avg", "spaceAggregation": "max"}],
            "filter": {"expression": "k8s.deployment.name = 'api-service'"},
            "groupBy": [
              {"name": "k8s.pod.name", "fieldContext": "resource", "fieldDataType": "string"},
              {"name": "deployment.environment", "fieldContext": "resource", "fieldDataType": "string"}
            ],
            "legend": "{{k8s.pod.name}} ({{deployment.environment}})"
          }
        }
      ]
    },
    "selectedQueryName": "A",
    "thresholds": {
      "kind": "basic",
      "spec": [
        {"name": "critical", "op": "above", "matchType": "all_the_times", "target": 0.8, "channels": ["slack-platform", "pagerduty-oncall"]}
      ]
    }
  },
  "evaluation": {"kind": "rolling", "spec": {"evalWindow": "15m", "frequency": "1m"}},
  "notificationSettings": {
    "groupBy": ["k8s.pod.name", "deployment.environment"],
    "renotify": {"enabled": true, "interval": "4h", "alertStates": ["firing"]}
  },
  "labels": {"severity": "critical", "team": "platform"},
  "annotations": {
    "description": "Pod {{$k8s.pod.name}} CPU is at {{$value}} of request in {{$deployment.environment}}.",
    "summary": "Pod CPU above {{$threshold}} of request"
  }
}
` + "```" + `

## 2. metric_threshold_formula — metric threshold with a builder_formula
Computes disk utilization as (1 - available/capacity) * 100 by combining two disabled base queries with a builder_formula. The formula emits 0-100, so compositeQuery.unit is set to "percent" and the target is a bare number.

` + "```" + `json
{
  "alert": "PersistentVolume above 80% utilization",
  "alertType": "METRIC_BASED_ALERT",
  "description": "Disk utilization for a persistent volume is above 80%",
  "ruleType": "threshold_rule",
  "version": "v5",
  "schemaVersion": "v2alpha1",
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "panelType": "graph",
      "unit": "percent",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A", "signal": "metrics", "stepInterval": 60, "disabled": true,
            "aggregations": [{"metricName": "k8s.volume.available", "timeAggregation": "max", "spaceAggregation": "max"}],
            "filter": {"expression": "k8s.volume.type = 'persistentVolumeClaim'"},
            "groupBy": [
              {"name": "k8s.persistentvolumeclaim.name", "fieldContext": "resource", "fieldDataType": "string"},
              {"name": "k8s.namespace.name", "fieldContext": "resource", "fieldDataType": "string"}
            ]
          }
        },
        {
          "type": "builder_query",
          "spec": {
            "name": "B", "signal": "metrics", "stepInterval": 60, "disabled": true,
            "aggregations": [{"metricName": "k8s.volume.capacity", "timeAggregation": "max", "spaceAggregation": "max"}],
            "filter": {"expression": "k8s.volume.type = 'persistentVolumeClaim'"},
            "groupBy": [
              {"name": "k8s.persistentvolumeclaim.name", "fieldContext": "resource", "fieldDataType": "string"},
              {"name": "k8s.namespace.name", "fieldContext": "resource", "fieldDataType": "string"}
            ]
          }
        },
        {
          "type": "builder_formula",
          "spec": {
            "name": "F1",
            "expression": "(1 - A/B) * 100",
            "legend": "{{k8s.persistentvolumeclaim.name}} ({{k8s.namespace.name}})"
          }
        }
      ]
    },
    "selectedQueryName": "F1",
    "thresholds": {
      "kind": "basic",
      "spec": [
        {"name": "critical", "op": "above", "matchType": "at_least_once", "target": 80, "channels": ["slack-storage"]}
      ]
    }
  },
  "evaluation": {"kind": "rolling", "spec": {"evalWindow": "30m", "frequency": "5m"}},
  "notificationSettings": {
    "groupBy": ["k8s.namespace.name", "k8s.persistentvolumeclaim.name"],
    "renotify": {"enabled": true, "interval": "2h", "alertStates": ["firing"]}
  },
  "labels": {"severity": "critical"},
  "annotations": {
    "description": "Volume {{$k8s.persistentvolumeclaim.name}} in {{$k8s.namespace.name}} is {{$value}}% full.",
    "summary": "Disk utilization above {{$threshold}}%"
  }
}
` + "```" + `

## 3. metric_promql — PromQL rule
PromQL expression instead of the builder. Dotted OTEL resource attributes are quoted ("deployment.environment"); non-dotted ones (topic, partition, group) stay bare. Useful for queries that combine series with group_right or other Prometheus operators. The envelope type is "promql" — not "builder_query". Read bylonis://promql/instructions for the full dotted-name / vector-matching guide.

` + "```" + `json
{
  "alert": "Kafka consumer group lag above 1000",
  "alertType": "METRIC_BASED_ALERT",
  "description": "Consumer group lag computed via PromQL",
  "ruleType": "promql_rule",
  "version": "v5",
  "schemaVersion": "v2alpha1",
  "condition": {
    "compositeQuery": {
      "queryType": "promql",
      "panelType": "graph",
      "queries": [
        {
          "type": "promql",
          "spec": {
            "name": "A",
            "query": "(max by(topic, partition, \"deployment.environment\")(kafka_log_end_offset) - on(topic, partition, \"deployment.environment\") group_right max by(group, topic, partition, \"deployment.environment\")(kafka_consumer_committed_offset)) > 0",
            "legend": "{{topic}}/{{partition}} ({{group}})"
          }
        }
      ]
    },
    "selectedQueryName": "A",
    "thresholds": {
      "kind": "basic",
      "spec": [
        {"name": "critical", "op": "above", "matchType": "all_the_times", "target": 1000, "channels": ["slack-data-platform", "pagerduty-data"]}
      ]
    }
  },
  "evaluation": {"kind": "rolling", "spec": {"evalWindow": "10m", "frequency": "1m"}},
  "notificationSettings": {
    "groupBy": ["group", "topic"],
    "renotify": {"enabled": true, "interval": "1h", "alertStates": ["firing"]}
  },
  "labels": {"severity": "critical"},
  "annotations": {
    "description": "Consumer group {{$group}} is {{$value}} messages behind on {{$topic}}/{{$partition}}.",
    "summary": "Kafka consumer lag high"
  }
}
` + "```" + `

## 4. metric_anomaly — anomaly rule (kind anomaly)
Fires when the calls of a service leave their usual band for that time of day. ByLonis predicts each 5-minute point from the history of the series (daily seasonality: 7 days) and scores it with a signed z = (value − expected) / σ. Each severity in thresholds.spec has:
- **z**: the deviations that fire it, 0.5–10. Sensitivity presets: 2 high, 3 medium, 4 low.
- **direction**: up, down or both (default).
- **minAnomalous**: N of «N of M», how many of the M points of the window (evalWindow / resolution) must pass z. 1 is «at least once», M is «always». Default 1.
- **recoveryZ**: an active alert recovers when no point passes it (hysteresis). Default max(z − 1, 0.5).

condition.anomaly is optional: resolution 5m (default) or 1h; minExpected (minimum traffic in the unit of the query: points expected under it are not evaluated); acceptLevelShiftAfter (1h–168h, default 24h); baselineFrom (unix ms: «reset baseline» after a deploy). seasonality: none, hourly (5m only), daily or weekly. The window must be a multiple of the resolution and the frequency between 1m and the resolution. Works on metrics, logs and traces builder queries and formulas, not PromQL or SQL. The rule needs 2 cycles of history (2 days for daily); until then it compares without seasonality and its alerts carry the label anomaly_learning. Templates can use {{$expected}}, {{$lower}}, {{$upper}}, {{$z}}, {{$seasonality}} and {{$learning}}; {{$value}} is the real value.

` + "```" + `json
{
  "alert": "Anomalous calls to checkout",
  "alertType": "METRIC_BASED_ALERT",
  "description": "Calls per second of the checkout service out of their usual band",
  "ruleType": "anomaly_rule",
  "version": "v5",
  "schemaVersion": "v2alpha1",
  "evaluation": {"kind": "rolling", "spec": {"evalWindow": "15m", "frequency": "5m"}},
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "panelType": "graph",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A", "signal": "metrics",
            "aggregations": [{"metricName": "bylonis_calls_total", "timeAggregation": "rate", "spaceAggregation": "sum"}],
            "filter": {"expression": "service.name = 'checkout'"},
            "groupBy": [{"name": "service.name", "fieldContext": "resource", "fieldDataType": "string"}]
          }
        }
      ]
    },
    "selectedQueryName": "A",
    "algorithm": "standard",
    "seasonality": "daily",
    "anomaly": {"resolution": "5m", "minExpected": 0.5},
    "thresholds": {
      "kind": "anomaly",
      "spec": [
        {"name": "critical", "z": 4, "direction": "both", "minAnomalous": 1, "channels": ["on-call"]},
        {"name": "warning", "z": 3, "recoveryZ": 2, "direction": "both", "minAnomalous": 2, "channels": ["team"]}
      ]
    }
  },
  "notificationSettings": {"renotify": {"enabled": true, "interval": "4h", "alertStates": ["firing"]}},
  "labels": {"severity": "critical"},
  "annotations": {
    "description": "{{$service.name}}: {{$value}} calls/s, expected {{$expected}} ({{$lower}}–{{$upper}}), z {{$z}}.",
    "summary": "Anomalous calls to {{$service.name}}"
  }
}
` + "```" + `

## 4b. metric_anomaly_v1 — anomaly rule (older v1 shape, still accepted)
Prefer kind anomaly above. The v1 shape has top-level evalWindow/frequency and condition.op/matchType/target/algorithm/seasonality, and no thresholds block: target is z, op the direction (below fires on drops, outside_bounds on both), matchType at_least_once or all_the_times. It works at the 5-minute resolution: the window must be a multiple of 5m (at most 24h) and the frequency between 1m and 5m. The anomaly function in the query is optional; the rule replaces it with its own parameters.

` + "```" + `json
{
  "alert": "Anomalous drop in ingested spans",
  "alertType": "METRIC_BASED_ALERT",
  "description": "Detect an abrupt drop in span ingestion",
  "ruleType": "anomaly_rule",
  "version": "v5",
  "evalWindow": "15m",
  "frequency": "5m",
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "panelType": "graph",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A", "signal": "metrics",
            "aggregations": [{"metricName": "otelcol_receiver_accepted_spans", "timeAggregation": "rate", "spaceAggregation": "sum"}],
            "groupBy": [{"name": "tenant_id", "fieldContext": "attribute", "fieldDataType": "string"}],
            "functions": [
              {"name": "anomaly", "args": [{"name": "z_score_threshold", "value": 3}]}
            ],
            "legend": "{{tenant_id}}"
          }
        }
      ]
    },
    "op": "below",
    "matchType": "all_the_times",
    "target": 3,
    "algorithm": "standard",
    "seasonality": "daily",
    "selectedQueryName": "A"
  },
  "labels": {"severity": "warning"},
  "preferredChannels": ["slack-ingestion"],
  "annotations": {
    "description": "Ingestion for tenant {{$tenant_id}} is anomalously low: {{$value}} spans/s, expected {{$expected}} (z {{$z}}).",
    "summary": "Span ingestion anomaly"
  }
}
` + "```" + `

## 5. logs_threshold — logs threshold count()
Counts matching log records (ERROR severity + body contains) over a rolling window.

` + "```" + `json
{
  "alert": "Payments service panic logs",
  "alertType": "LOGS_BASED_ALERT",
  "description": "Any panic log line emitted by the payments service",
  "ruleType": "threshold_rule",
  "version": "v5",
  "schemaVersion": "v2alpha1",
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "panelType": "graph",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A", "signal": "logs", "stepInterval": 60,
            "aggregations": [{"expression": "count()"}],
            "filter": {"expression": "service.name = 'payments-api' AND severity_text = 'ERROR' AND body CONTAINS 'panic'"},
            "groupBy": [
              {"name": "k8s.pod.name", "fieldContext": "resource", "fieldDataType": "string"},
              {"name": "deployment.environment", "fieldContext": "resource", "fieldDataType": "string"}
            ],
            "legend": "{{k8s.pod.name}} ({{deployment.environment}})"
          }
        }
      ]
    },
    "selectedQueryName": "A",
    "thresholds": {
      "kind": "basic",
      "spec": [
        {"name": "critical", "op": "above", "matchType": "at_least_once", "target": 0, "channels": ["slack-payments", "pagerduty-payments"]}
      ]
    }
  },
  "evaluation": {"kind": "rolling", "spec": {"evalWindow": "5m", "frequency": "1m"}},
  "notificationSettings": {
    "groupBy": ["k8s.pod.name", "deployment.environment"],
    "renotify": {"enabled": true, "interval": "15m", "alertStates": ["firing"]}
  },
  "labels": {"severity": "critical", "team": "payments"},
  "annotations": {
    "description": "{{$k8s.pod.name}} emitted {{$value}} panic log(s) in {{$deployment.environment}}.",
    "summary": "Payments service panic"
  }
}
` + "```" + `

## 6. logs_error_rate_formula — logs error rate (error count / total count × 100)
Two disabled log count queries (A = errors, B = total) combined via a builder_formula.

` + "```" + `json
{
  "alert": "Payments-api error log rate above 1%",
  "alertType": "LOGS_BASED_ALERT",
  "description": "Error log ratio as a percentage of total logs for payments-api",
  "ruleType": "threshold_rule",
  "version": "v5",
  "schemaVersion": "v2alpha1",
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "panelType": "graph",
      "unit": "percent",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A", "signal": "logs", "stepInterval": 60, "disabled": true,
            "aggregations": [{"expression": "count()"}],
            "filter": {"expression": "service.name = 'payments-api' AND severity_text IN ['ERROR', 'FATAL']"},
            "groupBy": [{"name": "deployment.environment", "fieldContext": "resource", "fieldDataType": "string"}]
          }
        },
        {
          "type": "builder_query",
          "spec": {
            "name": "B", "signal": "logs", "stepInterval": 60, "disabled": true,
            "aggregations": [{"expression": "count()"}],
            "filter": {"expression": "service.name = 'payments-api'"},
            "groupBy": [{"name": "deployment.environment", "fieldContext": "resource", "fieldDataType": "string"}]
          }
        },
        {
          "type": "builder_formula",
          "spec": {"name": "F1", "expression": "(A / B) * 100", "legend": "{{deployment.environment}}"}
        }
      ]
    },
    "selectedQueryName": "F1",
    "thresholds": {
      "kind": "basic",
      "spec": [
        {"name": "critical", "op": "above", "matchType": "at_least_once", "target": 1, "channels": ["slack-payments"]}
      ]
    }
  },
  "evaluation": {"kind": "rolling", "spec": {"evalWindow": "5m", "frequency": "1m"}},
  "notificationSettings": {
    "groupBy": ["deployment.environment"],
    "renotify": {"enabled": true, "interval": "30m", "alertStates": ["firing"]}
  },
  "labels": {"severity": "critical", "team": "payments"},
  "annotations": {
    "description": "Error log rate in {{$deployment.environment}} is {{$value}}%",
    "summary": "Payments-api error rate above {{$threshold}}%"
  }
}
` + "```" + `

## 7. traces_threshold_latency — traces p99 with unit conversion (ns → s)
Builder query against the traces signal with p99(duration_nano). The series unit is ns, but the threshold target is in seconds (targetUnit: "s") — ByLonis converts during evaluation.

` + "```" + `json
{
  "alert": "Search API p99 latency above 5s",
  "alertType": "TRACES_BASED_ALERT",
  "description": "p99 duration of the search endpoint exceeds 5s",
  "ruleType": "threshold_rule",
  "version": "v5",
  "schemaVersion": "v2alpha1",
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "panelType": "graph",
      "unit": "ns",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A", "signal": "traces", "stepInterval": 60,
            "aggregations": [{"expression": "p99(duration_nano)"}],
            "filter": {"expression": "service.name = 'search-api' AND name = 'GET /api/v1/search'"},
            "groupBy": [
              {"name": "service.name", "fieldContext": "resource", "fieldDataType": "string"},
              {"name": "http.route", "fieldContext": "attribute", "fieldDataType": "string"}
            ],
            "legend": "{{service.name}} {{http.route}}"
          }
        }
      ]
    },
    "selectedQueryName": "A",
    "thresholds": {
      "kind": "basic",
      "spec": [
        {"name": "warning", "op": "above", "matchType": "at_least_once", "target": 5, "targetUnit": "s", "channels": ["slack-search"]}
      ]
    }
  },
  "evaluation": {"kind": "rolling", "spec": {"evalWindow": "5m", "frequency": "1m"}},
  "notificationSettings": {
    "groupBy": ["service.name", "http.route"],
    "renotify": {"enabled": true, "interval": "30m", "alertStates": ["firing"]}
  },
  "labels": {"severity": "warning", "team": "search"},
  "annotations": {
    "description": "p99 latency for {{$service.name}} on {{$http.route}} crossed {{$threshold}}s.",
    "summary": "Search-api latency degraded"
  }
}
` + "```" + `

## 8. traces_error_rate_formula — traces error rate (error spans / total spans × 100)
Two disabled trace count queries (A = error spans, B = total spans) combined via a builder_formula.

` + "```" + `json
{
  "alert": "Search-api error rate above 5%",
  "alertType": "TRACES_BASED_ALERT",
  "description": "Request error rate for search-api, grouped by route",
  "ruleType": "threshold_rule",
  "version": "v5",
  "schemaVersion": "v2alpha1",
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "panelType": "graph",
      "unit": "percent",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A", "signal": "traces", "stepInterval": 60, "disabled": true,
            "aggregations": [{"expression": "count()"}],
            "filter": {"expression": "service.name = 'search-api' AND hasError = true"},
            "groupBy": [
              {"name": "service.name", "fieldContext": "resource", "fieldDataType": "string"},
              {"name": "http.route", "fieldContext": "attribute", "fieldDataType": "string"}
            ]
          }
        },
        {
          "type": "builder_query",
          "spec": {
            "name": "B", "signal": "traces", "stepInterval": 60, "disabled": true,
            "aggregations": [{"expression": "count()"}],
            "filter": {"expression": "service.name = 'search-api'"},
            "groupBy": [
              {"name": "service.name", "fieldContext": "resource", "fieldDataType": "string"},
              {"name": "http.route", "fieldContext": "attribute", "fieldDataType": "string"}
            ]
          }
        },
        {
          "type": "builder_formula",
          "spec": {"name": "F1", "expression": "(A / B) * 100", "legend": "{{service.name}} {{http.route}}"}
        }
      ]
    },
    "selectedQueryName": "F1",
    "thresholds": {
      "kind": "basic",
      "spec": [
        {"name": "critical", "op": "above", "matchType": "at_least_once", "target": 5, "channels": ["slack-search", "pagerduty-search"]}
      ]
    }
  },
  "evaluation": {"kind": "rolling", "spec": {"evalWindow": "5m", "frequency": "1m"}},
  "notificationSettings": {
    "groupBy": ["service.name", "http.route"],
    "renotify": {"enabled": true, "interval": "15m", "alertStates": ["firing"]}
  },
  "labels": {"severity": "critical", "team": "search"},
  "annotations": {
    "description": "Error rate on {{$service.name}} {{$http.route}} is {{$value}}%",
    "summary": "Search-api error rate above {{$threshold}}%"
  }
}
` + "```" + `

## 9. tiered_thresholds — tiered thresholds with per-tier channels and alertOnAbsent
Two tiers (warning and critical) in a single rule, each with its own target, op, matchType, and channels. alertOnAbsent + absentFor fires a no-data alert when the query returns no series for 15 consecutive evaluations (15 minutes when frequency=1m).

` + "```" + `json
{
  "alert": "Kafka consumer lag warn / critical",
  "alertType": "METRIC_BASED_ALERT",
  "description": "Warn at lag >= 50 and page at >= 200, tiered via thresholds.spec.",
  "ruleType": "threshold_rule",
  "version": "v5",
  "schemaVersion": "v2alpha1",
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "panelType": "graph",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A", "signal": "metrics", "stepInterval": 60, "disabled": true,
            "aggregations": [{"metricName": "kafka_log_end_offset", "timeAggregation": "max", "spaceAggregation": "max"}],
            "filter": {"expression": "topic != '__consumer_offsets'"},
            "groupBy": [
              {"name": "topic", "fieldContext": "attribute", "fieldDataType": "string"},
              {"name": "partition", "fieldContext": "attribute", "fieldDataType": "string"}
            ]
          }
        },
        {
          "type": "builder_query",
          "spec": {
            "name": "B", "signal": "metrics", "stepInterval": 60, "disabled": true,
            "aggregations": [{"metricName": "kafka_consumer_committed_offset", "timeAggregation": "max", "spaceAggregation": "max"}],
            "filter": {"expression": "topic != '__consumer_offsets'"},
            "groupBy": [
              {"name": "topic", "fieldContext": "attribute", "fieldDataType": "string"},
              {"name": "partition", "fieldContext": "attribute", "fieldDataType": "string"}
            ]
          }
        },
        {
          "type": "builder_formula",
          "spec": {"name": "F1", "expression": "A - B", "legend": "{{topic}}/{{partition}}"}
        }
      ]
    },
    "alertOnAbsent": true,
    "absentFor": 15,
    "selectedQueryName": "F1",
    "thresholds": {
      "kind": "basic",
      "spec": [
        {"name": "warning", "op": "above", "matchType": "all_the_times", "target": 50, "channels": ["slack-kafka-info"]},
        {"name": "critical", "op": "above", "matchType": "all_the_times", "target": 200, "channels": ["slack-kafka-alerts", "pagerduty-kafka"]}
      ]
    }
  },
  "evaluation": {"kind": "rolling", "spec": {"evalWindow": "5m", "frequency": "1m"}},
  "notificationSettings": {
    "groupBy": ["topic"],
    "renotify": {"enabled": true, "interval": "15m", "alertStates": ["firing"]}
  },
  "labels": {"team": "data-platform"},
  "annotations": {
    "description": "Consumer lag for {{$topic}} partition {{$partition}} is {{$value}}.",
    "summary": "Kafka consumer lag"
  }
}
` + "```" + `

## 10. notification_settings — full notificationSettings surface
Demonstrates groupBy (noise control), newGroupEvalDelay (grace period for new series), renotify on both firing and nodata states, and usePolicy: false (per-threshold channels rather than org-level routing).

` + "```" + `json
{
  "alert": "API 5xx error rate above 1%",
  "alertType": "TRACES_BASED_ALERT",
  "description": "Noise-controlled 5xx error rate alert with renotify on gaps",
  "ruleType": "threshold_rule",
  "version": "v5",
  "schemaVersion": "v2alpha1",
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "panelType": "graph",
      "unit": "percent",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A", "signal": "traces", "stepInterval": 60, "disabled": true,
            "aggregations": [{"expression": "count()"}],
            "filter": {"expression": "service.name CONTAINS 'api' AND http.status_code >= 500"},
            "groupBy": [
              {"name": "service.name", "fieldContext": "resource", "fieldDataType": "string"},
              {"name": "deployment.environment", "fieldContext": "resource", "fieldDataType": "string"}
            ]
          }
        },
        {
          "type": "builder_query",
          "spec": {
            "name": "B", "signal": "traces", "stepInterval": 60, "disabled": true,
            "aggregations": [{"expression": "count()"}],
            "filter": {"expression": "service.name CONTAINS 'api'"},
            "groupBy": [
              {"name": "service.name", "fieldContext": "resource", "fieldDataType": "string"},
              {"name": "deployment.environment", "fieldContext": "resource", "fieldDataType": "string"}
            ]
          }
        },
        {
          "type": "builder_formula",
          "spec": {"name": "F1", "expression": "(A / B) * 100", "legend": "{{service.name}} ({{deployment.environment}})"}
        }
      ]
    },
    "selectedQueryName": "F1",
    "thresholds": {
      "kind": "basic",
      "spec": [
        {"name": "critical", "op": "above", "matchType": "at_least_once", "target": 1, "channels": ["slack-api-alerts", "pagerduty-oncall"]}
      ]
    }
  },
  "evaluation": {"kind": "rolling", "spec": {"evalWindow": "5m", "frequency": "1m"}},
  "notificationSettings": {
    "groupBy": ["service.name", "deployment.environment"],
    "newGroupEvalDelay": "2m",
    "usePolicy": false,
    "renotify": {"enabled": true, "interval": "30m", "alertStates": ["firing", "nodata"]}
  },
  "labels": {"team": "platform"},
  "annotations": {
    "description": "{{$service.name}} 5xx rate in {{$deployment.environment}} is {{$value}}%.",
    "summary": "API service error rate elevated"
  }
}
` + "```" + `

## 11. metric_cost_meter — Cost Meter budget alert (cumulative window, source "meter")

Fires when today's total log ingestion exceeds 10 GiB. The query targets Cost Meter (source "meter") with timeAggregation increase; the threshold uses matchType in_total over a cumulative daily window. For a $-denominated budget, add a builder_formula converting bytes to cost (e.g. "(A / 1e9) * cost_per_gb") and set selectedQueryName to the formula.

` + "```" + `json
{
  "alert": "Daily log ingestion budget",
  "alertType": "METRIC_BASED_ALERT",
  "ruleType": "threshold_rule",
  "condition": {
    "compositeQuery": {
      "queryType": "builder",
      "queries": [
        {
          "type": "builder_query",
          "spec": {
            "name": "A",
            "signal": "metrics",
            "source": "meter",
            "stepInterval": 3600,
            "aggregations": [
              {"metricName": "bylonis.meter.log.size", "timeAggregation": "increase", "spaceAggregation": "sum"}
            ]
          }
        }
      ]
    },
    "selectedQueryName": "A",
    "thresholds": {
      "kind": "basic",
      "spec": [
        {"name": "critical", "target": 10737418240, "op": "above", "matchType": "in_total", "channels": ["my-channel"]}
      ]
    }
  },
  "evaluation": {
    "kind": "cumulative",
    "spec": {"schedule": {"type": "daily", "minute": 0, "hour": 0}, "frequency": "1m", "timezone": "UTC"}
  }
}
` + "```" + `

## Key Notes
1. Metrics signal → object aggregation shape ({metricName, timeAggregation, spaceAggregation}). Logs/traces → expression shape ({expression: "count()"}).
2. selectedQueryName should reference the query or formula that determines the alert.
3. Use bylonis_get_alert to inspect existing alerts for the exact format your ByLonis version expects.
4. Channel names in thresholds.spec[].channels must match exactly the names from bylonis_list_notification_channels.
5. For threshold_rule/promql_rule, schemaVersion/evaluation/notificationSettings are auto-generated if omitted. For anomaly_rule, supply evalWindow/frequency at the top level and op/matchType/target/algorithm/seasonality under condition — no thresholds block, no auto-generated evaluation.
6. absentFor is in minutes (= consecutive evaluation cycles when frequency is 1m).
7. holdDuration (top level) is how long the condition must hold before firing; evalWindow is how much data each evaluation reads.
`
