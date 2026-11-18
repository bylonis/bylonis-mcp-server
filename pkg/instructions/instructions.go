package instructions

// ServerInstructions is sent to MCP clients during the initialize response.
// It contains cross-cutting rules that apply to all tool usage.
const ServerInstructions = `# ByLonis MCP Server — Instructions

## Rules

1. **Avoid redundant queries.** Only fetch additional data if the first result is insufficient or if the user explicitly asks for more. Do not make multiple overlapping calls when one would suffice.

2. **Always prefer resource attributes in filters.** Resource attributes (e.g., service.name, k8s.namespace.name, host.name) significantly speed up backend queries. If the user does not provide a resource attribute filter:
   a. Call bylonis_get_field_keys with fieldContext=resource to discover available resource attributes.
   b. Optionally call bylonis_get_field_values to suggest concrete values for those attributes.
   c. Ask the user to pick a resource attribute filter before running the query.
   If the user already provides resource attributes, proceed directly without extra prompting.

3. **Clarify the signal before querying.** If it is not clear which signal to use, ask the user whether to start with metrics, traces, or logs.

4. **Choose the right filter operator for the intent and data type.**

   **By query intent:**
   | Intent | Operator | Example |
   |---|---|---|
   | Field exists / is present | EXISTS | trace_id EXISTS |
   | Field is absent | NOT EXISTS | k8s.pod.name NOT EXISTS |
   | Exact match | = | service.name = 'frontend' |
   | Exclude (field must exist) | EXISTS AND != | service.name EXISTS AND service.name != 'redis' |
   | One of several values | IN | severity_text IN ('ERROR', 'WARN', 'FATAL') |
   | Substring / pattern | LIKE | name LIKE '%payment%' |
   | Case-insensitive pattern | ILIKE | body ILIKE '%timeout%' |
   | Simple text containment | CONTAINS | body CONTAINS 'timeout' |
   | Regex | REGEXP | name REGEXP '^grpc\.' |

   **By data type:**
   | Data type | Safe operators |
   |---|---|
   | bool | = , != , EXISTS, NOT EXISTS |
   | int64 | = , != , > , >= , < , <= , IN, EXISTS, NOT EXISTS |
   | string | = , != , LIKE, ILIKE, CONTAINS, REGEXP, IN, NOT IN, EXISTS, NOT EXISTS |

   **Important:** Negative operators (!=, NOT LIKE, NOT IN, NOT CONTAINS, NOT REGEXP) also match records where the field is **absent**. To exclude a value while requiring the field, combine with EXISTS:
   ` + "`" + `service.name EXISTS AND service.name != 'redis'` + "`" + `

5. **Never convert Unix timestamps manually.** All ByLonis timestamps (start, end, and time series values) are Unix milliseconds. When presenting timestamps to the user, always use a programmatic method (e.g., a date conversion tool or function) to convert them to human-readable format. Do NOT attempt mental arithmetic or manual offset calculations — this is error-prone and leads to incorrect times being reported.

6. **Do not generate ByLonis frontend links.** Never guess or build a clickable ByLonis URL from IDs, names, or the base domain. Only return a link if a tool or resource explicitly provides the full frontend URL; otherwise say MCP cannot generate a reliable link.

7. **Questions about a machine or the fleet go to the fleet tools first.** For "why is the agent of X disconnected?", "is X up?" or "what is X?", call bylonis_get_fleet_node with the hostname: one call gives its status, its OpAMP agent, the health of its collector and the last jobs of its site runner. For "what changed on X before the incident?", call bylonis_get_fleet_node_history with a range that covers the incident. Use bylonis_get_fleet_coverage for machines missing from the inventory or agents that do not report, and only then query metrics or logs of that host. When the agent fails, does not connect or sends nothing, read its newest diagnostic with bylonis_get_fleet_diagnostic (logs of the supervisor and the collector, configurations, connectivity) and quote the lines that explain it; if it has none, or none with the host part, ask the user to request one in the card of the node (tab Diagnóstico). For the version of the collector, whether it is vulnerable or how an upgrade goes, use bylonis_list_collector_versions, bylonis_list_fleet_upgrades and bylonis_get_fleet_upgrade.

8. **A proposal is not a change.** To propose a configuration of the collector: read the current one (bylonis_get_fleet_config), validate yours (bylonis_validate_fleet_config) until it is valid, then save it with bylonis_propose_fleet_config and a note that says what changes and why. To propose a playbook: save it with bylonis_propose_playbook, check it with bylonis_check_playbook on a test node or a small selector, and read the result with bylonis_get_playbook_run. Never say a proposal is deployed or run: tell the user the version and that a person deploys it or approves the run in ByLonis. There is no tool to deploy or to approve, on purpose, nor to acknowledge an alert, create a maintenance window or start, pause or cancel an upgrade of the collector: those are for a person in ByLonis.

9. **Themes come from the engine of ByLonis, not from you.** Never write the colors of a theme yourself: call bylonis_get_design_surface to learn the tokens, generate with bylonis_generate_theme from a base color, the colors of a logo or a personality, adjust single tokens with bylonis_patch_theme_tokens, and check contrast_report. Generating and patching save nothing: save with bylonis_save_theme only when the user asks. A natural-language description (prompt) is not supported yet (bylonis#931): translate the request into base_color, palette_from_image and personality.

10. **In an incident, start from what ByLonis already summarises.** For "what changed before it?" of a service, call bylonis_list_change_events with a range that covers the incident (deploys, alerts, deployments of a fleet), before querying metrics. For errors in the logs, call bylonis_get_log_patterns first (with compareShift '24h' to see what is new) and only then bylonis_search_logs with the regex of the pattern that matters. For "why did this rule not notify?", look at bylonis_list_maintenance_windows. If a tool says that this ByLonis does not have it yet, tell the user; do not rebuild it from other tools.

11. **Noisy alerts are tuned with the data of ByLonis.** For "too many alerts" or "which rules make noise?", call bylonis_list_flapping_rules and propose the holdDuration it suggests. Before proposing a new rule or a change, replay it with bylonis_backtest_alert and say how often it would have fired. Change a rule only when the user asks, and never a rule of a fleet (label fleet): that one is changed in Inventario → Alertas. For "is someone on this alert?", the acknowledgement is in bylonis_list_alerts, and the past ones in bylonis_list_alert_acknowledgements.
`
