# ByLonis MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server that gives AI assistants access to the observability data of a ByLonis instance: metrics, traces, logs, alerts, dashboards, saved views, notification channels and services, queried in natural language.

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
| `bylonis_get_alert_history` | Get alert state history timeline for a rule |
| `bylonis_create_alert` | Create an alert rule via POST /api/v2/rules; v2alpha1 for threshold/promql, v1 for anomaly |
| `bylonis_update_alert` | Update an alert rule by UUIDv7 via PUT /api/v2/rules/{ruleId} |
| `bylonis_delete_alert` | Delete an alert rule by UUIDv7 via DELETE /api/v2/rules/{ruleId} |
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

#### `bylonis_list_alert_rules`

Lists configured alert rules from `GET /api/v2/rules`, including inactive/OK and disabled rules. Returns compact summaries with `ruleId`, `alert`, `alertType`, `ruleType`, `state`, `disabled`, `severity`, `labels`, `createdAt`, and `updatedAt`.

- **Parameters**:
  - `limit` (optional) - Maximum number of rules to return (default: 50)
  - `offset` (optional) - Number of rules to skip for pagination (default: 0)

#### `bylonis_get_alert`

Gets the rule definition for an alert (`GET /api/v2/rules/{ruleId}`).

- **Parameters**: `ruleId` (required) - Alert rule ID (UUIDv7 on v2-capable servers).
- **Note**: Response shape depends on the ByLonis server version. Post-#10997 servers return the canonical `Rule` type with `createdAt/updatedAt/createdBy/updatedBy`; older servers return `GettableRule` with `createAt/updateAt/createBy/updateBy` (no 'd').

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

Gets alert history timeline for a specific rule.

- **Parameters**:
  - `ruleId` (required) - Alert rule ID
  - `timeRange` (optional) - Time range like '2h', '6h', '2d', '7d' (ignored when both `start` and `end` are provided)
  - `start` (optional) - Start timestamp in milliseconds (defaults to 6 hours ago)
  - `end` (optional) - End timestamp in milliseconds (defaults to now)
  - `offset` (optional) - Offset for pagination (default: 0)
  - `limit` (optional) - Limit number of results (default: 20)
  - `order` (optional) - Sort order: 'asc' or 'desc' (default: 'asc')

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
  - `anomaly_rule` → **v1** schema: top-level `evalWindow` and `frequency`; `condition.op`/`matchType`/`target`/`algorithm`/`seasonality`; anomaly function inside `compositeQuery.queries[].spec.functions`. Omit `thresholds`, `evaluation`, `schemaVersion`.
- **Tip**: Read MCP resources `bylonis://alert/instructions` and `bylonis://alert/examples` (ten canonical payloads plus a Cost Meter cumulative-budget example) before composing payloads. For `promql_rule`, also read `bylonis://promql/instructions` — OTel dotted metric names require the Prometheus 3.x UTF-8 quoted-selector form.

#### `bylonis_update_alert`

Update an existing alert rule via `PUT /api/v2/rules/{ruleId}`. Replaces the full rule configuration — fetch the current rule with `bylonis_get_alert` first and merge changes on top of it.

- **Parameters**:
  - `ruleId` (required) - UUIDv7 of the rule to update (obtain from `bylonis_list_alert_rules` / `bylonis_get_alert`).
  - Plus all fields of the alert rule schema (same shape as `bylonis_create_alert`).

#### `bylonis_delete_alert`

Delete an alert rule via `DELETE /api/v2/rules/{ruleId}`. Irreversible — confirm with the user first.

- **Parameters**:
  - `ruleId` (required) - UUIDv7 of the rule to delete. The server rejects non-UUIDv7 values with `invalid_input`.

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

#### `bylonis_update_notification_channel`

Update an existing notification channel and send a test notification.

- **Parameters**:
  - `id` (required) - Notification channel UUID
  - `type` (required) - Channel type
  - `name` (required) - Channel name
  - Full channel configuration fields for the selected channel type

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
