# ByLonis MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server that gives AI assistants access to the observability data of a ByLonis instance: metrics, traces, logs, alerts, dashboards, saved views, notification channels and services, queried in natural language, and the themes of the interface.

ByLonis MCP Server is a fork of SigNoz MCP Server (Apache-2.0); see [NOTICE](NOTICE).

## Connect to your ByLonis instance

Every ByLonis tenant runs its own MCP server at:

```
https://<tenant>.bylonis.ai/mcp
```

Requests authenticate with a service account API key in the `BYLONIS-API-KEY` header. Create the key in **Settings → Service Accounts**; **Settings → MCP Server** shows ready-to-paste snippets for Cursor, VS Code, Claude Code, Claude Desktop and Codex.

Claude Code:

```bash
claude mcp add --transport http bylonis https://<tenant>.bylonis.ai/mcp \
  --header "BYLONIS-API-KEY: <your-api-key>"
```

Any client that accepts a URL plus headers (Cursor `.cursor/mcp.json`, for example):

```json
{
  "mcpServers": {
    "bylonis": {
      "url": "https://<tenant>.bylonis.ai/mcp",
      "headers": { "BYLONIS-API-KEY": "<your-api-key>" }
    }
  }
}
```

Since ByLonis v1.0 the upstream `SIGNOZ-API-KEY` and `X-SigNoz-*` headers are no longer accepted: a request that only carries `SIGNOZ-API-KEY` gets `401` (unless the server has its own `BYLONIS_API_KEY`). Use the names under [Request headers](#request-headers).

## Running the server

The image is published as `ghcr.io/bylonis/bylonis-mcp-server`. To build it or the binary from source:

```bash
docker build --build-arg VERSION=dev -t bylonis-mcp-server .
make build   # bin/bylonis-mcp-server
```

### Stdio mode

Point your MCP client at the binary; the server talks to one ByLonis instance:

```json
{
  "mcpServers": {
    "bylonis": {
      "command": "/absolute/path/to/bylonis-mcp-server",
      "env": {
        "BYLONIS_URL": "https://<tenant>.bylonis.ai",
        "BYLONIS_API_KEY": "<your-api-key>"
      }
    }
  }
}
```

### HTTP mode

```bash
BYLONIS_URL=https://<tenant>.bylonis.ai \
TRANSPORT_MODE=http \
MCP_SERVER_PORT=8000 \
./bylonis-mcp-server
```

Clients then send their key in `BYLONIS-API-KEY` (as in the snippets above). If `BYLONIS_API_KEY` is also set on the server, clients need no header. Without `BYLONIS_URL`, each request must carry the instance URL in `X-ByLonis-URL`.

With `OAUTH_ENABLED=true` (plus `OAUTH_TOKEN_SECRET` and `OAUTH_ISSUER_URL`) clients only need the URL: they discover the OAuth endpoints, open a browser page that asks for the ByLonis instance URL and API key, and exchange them for scoped tokens.

### Request headers

| Header | Purpose |
|--------|---------|
| `BYLONIS-API-KEY` | Service account API key. `Authorization: Bearer <token>` is also accepted. |
| `X-ByLonis-URL` | Instance URL for this request, when the server has no `BYLONIS_URL`. |
| `X-ByLonis-Client-Source`, `X-ByLonis-Assistant-Thread-Id`, `X-ByLonis-Assistant-Execution-Id` | Optional caller correlation, recorded on logs, traces and metrics. |

The server's variables are listed under [Environment Variables](#environment-variables).

### HTTP probe endpoints

| Endpoint | Purpose |
|----------|---------|
| `/livez` | Liveness. `200 OK` whenever the process answers HTTP. |
| `/readyz` | Readiness. `200 OK` once the server can take traffic. |
| `/healthz` | Legacy alias of `/readyz`. |

## What can you do with it?

```
"Show me all available metrics"
"What's the p99 latency for http_request_duration_seconds?"
"List all active alerts"
"Show me error logs for the paymentservice from the last hour"
"How many errors per service in the last hour?"
"Search traces for the checkout service from the last hour"
"Get details for trace ID abc123"
"Create a dashboard with CPU and memory widgets"
"Why is the agent of compute-n07 disconnected?"
"What changed on storage-02 before yesterday's incident?"
"Which alert rules make noise, and what hold would fix them?"
"What is new in the logs of the API since yesterday?"
"Is the collector of lumina-k3s up to date, and how is its upgrade going?"
```

## Available Tools

> **Compatibility:** alert-rule tools target `/api/v2/rules/*` and notification-channel tools the render-envelope `/api/v1/channels/*` routes; every ByLonis release has both.

> **Tool metadata:** every tool accepts `searchContext`, the user's original question/search text. It is used for MCP observability and is not forwarded to the ByLonis API.

| Tool | Description |
|------|-------------|
| `bylonis_list_metrics` | Search and list available metrics |
| `bylonis_query_metrics` | Query metrics with smart aggregation defaults |
| `bylonis_get_field_keys` | Discover available field keys for metrics, traces, or logs |
| `bylonis_get_field_values` | Get possible values for a field key |
| `bylonis_list_alerts` | List firing/silenced/inhibited Alertmanager alert *instances* (not rule definitions) |
| `bylonis_list_alert_rules` | List configured alert rules, including inactive/OK and disabled rules |
| `bylonis_get_alert` | Get an alert rule definition by ID via GET /api/v2/rules/{ruleId} |
| `bylonis_get_alert_history` | Get alert state history timeline for a rule, or how many times every rule fired (without `ruleId`) |
| `bylonis_create_alert` | Create an alert rule via POST /api/v2/rules; v2alpha1 for threshold/promql and for anomaly (`thresholds.kind` anomaly) |
| `bylonis_update_alert` | Update an alert rule by UUIDv7 via PUT /api/v2/rules/{ruleId} |
| `bylonis_delete_alert` | Delete an alert rule by UUIDv7 via DELETE /api/v2/rules/{ruleId} |
| `bylonis_acknowledge_alert` | Acknowledge a firing alert (someone works on it) via POST /api/v2/alert_acknowledgements; note required |
| `bylonis_create_maintenance_window` | Silence named alert rules for a fixed window (max 24h, reason required) via POST /api/v1/downtime_schedules |
| `bylonis_delete_maintenance_window` | Remove a maintenance window via DELETE /api/v1/downtime_schedules/{id} |
| `bylonis_ask_ia_ops` | Ask IA OPS (the SRE agent) a question; returns its conclusion and evidence. Read-only, costs model tokens. Needs `IA_OPS_URL` |
| `bylonis_ai_gateway_usage` | AI Gateway usage by team, application, model or provider in a time range: requests, rejected, tokens and cost (USD). Read-only |
| `bylonis_ai_gateway_pool` | The AI Gateway pool of provider accounts: provider, scope, status, models, RPM and last connection check. Never a credential. Read-only |
| `bylonis_ai_gateway_budgets` | The AI Gateway budgets with what each spent in its window: percent, remaining, and which are exhausted, stop before 100 % or have a stale report. Read-only |
| `bylonis_list_dashboards` | List all dashboards with summaries |
| `bylonis_get_dashboard` | Get full dashboard configuration |
| `bylonis_create_dashboard` | Create a new dashboard |
| `bylonis_update_dashboard` | Update an existing dashboard |
| `bylonis_delete_dashboard` | Delete a dashboard by UUID |
| `bylonis_list_services` | List services within a time range |
| `bylonis_get_service_top_operations` | Get top operations for a service |
| `bylonis_list_views` | List saved Explorer views for a sourcePage (traces/logs/metrics) |
| `bylonis_get_view` | Get a saved view by UUID |
| `bylonis_create_view` | Create a new saved Explorer view |
| `bylonis_update_view` | Replace an existing saved view (full-body PUT) |
| `bylonis_delete_view` | Delete a saved view by UUID |
| `bylonis_aggregate_logs` | Aggregate logs (count, avg, p99, etc.) with grouping |
| `bylonis_search_logs` | Search logs with flexible filtering |
| `bylonis_aggregate_traces` | Aggregate trace statistics with grouping |
| `bylonis_search_traces` | Search traces with flexible filtering |
| `bylonis_get_trace_details` | Get full trace with all spans |
| `bylonis_execute_builder_query` | Execute a raw Query Builder v5 query |
| `bylonis_list_notification_channels` | List notification channels |
| `bylonis_get_notification_channel` | Get a single notification channel by ID |
| `bylonis_create_notification_channel` | Create a notification channel and send a test notification |
| `bylonis_update_notification_channel` | Update a notification channel and send a test notification |
| `bylonis_delete_notification_channel` | Delete a notification channel by ID |
| `bylonis_list_fleet_nodes` | List the nodes of the Fleet Control inventory, filtered by search, label selector, site, status, kind or agent |
| `bylonis_get_fleet_node` | The card of one node by hostname: status, OpAMP agent, health of its collector and last runner jobs |
| `bylonis_get_fleet_node_history` | What changed on a node (fields, labels, groups, agents, Ansible runs), who did it, in a range |
| `bylonis_get_fleet_coverage` | Machines sending telemetry outside the inventory, silent agents and nodes without an agent |
| `bylonis_list_fleet_diagnostics` | List the diagnostics (flares) of a node or an agent of a cluster |
| `bylonis_get_fleet_diagnostic` | Read a diagnostic of an agent: what ByLonis knows and the logs and configurations of its host, censored |
| `bylonis_list_fleets` | List the fleets of Fleet Control with their configuration versions |
| `bylonis_get_fleet_config` | Get a configuration template of a fleet (the newest by default) |
| `bylonis_validate_fleet_config` | Validate a configuration for a fleet in dry run, saving nothing |
| `bylonis_propose_fleet_config` | Save a validated configuration as the next version of a fleet, **not deployed** |
| `bylonis_list_playbooks` | List the Ansible playbooks |
| `bylonis_get_playbook` | Get a playbook with the YAML of a version and its versions |
| `bylonis_propose_playbook` | Save a playbook, or its next version, in the library: nothing runs |
| `bylonis_check_playbook` | Check a playbook with `--check --diff` on a selector or group: nothing changes |
| `bylonis_get_playbook_run` | Get the check and run of a playbook, host by host |
| `bylonis_list_fleet_agent_health` | Health of the collectors of the fleet from their own metrics, optionally only those with problems |
| `bylonis_get_design_surface` | What a theme is made of: tokens and groups, personalities, contrast pairs, color formats, built-in themes |
| `bylonis_generate_theme` | Generate a theme (light and dark) from a base color, logo colors and/or a personality, **not saved** |
| `bylonis_patch_theme_tokens` | Change some tokens of a theme: new modes, diff and contrast report, **not saved** |
| `bylonis_validate_theme` | Validate a theme: tokens, colors and the WCAG contrast of every semantic pair |
| `bylonis_list_themes` | List the themes of the organization with their contrast status |
| `bylonis_get_theme` | Get a theme with its colors and the pairs that fail the contrast |
| `bylonis_save_theme` | Create a theme or update one (administrator API key) |
| `bylonis_list_flapping_rules` | Rules that flap, with how many alerts each holdDuration would have avoided and the one ByLonis proposes |
| `bylonis_list_alert_acknowledgements` | Who acknowledged the alerts of the last days, when and with what note, also the resolved ones |
| `bylonis_list_maintenance_windows` | Planned maintenance windows, their schedule and the rules they cover |
| `bylonis_list_change_events` | Deploys, alerts and fleet deployments of a range, for "what changed before the incident?" |
| `bylonis_get_log_patterns` | Logs grouped by message template, optionally compared with an earlier range |
| `bylonis_backtest_alert` | Replay a rule over the last days and say how often it would have fired, **creating nothing** |
| `bylonis_list_collector_versions` | Versions of the collector, their status and advisories, and the version each fleet runs |
| `bylonis_list_fleet_upgrades` | Upgrades of the collector of the fleets, and the upgrade windows of a fleet |
| `bylonis_get_fleet_upgrade` | One upgrade with its hosts in their batches and its history |

<details>
<summary><strong>Parameter Reference</strong></summary>

#### `bylonis_list_metrics`

Search and list available metrics from ByLonis. Supports filtering by name substring, time range, and source.

- **Parameters**:
  - `searchText` (optional) - Filter metrics by name substring (e.g., 'cpu', 'memory')
  - `limit` (optional) - Maximum number of metrics to return (default: 50)
  - `start` (optional) - Start time in unix milliseconds
  - `end` (optional) - End time in unix milliseconds
  - `source` (optional) - Data-source filter. Use `"meter"` to list Cost Meter metrics — the usage/billing metrics ByLonis meters on (currently telemetry ingestion volume); omit for the default metrics store

#### `bylonis_query_metrics`

Query metrics with smart aggregation defaults and validation. Automatically applies the right timeAggregation and spaceAggregation based on metric type (gauge, counter, histogram). Auto-fetches metric metadata if not provided.

- **Parameters**:
  - `metricName` (required) - Metric name to query
  - `metricType` (optional) - gauge, sum, histogram, exponential_histogram (auto-fetched if absent)
  - `isMonotonic` (optional) - true/false (auto-fetched if absent)
  - `temporality` (optional) - cumulative, delta, unspecified (auto-fetched if absent)
  - `timeAggregation` (optional) - Aggregation over time (auto-defaulted by type)
  - `spaceAggregation` (optional) - Aggregation across dimensions (auto-defaulted by type)
  - `groupBy` (optional) - Comma-separated field names
  - `filter` (optional) - Filter expression
  - `timeRange` (optional) - Relative range: 30m, 1h, 6h, 24h, 7d (default: 1h; ignored when both `start` and `end` are provided)
  - `start`/`end` (optional) - Unix ms timestamps. When both are provided, they override `timeRange`
  - `stepInterval` (optional) - Step in seconds (auto-calculated if omitted)
  - `requestType` (optional) - time_series (default) or scalar
  - `reduceTo` (optional) - For scalar: sum, count, avg, min, max, last, median
  - `formula` (optional) - Expression over named queries (e.g., "A / B * 100")
  - `formulaQueries` (optional) - JSON array of additional named metric queries for formula
  - `source` (optional) - Data-source filter. Use `"meter"` to query Cost Meter data; omit for the default metrics store

#### `bylonis_list_alerts`

Lists currently firing/silenced/inhibited alert *instances* from Alertmanager — **not** rule definitions. Use `bylonis_list_alert_rules` for configured rules, `bylonis_get_alert` with a `ruleId` for one full rule definition, or `bylonis_get_alert_history` for the state timeline.

An acknowledged alert (bylonis#595) also has `acknowledgedBy`, `acknowledgedAt` and `acknowledgementNote`; every alert has the `series` and `firedAt` that identify it. Acknowledging is done by a person in ByLonis, not with the MCP.

#### `bylonis_list_alert_rules`

Lists configured alert rules from `GET /api/v2/rules`, including inactive/OK and disabled rules. Returns compact summaries with `ruleId`, `alert`, `alertType`, `ruleType`, `state`, `disabled`, `severity`, `labels`, `createdAt`, and `updatedAt`.

- **Parameters**:
  - `limit` (optional) - Maximum number of rules to return (default: 50)
  - `offset` (optional) - Number of rules to skip for pagination (default: 0)

#### `bylonis_get_alert`

Gets the rule definition for an alert (`GET /api/v2/rules/{ruleId}`).

- **Parameters**: `ruleId` (required) - Alert rule ID (UUIDv7).
- Returns the rule with its audit fields `createdAt/updatedAt/createdBy/updatedBy`.

#### `bylonis_list_dashboards`

Lists all dashboards with summaries (name, UUID, description, tags).

#### `bylonis_get_dashboard`

Gets complete dashboard configuration.

- **Parameters**: `uuid` (required) - Dashboard UUID

#### `bylonis_create_dashboard`

Creates a dashboard.

- **Parameters:**
  - `title` (required) – Dashboard name
  - `description` (optional) – Short summary of what the dashboard shows
  - `tags` (optional) – List of tags
  - `layout` (required) – Widget positioning grid
  - `variables` (optional) – Map of variables available for use in queries
  - `widgets` (required) – List of widgets added to the dashboard

#### `bylonis_update_dashboard`

Updates an existing dashboard.

- **Parameters:**
  - `uuid` (required) – Unique identifier of the dashboard to update
  - `dashboard` (required) – Complete dashboard object representing the post-update state
    - `title` (required) – Dashboard name
    - `description` (optional) – Short summary of what the dashboard shows
    - `tags` (optional) – List of tags applied to the dashboard
    - `layout` (required) – Full widget positioning grid
    - `variables` (optional) – Map of variables available for use in queries
    - `widgets` (required) – Complete set of widgets defining the updated dashboard

#### `bylonis_list_services`

Lists all services within a time range.

- **Parameters**:
  - `timeRange` (optional) - Time range like '2h', '6h', '2d', '7d' (ignored when both `start` and `end` are provided)
  - `start` (optional) - Start time in nanoseconds (defaults to 6 hours ago)
  - `end` (optional) - End time in nanoseconds (defaults to now)

#### `bylonis_get_service_top_operations`

Gets top operations for a specific service.

- **Parameters**:
  - `service` (required) - Service name
  - `timeRange` (optional) - Time range like '2h', '6h', '2d', '7d' (ignored when both `start` and `end` are provided)
  - `start` (optional) - Start time in nanoseconds (defaults to 6 hours ago)
  - `end` (optional) - End time in nanoseconds (defaults to now)
  - `tags` (optional) - JSON array of tags

#### `bylonis_get_alert_history`

With `ruleId`, gets the alert history timeline of that rule. **Without `ruleId`**, answers "which alerts
fired, and how many times" in one call. The server has one history endpoint per rule, so the MCP queries
every rule's firing transitions in parallel (8 at a time) and returns:

- `fired`: for each rule that fired, its `firings` count, `lastFiredAt` (RFC 3339, UTC) and
  `lastFiredLabels` (the labels of that series), sorted by `firings`;
- `notFired`: the names of the rules that did not fire;
- `errors`: the rules whose history could not be read, if any.

Each row of the history is a state change, so the firing rows are the times the rule fired: the count
the alert's page shows. Before, a question about all alerts took one call per rule (bylonis#473).

- **Parameters**:
  - `ruleId` (optional) - Alert rule ID. Omit it for the summary of all rules
  - `timeRange` (optional) - Time range like '2h', '6h', '2d', '7d' (ignored when both `start` and `end` are provided)
  - `start` (optional) - Start timestamp in milliseconds (defaults to 6 hours ago)
  - `end` (optional) - End timestamp in milliseconds (defaults to now)
  - `state` (optional, with `ruleId`) - 'firing' or 'inactive'
  - `offset` (optional, with `ruleId`) - Offset for pagination (default: 0)
  - `limit` (optional, with `ruleId`) - Limit number of results (default: 20)
  - `order` (optional, with `ruleId`) - Sort order: 'asc' or 'desc' (default: 'asc')

#### `bylonis_list_views`

List ByLonis saved Explorer views for a given sourcePage. Supports pagination; response includes a `pagination` block with `total`, `hasMore`, and `nextOffset`.

- **Parameters**:
  - `sourcePage` (required) - One of: `traces`, `logs`, `metrics`
  - `name` (optional) - Partial-match filter on view name (server-side)
  - `category` (optional) - Partial-match filter on view category (server-side)
  - `limit` (optional) - Page size (default: 50)
  - `offset` (optional) - Number of results to skip (default: 0)

#### `bylonis_get_view`

Get a single saved view by UUID.

- **Parameters**: `viewId` (required) - Saved view UUID

#### `bylonis_create_view`

Create a new saved Explorer view.

- **Parameters**: JSON payload matching the `SavedView` schema.
- **Tip**: Read MCP resources `bylonis://view/instructions` and `bylonis://view/examples` before composing payloads.

#### `bylonis_update_view`

Replace an existing saved view (full-body PUT).

- **Parameters**:
  - `viewId` (required) - UUID of the view to replace
  - `view` (required) - Full `SavedView` object (`name`, `sourcePage`, `compositeQuery`, plus any of `category`, `tags`, `extraData`)
- **Tip**: Read MCP resources `bylonis://view/instructions` and `bylonis://view/examples` before composing payloads. Call `bylonis_get_view` first, pass its `data` object under `view` with whichever fields changed. Partial bodies wipe unspecified fields.

#### `bylonis_delete_view`

Delete a saved view by UUID.

- **Parameters**: `viewId` (required) - Saved view UUID



#### `bylonis_aggregate_logs`

Aggregate logs with count, average, sum, min, max, or percentiles, optionally grouped by fields.

- **Parameters**:
  - `aggregation` (required) - Aggregation function: count, count_distinct, avg, sum, min, max, p50, p75, p90, p95, p99, rate
  - `aggregateOn` (optional) - Field to aggregate on (required for all except count and rate)
  - `groupBy` (optional) - Comma-separated fields to group by (e.g., 'service.name, severity_text')
  - `filter` (optional) - Filter expression using ByLonis search syntax
  - `service` (optional) - Shortcut filter for service name
  - `severity` (optional) - Shortcut filter for severity (DEBUG, INFO, WARN, ERROR, FATAL)
  - `orderBy` (optional) - Order expression and direction (e.g., 'count() desc')
  - `limit` (optional) - Maximum number of groups to return (default: 10, max: 10000; higher values are clamped to bound server memory)
  - `timeRange` (optional) - Time range like '30m', '1h', '6h', '24h' (default: '1h'; ignored when both `start` and `end` are provided)
  - `start` / `end` (optional) - Start/end time in milliseconds. When both are provided, they override `timeRange`

#### `bylonis_search_logs`

Search logs with flexible filtering across all services.

- **Parameters**:
  - `query` (optional) - Filter expression using ByLonis search syntax (e.g., "service.name = 'payment-svc' AND http.status_code >= 400")
  - `service` (optional) - Service name to filter by
  - `severity` (optional) - Severity filter (DEBUG, INFO, WARN, ERROR, FATAL)
  - `searchText` (optional) - Text to search for in log body (uses CONTAINS matching)
  - `timeRange` (optional) - Time range like '30m', '1h', '6h', '24h' (default: '1h'; ignored when both `start` and `end` are provided)
  - `start` / `end` (optional) - Start/end time in milliseconds. When both are provided, they override `timeRange`
  - `limit` (optional) - Maximum number of logs to return (default: 100, max: 10000; higher values are clamped — paginate with `offset`)
  - `offset` (optional) - Offset for pagination (default: 0)

#### `bylonis_get_field_keys`

Get available field keys for a given signal (metrics, traces, or logs).

- **Parameters**:
  - `signal` (required) - Signal type: `metrics`, `traces`, or `logs`
  - `searchText` (optional) - Filter field keys by name substring
  - `metricName` (optional) - Filter by metric name (relevant for metrics signal)
  - `fieldContext` (optional) - Filter by field context (e.g., `resource`, `span`)
  - `fieldDataType` (optional) - Filter by data type (e.g., `string`, `int64`)
  - `source` (optional) - Filter by source

#### `bylonis_get_field_values`

Get possible values for a specific field key for a given signal.

- **Parameters**:
  - `signal` (required) - Signal type: `metrics`, `traces`, or `logs`
  - `name` (required) - Field key name to get values for (e.g., `service.name`, `http.method`)
  - `searchText` (optional) - Filter values by substring
  - `metricName` (optional) - Filter by metric name (relevant for metrics signal)
  - `source` (optional) - Filter by source


#### `bylonis_search_traces`

Search traces/spans with flexible filtering.

- **Parameters**:
  - `query` (optional) - Filter expression using ByLonis search syntax (e.g., "service.name = 'payment-svc' AND hasError = true")
  - `service` (optional) - Service name to filter by
  - `operation` (optional) - Operation/span name to filter by
  - `error` (optional) - Filter by error status ('true' or 'false')
  - `minDuration` / `maxDuration` (optional) - Min/max span duration in nanoseconds (e.g., '500000000' for 500ms)
  - `timeRange` (optional) - Time range like '30m', '1h', '6h', '24h' (default: '1h'; ignored when both `start` and `end` are provided)
  - `start` / `end` (optional) - Start/end time in milliseconds. When both are provided, they override `timeRange`
  - `limit` (optional) - Maximum number of traces to return (default: 100, max: 10000; higher values are clamped — paginate with `offset`)
  - `offset` (optional) - Offset for pagination (default: 0)

#### `bylonis_aggregate_traces`

Aggregate trace statistics like count, average, sum, min, max, or percentiles over spans, optionally grouped by fields.

- **Parameters**:
  - `aggregation` (required) - Aggregation function: count, count_distinct, avg, sum, min, max, p50, p75, p90, p95, p99, rate
  - `aggregateOn` (optional) - Field to aggregate on (e.g., 'durationNano'). Required for all except count and rate
  - `groupBy` (optional) - Comma-separated fields to group by (e.g., 'service.name, name')
  - `filter` (optional) - Filter expression using ByLonis search syntax
  - `service` (optional) - Shortcut filter for service name
  - `operation` (optional) - Shortcut filter for span/operation name
  - `error` (optional) - Shortcut filter for error spans ('true' or 'false')
  - `orderBy` (optional) - Order expression and direction (e.g., 'avg(durationNano) desc')
  - `limit` (optional) - Maximum number of groups to return (default: 10, max: 10000; higher values are clamped to bound server memory)
  - `timeRange` (optional) - Time range like '30m', '1h', '6h', '24h' (default: '1h'; ignored when both `start` and `end` are provided)
  - `start` / `end` (optional) - Start/end time in milliseconds. When both are provided, they override `timeRange`

#### `bylonis_get_trace_details`

Gets trace information including all spans and metadata.

- **Parameters**:
  - `traceId` (required) - Trace ID to get details for
  - `timeRange` (optional) - Time range like '2h', '6h', '2d', '7d' (ignored when both `start` and `end` are provided)
  - `start` (optional) - Start time in milliseconds (defaults to 6 hours ago)
  - `end` (optional) - End time in milliseconds (defaults to now)
  - `includeSpans` (optional) - Include detailed span information (true/false, default: true)



#### `bylonis_create_alert`

Create a new alert rule in ByLonis via `POST /api/v2/rules`.

- **Parameters**: JSON payload matching the ByLonis alert rule schema.
- **Schema varies by `ruleType`**:
  - `threshold_rule` / `promql_rule` → **v2alpha1** (structured `condition.thresholds`, `evaluation`, `notificationSettings`).
  - `anomaly_rule` → **v2alpha1** with `condition.thresholds.kind` = `anomaly` (bylonis#901): per tier `z` (2 high, 3 medium, 4 low sensitivity), `direction` (up/down/both), `minAnomalous` (N of the M points of the window) and an optional `recoveryZ`; `condition.seasonality` (none/hourly/daily/weekly) and an optional `condition.anomaly` (`resolution` 5m or 1h, `minExpected`, `acceptLevelShiftAfter`, `baselineFrom`). The evaluation is rolling: `evalWindow` a multiple of the resolution and `frequency` between 1m and the resolution (default 15m every 5m). It works on metrics, logs and traces builder queries.
  - The older **v1** anomaly shape (top-level `evalWindow`/`frequency`, `condition.op`/`matchType`/`target`/`algorithm`/`seasonality`, no `thresholds`) is still accepted, with a window that is a multiple of 5m and a frequency between 1m and 5m.
- **Tip**: Read MCP resources `bylonis://alert/instructions` and `bylonis://alert/examples` (ten canonical payloads plus a Cost Meter cumulative-budget example) before composing payloads. For `promql_rule`, also read `bylonis://promql/instructions` — OTel dotted metric names require the Prometheus 3.x UTF-8 quoted-selector form.
- **`holdDuration`** (bylonis#594, any `ruleType`): how long the condition must hold before the alert fires, between `0s` and `24h`, like the `for` of Prometheus.
- **Fleet rules**: rules with the label `fleet` belong to Inventario → Alertas. ByLonis rejects creating, updating or deleting them through the API with `400 fleet_managed_rule` (bylonis#589).

#### `bylonis_update_alert`

Update an existing alert rule via `PUT /api/v2/rules/{ruleId}`. Replaces the full rule configuration — fetch the current rule with `bylonis_get_alert` first and merge changes on top of it, `holdDuration` included. Not for fleet rules (label `fleet`).

- **Parameters**:
  - `ruleId` (required) - UUIDv7 of the rule to update (obtain from `bylonis_list_alert_rules` / `bylonis_get_alert`).
  - Plus all fields of the alert rule schema (same shape as `bylonis_create_alert`).

#### `bylonis_delete_alert`

Delete an alert rule via `DELETE /api/v2/rules/{ruleId}`. Irreversible — confirm with the user first. A firing rule sends its resolved notification and its alerts are closed in the history. Not for fleet rules (label `fleet`).

- **Parameters**:
  - `ruleId` (required) - UUIDv7 of the rule to delete. The server rejects non-UUIDv7 values with `invalid_input`.

#### `bylonis_acknowledge_alert`

Acknowledge a firing alert (bylonis#1052): someone works on it, and its channel is not notified again until something in its group changes. Allowed with the viewer role in ByLonis. The origin of the call (`X-ByLonis-Client-Source`) is added to the note.

- **Parameters**:
  - `ruleId` (required) - UUIDv7 of the rule.
  - `note` (required, at least 10 characters) - who works on it and what is being done.
  - `series`, `firedAt` (optional, together) - from `bylonis_list_alerts`; needed when the rule has several firing alerts. Without them the tool takes the only firing alert or lists the candidates.

#### `bylonis_create_maintenance_window`

Silence the notifications of named alert rules for a fixed window (bylonis#1052), e.g. during a planned intervention. Needs the editor role in ByLonis. Stricter than the API: the rules are required (an empty list would silence every rule), they must exist, the window lasts at most 24h and starts now or at `start` (not in the past), and a reason is required; the reason and the origin of the call go into the description.

- **Parameters**: `ruleIds` (required, comma-separated UUIDv7), `duration` (required, `<n>m` or `<n>h`, max 24h), `reason` (required, at least 10 characters), `start` (optional, RFC 3339), `name` (optional).

#### `bylonis_delete_maintenance_window`

Remove a maintenance window by id (from `bylonis_list_maintenance_windows`). Needs the editor role in ByLonis.

- **Parameters**: `id` (required).

#### `bylonis_ask_ia_ops`

Ask IA OPS, the SRE agent of ByLonis, a question (bylonis#1053) and get its conclusion with the evidence it used. Sent to `IA_OPS_URL/ask` with the same credential the client used (`BYLONIS-API-KEY` of a service account, or a session token) and its `X-ByLonis-Client-Source`. IA OPS runs a read-only turn without a user conversation (bounded in time, one at a time, a few per hour per account) and saves it as a conversation of the account (`agente:<name>`). It costs model tokens to the tenant. Registered only when `IA_OPS_URL` is set (e.g. `https://<tenant>.bylonis.ai/ia-ops-api`).

- **Parameters**: `question` (required, 3–2000 characters).

#### `bylonis_ai_gateway_usage`

How much the AI Gateway was used (bylonis#1348), from the gateway's own traces (`service.name` bylonis-aigateway, span `gateway.request`) through `/api/v5/query_range`: requests, rejected requests, unpriced requests, input and output tokens and cost in USD, sorted by cost. Teams come with their name (`/api/v1/ai/teams`) when the caller can read them. The SQL only takes columns from a fixed set and filter values that match `[A-Za-z0-9._:/-]`; anything else is refused before reaching ByLonis.

- **Parameters**: `groupBy` (team, app, model, provider; default `team,app,model`), `team` (id or name), `app`, `model`, `timeRange` (default `24h`), `start`, `end`, `limit` (default 20, at most 200).

#### `bylonis_ai_gateway_pool`

The pool of provider accounts of the AI Gateway (`/api/v1/ai/accounts`, organization and teams): provider, scope and team name, status (active, draining, disabled), endpoint, models, RPM, priority and the last connection check. It leaves out the credential's last four characters and the URL. Needs the permission to read the AI accounts.

- **Parameters**: `provider`, `status` (both optional).

#### `bylonis_ai_gateway_budgets`

The budgets of the AI Gateway (`/api/v1/ai-gateway/budgets`: organization, teams, users, keys and projects) with what each has spent in its current window as the gateway last reported it: limit, spent, reserved, remaining, percent, end of the window, policy and parent. Sorted by percent. `warnings` lists the budgets that are exhausted, that stop before 100 % (what is left is less than the last request reserved), whose report is stale, and those the gateway has not reported yet. Needs the permission to read the AI Gateway administration.

- **Parameters**: `level`, `name` (contains), `minPercent` (all optional).

#### `bylonis_delete_dashboard`

Delete a dashboard by UUID.

- **Parameters**: `uuid` (required) - Dashboard UUID to delete

#### `bylonis_list_notification_channels`

List notification channels configured in ByLonis.

- **Parameters**:
  - `limit` (optional) - Maximum number of channels to return (default: 50)
  - `offset` (optional) - Offset for pagination (default: 0)

#### `bylonis_create_notification_channel`

Create a notification channel and send a test notification.

- **Parameters**:
  - `type` (required) - Channel type: slack, webhook, pagerduty, email, opsgenie, msteams
  - `name` (required) - Channel name
  - Type-specific fields (required by channel type), such as `slack_api_url`, `webhook_url`, `pagerduty_routing_key`, `email_to`, `opsgenie_api_key`, or `msteams_webhook_url`
  - `email_headers` (optional) - JSON object of email headers, e.g. `{"Subject":"[{{ .CommonLabels.severity }}] {{ .CommonLabels.alertname }}"}`

#### `bylonis_update_notification_channel`

Update an existing notification channel and send a test notification.

- **Parameters**:
  - `id` (required) - Notification channel UUID
  - `type` (required) - Channel type
  - `name` (required) - Channel name
  - Full channel configuration fields for the selected channel type, except the secrets: omitted, the saved ones are kept (bylonis#272)
  - **Email**: the saved template (`email_html`), headers (`email_headers`, the subject) and SMTP connection are kept when omitted (bylonis#1023, bylonis#783). If the saved channel cannot be read, nothing is changed.

#### `bylonis_get_notification_channel`

Get a single notification channel by ID (`GET /api/v1/channels/{id}`).

- **Parameters**:
  - `id` (required) - Notification channel UUID

#### `bylonis_delete_notification_channel`

Delete a notification channel by ID (`DELETE /api/v1/channels/{id}`). Irreversible — warn if alert rules still reference this channel.

- **Parameters**:
  - `id` (required) - Notification channel UUID

#### `bylonis_execute_builder_query`

Executes a ByLonis Query Builder v5 query.

- **Parameters**: `query` (required) - Complete ByLonis Query Builder v5 JSON object
- **Query types**: the per-envelope `compositeQuery.queries[i].type` selects the spec shape:
  - `builder_query` — signal-specific spec (logs/traces/metrics) with filter, aggregations, groupBy, etc.
  - `builder_formula` — formula expression referencing other query names (e.g. `A / B * 100`).
  - `promql` — `{name, query, disabled, step?, legend?}`. PromQL for OTel metrics requires the Prometheus 3.x UTF-8 quoted-selector form `{"metric.name.with.dots"}`; read the `bylonis://promql/instructions` resource for details.
  - `clickhouse_sql` — `{name, query, disabled, legend?}`.

</details>

### Fleet tools (bylonis#463)

Read-only tools over the inventory of Fleet Control: they only send GETs under `/api/v1/inventory/` and
`/api/v1/fleet/`, with the key of the caller, so they see what that key may see. The API never returns the
secrets of the credentials, and the tools also drop the id of a node's credential, the enrollment token of
an agent, effective configurations and time series. While the tenant has Fleet Control off, they answer
404 and say so. The deployments of #450 will get their own tools when they exist.

#### `bylonis_get_fleet_node`

One call for "why is the agent of X disconnected?": the node (status and since when, last seen, IPs, site,
labels by source, attributes, groups), its OpAMP agent (connected or not, since when, last heartbeat,
versions), the health of its collector in the window (`window`, default `1h`: items accepted, refused,
sent and failed, queue, CPU, memory, uptime, problems) and the last 10 jobs of its site runner. A part
that fails is reported inside the answer and the rest still comes.

- **Parameters**: `hostname` (exact, case-insensitive; similar names are suggested when none matches) or
  `nodeId`; `window` (optional).

#### `bylonis_get_fleet_node_history`

For "what changed on this host before the incident?": the history of the node, newest first, with the
operation (create, update, delete, link, unlink, ansible), the field, old and new values, who and from
where (ui, api, agent, import, discovery, system).

- **Parameters**: `hostname` or `nodeId`; `timeRange` (`<n>m`, `<n>h`, `<n>d`; default `7d`) or `start`/`end`
  (RFC 3339 or milliseconds); `limit` (default 50, max 200).

#### `bylonis_get_fleet_diagnostic` and `bylonis_list_fleet_diagnostics` (bylonis#461)

For "why does the agent of X not send data, not connect or fail?": a diagnostic (flare) of the agent, which a
person asks for in the card of the node.
- **Sections from ByLonis:** the node, the agent and the health of each component, its effective configuration,
  its status against its fleet, its own metrics of the last hour, the history of the node and the jobs of its
  site runner.
- **Sections from the host,** which the site runner collected or someone uploaded: versions, system, services,
  configurations, connectivity, and the logs of the supervisor and the collector. For a cluster, the output of
  the kubectl commands.
- **Secrets:** ByLonis censors them (`<censurado>`).
- **Permission and audit:** reading a diagnostic needs the permission `diagnose` (granted in Inventario → Permisos),
  and ByLonis records the read in its audit.
- **Parameters:**
  - `bylonis_get_fleet_diagnostic`: `diagnosticId`, or `hostname` / `nodeId` for the newest one of a node;
    `sections` (comma-separated names); `maxChars` per section (default 20000, max 200000). A log keeps its end.
  - `bylonis_list_fleet_diagnostics`: `hostname`, `nodeId` or `agentId`.
- **Read only:** there is no tool to create one.

### Proposals of IA OPS (bylonis#459)

IA OPS can **propose** a configuration of the collector (a new version of a fleet, validated for every member)
and an Ansible playbook (a version in the library, then its `--check --diff`), marked "Propuesta de IA OPS" in
their notes. **Nothing is deployed nor run**: there is no tool to deploy a version or to approve a run, the
client only posts to the paths of the proposals, and ByLonis refuses the approval of a run with an API key. A
person deploys or approves in ByLonis.

### Alert quality, changes and log patterns (bylonis#1024)

Read-only tools over what ByLonis already computes. The client only reads `/api/v2/alert_quality/flapping`,
`/api/v2/alert_acknowledgements`, `/api/v1/events` and `/api/v1/downtime_schedules`, and only posts to
`/api/v5/logs/patterns` and `/api/v1/rules/backtest`, which save nothing. **There is no tool to acknowledge an
alert, create a maintenance window, or start, pause or cancel an upgrade of the collector:** a person does it in
ByLonis. A ByLonis older than the endpoint answers its path with the web page; the tools then say that the tenant
has to be upgraded for it.

- `bylonis_list_flapping_rules` (bylonis#594): `days` (1–90, default 7). Per rule: alerts, share of short ones
  (under 5 minutes), median duration, current holdDuration, how many alerts each hold would have avoided, and
  the suggestion of ByLonis. Rules of a fleet (`fleet: true`) are changed in Inventario → Alertas.
- `bylonis_list_alert_acknowledgements` (bylonis#595): `ruleId`, `days` (1–90, default 30). Same field names as
  `bylonis_list_alerts`: `acknowledgedBy`, `acknowledgedAt`, `acknowledgementNote`.
- `bylonis_list_maintenance_windows`: `ruleId` (the windows that cover it), `status`, `includeExpired`. The rules
  come by name; `allRules: true` when the window has none, so it covers every rule.
- `bylonis_list_change_events`: `service`, `environment`, `kinds` (`deploy`, `alert_firing`, `alert_resolved`,
  `fleet_config`), and `timeRange` (default `24h`) or `start`/`end`; at most 31 days and 500 events.
- `bylonis_get_log_patterns`: `filter` (expression of the logs explorer), `limit` (default 50, max 1000),
  `compareShift` (e.g. `24h`, at most `30d`), `exact`, and the range (default `1h`). Each pattern carries a
  `regex` to read its lines with `bylonis_search_logs`.
- `bylonis_backtest_alert` (bylonis#37): the fields of `bylonis_create_alert`, without channels, and `days`
  (1–14, default 7). Same validation as create. ByLonis asks for the editor role.
- `bylonis_list_collector_versions`, `bylonis_list_fleet_upgrades` (`fleet`, `limit`) and
  `bylonis_get_fleet_upgrade` (`upgradeId`, `hostname`) (bylonis#460, bylonis#602): the hosts of an upgrade
  come by name, without the ids of their jobs or their agents.

### Themes (bylonis#705)

The themes of the organization: the colors of the interface in light and dark mode. The MCP has no logic of its
own: each tool calls an endpoint of ByLonis under `/api/v1/themes`, whose deterministic engine generates the
themes and whose validator measures the WCAG 2.1 contrast. Generating, patching and validating save nothing;
only `bylonis_save_theme` saves. ByLonis lets only administrators generate, patch, validate and save (finer
permissions arrive with bylonis#644): with another key those tools answer 403 and say an administrator key is
needed. No tool deletes a theme or picks the theme of the organization. A natural-language `prompt` is not
available yet (bylonis#931): the tools reject it.

#### `bylonis_get_design_surface`

The modes, the 48 semantic tokens with their group (`surfaces`, `text`, `accent`, `status`, `charts`), the
personalities, the contrast pairs with their minimum, the color formats (`#rrggbb`, `#rrggbbaa`,
`oklch(L C H[/A])`) and the built-in themes (`bylonis`). `GET /api/v1/themes/design-surface`.

#### `bylonis_generate_theme`

`POST /api/v1/themes/generate`. Returns `{theme: {name, modes}, contrast_report: {ok, failures, failing}, warnings, saved: false}`;
the engine corrects the lightness until every pair passes, and `warnings` says what it adjusted.

- **Parameters** (at least one of the first three):
  - `base_color` (optional) - `#rrggbb`, `#rrggbbaa` or `oklch(L C H)`; it becomes the accent
  - `palette_from_image` (optional) - up to 8 colors extracted from a logo (only the hex values), the dominant
    first; the first is the accent when there is no `base_color`, the second the brand color
  - `personality` (optional) - `technical-dark`, `clean-light`, `corporate` or `high-contrast` (text at 7:1)
  - `name` (optional) - name of the theme; derived from the input if omitted
  - `prompt` - not available yet (bylonis#931): rejected

#### `bylonis_patch_theme_tokens`

`POST /api/v1/themes/patch`. Returns `{theme: {modes}, diff: [{path, from, to}], contrast_report, saved: false}`.
It does not correct the contrast.

- **Parameters**:
  - `theme_id` (optional) - a saved theme or `bylonis`; either this or `theme`
  - `theme` (optional) - `{"modes": {"light": {...}, "dark": {...}}}`, as `bylonis_generate_theme` returns it
  - `patch` (required) - `"mode.token"` → color, e.g. `{"light.accent": "#2563eb"}`
  - `prompt` - not available yet (bylonis#931): rejected

#### `bylonis_validate_theme`

`POST /api/v1/themes/validate`: `{valid, errors, contrast}`, the contrast pair by pair in both modes.

- **Parameters**: `theme` (required) - `{"name"?: "...", "modes": {"light": {...}, "dark": {...}}}`

#### `bylonis_list_themes` and `bylonis_get_theme`

`GET /api/v1/themes` (compact: id, name, source, built-in, contrast status; plus the theme of the organization
and the one that applies to the caller) and `GET /api/v1/themes/{id}` (colors and the failing pairs).

- **Parameters** of `bylonis_get_theme`: `theme_id` (required) - id or `bylonis`

#### `bylonis_save_theme`

Creates a theme (`POST /api/v1/themes`) or updates `theme_id` (`PUT /api/v1/themes/{id}`). Failing the contrast
does not prevent saving, but such a theme cannot be the theme of the organization.

- **Parameters**: `theme_id` (optional), `name` (required), `modes` (required) - `{"light": {...}, "dark": {...}}`

## Environment Variables

| Variable          | Description                                                                    | Required                            |
| ----------------- | ------------------------------------------------------------------------------ | ----------------------------------- |
| `BYLONIS_URL`      | ByLonis instance URL                                                           | Yes (stdio); Optional (http with OAuth) |
| `BYLONIS_API_KEY`  | ByLonis service account API key (Settings → Service Accounts) | Yes (stdio); Optional (http with OAuth) |
| `LOG_LEVEL`       | Logging level: `info`(default), `debug`, `warn`, `error`                       | No                                  |
| `TRANSPORT_MODE`  | MCP transport mode: `stdio`(default) or `http`                                 | No                                  |
| `MCP_SERVER_PORT` | Port for HTTP transport mode                                                   | Yes only when `TRANSPORT_MODE=http` |
| `MCP_MAX_REQUEST_BYTES` | Max inbound MCP HTTP request body size in bytes (default: `4194304` / 4 MiB). Bounds memory from a single oversized request. | No |
| `OAUTH_ENABLED`   | Enable OAuth 2.1 authentication flow (`true`/`false`)                          | No (default: `false`)               |
| `OAUTH_TOKEN_SECRET` | Encryption key for OAuth tokens (min 32 bytes, e.g. `openssl rand -base64 32`) | Yes when `OAUTH_ENABLED=true`    |
| `OAUTH_ISSUER_URL` | Public URL of this MCP server (used in OAuth metadata discovery)              | Yes when `OAUTH_ENABLED=true`       |
| `OAUTH_ACCESS_TOKEN_TTL_MINUTES` | Access token lifetime in minutes (default: 60)                  | No                                  |
| `OAUTH_REFRESH_TOKEN_TTL_MINUTES` | Refresh token lifetime in minutes (default: 1440 / 24h)       | No                                  |
| `OAUTH_AUTH_CODE_TTL_SECONDS` | Authorization code lifetime in seconds (default: 600 / 10min)      | No                                  |
| `BYLONIS_CUSTOM_HEADERS` | Extra HTTP headers added to every API request, useful when ByLonis is behind a reverse proxy requiring auth (e.g. `CF-Access-Client-Id:id.access,CF-Access-Client-Secret:secret`). Format: `Key1:Value1,Key2:Value2` | No |
| `BYLONIS_INSTANCE_URL_ALLOWLIST` | Multi-tenant (http) only: comma-separated allowlist of ByLonis hosts the server will proxy to. Entries are exact hosts (`houston.bylonis.ai`) or wildcards (`*.bylonis.ai`, which matches any subdomain); a scheme/port/path included by mistake is reduced to the bare host. When set, non-matching instance URLs are refused at every ingress: the OAuth form and `X-ByLonis-URL` header get HTTP 403, the OAuth token endpoint (including existing refresh tokens) returns `invalid_grant`, and `/mcp` requests with an OAuth token get 403. Each rejection increments a failure metric tagged `disallowed_bylonis_url`, and the message tells the user to use the MCP URL shown in Settings → MCP Server of their instance. Empty/unset allows any host. The operator's own `BYLONIS_URL` is exempt. | No |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP gRPC endpoint for the MCP server's own traces and metrics. Internal telemetry export is disabled when no OTLP endpoint/exporter is configured. For plaintext collectors, use an `http://` endpoint such as `http://localhost:4317`. | No |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | Trace-specific OTLP gRPC endpoint; overrides `OTEL_EXPORTER_OTLP_ENDPOINT` for traces. | No |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | Metrics-specific OTLP gRPC endpoint; overrides `OTEL_EXPORTER_OTLP_ENDPOINT` for metrics. | No |
| `OTEL_TRACES_EXPORTER` | Set to `none` to disable internal trace export even when an OTLP endpoint is configured. | No |
| `OTEL_METRICS_EXPORTER` | Set to `none` to disable internal metrics export and runtime metrics even when an OTLP endpoint is configured. | No |

Only the names above are read. The upstream `SIGNOZ_*` variables (and the `SIGNOZ-API-KEY` / `X-SigNoz-*` request headers) were accepted as aliases until ByLonis v1.0 and are now ignored. OAuth tokens carry the instance URL as `bylonis_url`, so tokens issued by earlier versions are rejected and the client has to authorize again.

The MCP server does not run an OTLP log exporter; logs are emitted as JSON to stderr. `OTEL_LOGS_EXPORTER` is therefore not used.

## Architecture

For a detailed overview of request flow, component interactions, and design decisions, see [docs/architecture.md](docs/architecture.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow and PR checklist.

## License

Apache License 2.0; see [LICENSE](LICENSE) and [NOTICE](NOTICE).
