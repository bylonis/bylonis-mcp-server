package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
)

// The fleet of ByLonis (bylonis#463): the inventory of Fleet Control (sites, nodes with or without an agent, their
// history), the health of the agents and the coverage. Read only; the API returns no secrets, and the tools drop
// even the id of the credential of a node.

const (
	fleetDefaultLimit = 50
	fleetMaxLimit     = 200
	// fleetRecentJobs is how many jobs of its site runner the card of a node shows.
	fleetRecentJobs = 10
)

func (h *Handler) RegisterFleetHandlers(s *server.MCPServer) {
	h.logger.Debug("Registering fleet handlers")

	searchContext := mcp.WithString("searchContext", mcp.Description("The user's original question or search text that triggered this tool call. Always include the user's raw query here for better results."))

	addTool(s, mcp.NewTool("bylonis_list_fleet_nodes",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		searchContext,
		mcp.WithDescription("Lists the nodes (machines) of the Fleet Control inventory: hosts, VMs, k8s nodes, storage and network devices, with or without an agent.\n\n"+
			"Returns per node: id, hostname, site, kind, status (up/down/unknown, from the agent or a ping/TCP probe), when it was last seen and when its status changed, IPs, how it is watched (livenessMethod), its OpAMP agent (agentId, linkState) and its effective labels.\n\n"+
			"FILTERING: 'search' matches hostname or display name; 'selector' is a label selector like 'site=lumina,class=escritorio' or 'kind in (host,vm)'; 'status', 'kind' and 'hasAgent' filter directly; 'site' takes a site name.\n"+
			"Use bylonis_get_fleet_node for the full card of one node, its agent and its recent jobs.\n\n"+
			"PAGINATION: 'limit' (default 50, max 200) and 'offset'; the response has 'total'."),
		mcp.WithString("search", mcp.Description("Text in the hostname or display name.")),
		mcp.WithString("selector", mcp.Description("Label selector, e.g. 'site=lumina,class=escritorio'.")),
		mcp.WithString("site", mcp.Description("Site name.")),
		mcp.WithString("status", mcp.Description("up, down or unknown.")),
		mcp.WithString("kind", mcp.Description("host, vm, k8s_node, storage, network_device or other.")),
		mcp.WithString("hasAgent", mcp.Description("'true' for nodes with an OpAMP agent, 'false' for those without.")),
		mcp.WithString("limit", mcp.Description("Maximum nodes to return. Default 50, max 200.")),
		mcp.WithString("offset", mcp.Description("Nodes to skip. Default 0.")),
	), h.handleListFleetNodes)

	addTool(s, mcp.NewTool("bylonis_get_fleet_node",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		searchContext,
		mcp.WithDescription("The full card of one node of the fleet, by hostname or id, in one call: use it for questions like 'why is the agent of compute-n07 disconnected?'.\n\n"+
			"'statusReason' says in words why the node has its status (what watches it and what that saw; 'unknown' is NOT 'down'): start the answer from it.\n\n"+
			"Returns: the node (status, last seen, IPs, site, labels by source, attributes, groups), its OpAMP agent (connected or not, since when, last heartbeat, versions, health reported by the supervisor), the health of the collector from its own metrics in the window (accepted/refused/sent/failed items, queue, CPU, memory, uptime, problems: dropping, queue_full, exporter_failing), and the last jobs of its site runner (access checks, remote installs, Ansible).\n\n"+
			"Use bylonis_get_fleet_node_history for what changed on the node."),
		mcp.WithString("hostname", mcp.Description("Hostname of the node (exact, case-insensitive). Either this or nodeId.")),
		mcp.WithString("nodeId", mcp.Description("Id of the node. Either this or hostname.")),
		mcp.WithString("window", mcp.Description("Window of the agent health, e.g. '15m', '1h', '24h'. Default '1h'.")),
	), h.handleGetFleetNode)

	addTool(s, mcp.NewTool("bylonis_get_fleet_node_history",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		searchContext,
		mcp.WithDescription("The history of one node of the fleet, newest first: what changed (fields, labels, groups, site, fleet, credential name), agents linked or unlinked, and the Ansible runs on it, with who did it and from where (ui, api, agent, import, discovery, system). Use it for 'what changed on this host before the incident?'.\n\n"+
			"'byOperation' counts the changes by operation: create, update, delete, link/unlink (agents) and ansible (playbook runs, whose newValue is the recap). Mention the Ansible runs and agent links explicitly when they are there.\n\n"+
			"Defaults to the last 7 days; 'timeRange' ('24h', '7d') or 'start'/'end' (RFC3339 or milliseconds) narrow it."),
		mcp.WithString("hostname", mcp.Description("Hostname of the node. Either this or nodeId.")),
		mcp.WithString("nodeId", mcp.Description("Id of the node. Either this or hostname.")),
		mcp.WithString("timeRange", mcp.Description("How far back: '<n>m', '<n>h' or '<n>d'. Default '7d'. Ignored with start.")),
		mcp.WithString("start", mcp.Description("Start, RFC3339 or milliseconds since epoch.")),
		mcp.WithString("end", mcp.Description("End, RFC3339 or milliseconds since epoch. Default now.")),
		mcp.WithString("limit", mcp.Description("Maximum changes. Default 50, max 200.")),
	), h.handleGetFleetNodeHistory)

	addTool(s, mcp.NewTool("bylonis_get_fleet_coverage",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		searchContext,
		mcp.WithDescription("Compares the inventory with the telemetry in a window: machines that send metrics, logs or traces but are not in the inventory (untracked), nodes that should have an agent and do not report (silent), nodes alive without an agent (gaps, candidates to install it), and per site the share of nodes with a healthy agent."),
		mcp.WithString("site", mcp.Description("Site name; all sites if omitted.")),
		mcp.WithString("window", mcp.Description("'1h', '24h' or '168h'. Default '24h'.")),
	), h.handleGetFleetCoverage)

	addTool(s, mcp.NewTool("bylonis_list_fleet_agent_health",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		searchContext,
		mcp.WithDescription("The health of the collectors of the fleet from their own metrics (otelcol_*) in a window, by node: accepted, refused, sent and failed items, queue usage, CPU, memory, and the problems dropping, queue_full and exporter_failing. Only nodes whose agents send their own metrics appear.\n\n"+
			"'problemsOnly' keeps the nodes with a problem."),
		mcp.WithString("site", mcp.Description("Site name; all sites if omitted.")),
		mcp.WithString("window", mcp.Description("'15m', '1h', '24h'… Default '15m'.")),
		mcp.WithString("problemsOnly", mcp.Description("'true' to keep only the nodes with a problem.")),
	), h.handleListFleetAgentHealth)
}

// fleetArgs reads the string arguments of a tool call.
type fleetArgs map[string]any

func (args fleetArgs) text(name string) string {
	value, _ := args[name].(string)
	return strings.TrimSpace(value)
}

func (args fleetArgs) number(name string, fallback, max int) (int, error) {
	text := args.text(name)
	if text == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil || value < 0 {
		return 0, fmt.Errorf(`Parameter validation failed: %q must be a non-negative number, e.g. "%d"`, name, fallback)
	}
	if max > 0 && value > max {
		value = max
	}
	return value, nil
}

// fleetData reads the data of an answer of the API of ByLonis.
func fleetData[T any](raw json.RawMessage) (T, error) {
	var envelope struct {
		Data T `json:"data"`
	}
	err := json.Unmarshal(raw, &envelope)
	return envelope.Data, err
}

func fleetResult(value any) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(raw)), nil
}

// siteIDs maps the names of the sites, in lower case, to their ids, and the ids to their names.
func (h *Handler) fleetSites(ctx context.Context, client bylonisclient.Client) (map[string]string, map[string]string, error) {
	raw, err := client.GetFleet(ctx, "/api/v1/inventory/sites", nil)
	if err != nil {
		return nil, nil, err
	}
	sites, err := fleetData[[]struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}](raw)
	if err != nil {
		return nil, nil, err
	}
	byName, byID := map[string]string{}, map[string]string{}
	for _, site := range sites {
		byName[strings.ToLower(site.Name)] = site.ID
		byID[site.ID] = site.Name
	}
	return byName, byID, nil
}

func (h *Handler) siteParam(ctx context.Context, client bylonisclient.Client, args fleetArgs, query url.Values) error {
	name := args.text("site")
	if name == "" {
		return nil
	}
	byName, _, err := h.fleetSites(ctx, client)
	if err != nil {
		return err
	}
	id, ok := byName[strings.ToLower(name)]
	if !ok {
		names := make([]string, 0, len(byName))
		for n := range byName {
			names = append(names, n)
		}
		return fmt.Errorf("there is no site %q; the sites are: %s", name, strings.Join(names, ", "))
	}
	query.Set("siteId", id)
	return nil
}

// fleetNode is a node of the inventory as the tools give it: without the id of its credential.
type fleetNode map[string]any

func compactNode(node fleetNode, siteNames map[string]string) fleetNode {
	out := fleetNode{}
	for _, key := range []string{"id", "hostname", "displayName", "kind", "status", "statusChangedAt", "lastSeenAt",
		"primaryIp", "publicIp", "osType", "livenessMethod", "agentId", "linkState", "labels"} {
		if value, ok := node[key]; ok && value != nil && value != "" {
			out[key] = value
		}
	}
	if siteID, ok := node["siteId"].(string); ok {
		out["site"] = siteNames[siteID]
	}
	return out
}

func (h *Handler) handleListFleetNodes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	limit, err := args.number("limit", fleetDefaultLimit, fleetMaxLimit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	offset, err := args.number("offset", 0, 0)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	for param, arg := range map[string]string{"q": "search", "selector": "selector", "status": "status", "kind": "kind", "hasAgent": "hasAgent"} {
		if value := args.text(arg); value != "" {
			query.Set(param, value)
		}
	}
	if err := h.siteParam(ctx, client, args, query); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	_, siteNames, err := h.fleetSites(ctx, client)
	if err != nil {
		return h.fleetError(ctx, "bylonis_list_fleet_nodes", err)
	}
	raw, err := client.GetFleet(ctx, "/api/v1/inventory/nodes", query)
	if err != nil {
		return h.fleetError(ctx, "bylonis_list_fleet_nodes", err)
	}
	page, err := fleetData[struct {
		Nodes []fleetNode `json:"nodes"`
		Total int         `json:"total"`
	}](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	nodes := make([]fleetNode, 0, len(page.Nodes))
	for _, node := range page.Nodes {
		nodes = append(nodes, compactNode(node, siteNames))
	}
	return fleetResult(map[string]any{
		"nodes": nodes, "total": page.Total,
		"pagination": map[string]any{"limit": limit, "offset": offset, "hasMore": offset+len(nodes) < page.Total, "nextOffset": offset + len(nodes)},
	})
}

// fleetNodeID resolves the node of a call: its id, or the node with that exact hostname.
func (h *Handler) fleetNodeID(ctx context.Context, client bylonisclient.Client, args fleetArgs) (string, error) {
	if id := args.text("nodeId"); id != "" {
		return id, nil
	}
	hostname := args.text("hostname")
	if hostname == "" {
		return "", fmt.Errorf(`Parameter validation failed: give "hostname" or "nodeId". Example: {"hostname": "compute-n07"}`)
	}
	raw, err := client.GetFleet(ctx, "/api/v1/inventory/nodes", url.Values{"q": {hostname}, "limit": {"100"}})
	if err != nil {
		return "", err
	}
	page, err := fleetData[struct {
		Nodes []fleetNode `json:"nodes"`
	}](raw)
	if err != nil {
		return "", err
	}
	similar := []string{}
	for _, node := range page.Nodes {
		name, _ := node["hostname"].(string)
		if strings.EqualFold(name, hostname) {
			id, _ := node["id"].(string)
			return id, nil
		}
		similar = append(similar, name)
	}
	if len(similar) > 0 {
		return "", fmt.Errorf("no node is called %q; similar: %s", hostname, strings.Join(similar, ", "))
	}
	return "", fmt.Errorf("no node is called %q; use bylonis_list_fleet_nodes to search", hostname)
}

func (h *Handler) handleGetFleetNode(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := h.fleetNodeID(ctx, client, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	path := "/api/v1/inventory/nodes/" + url.PathEscape(id)
	raw, err := client.GetFleet(ctx, path, nil)
	if err != nil {
		return h.fleetError(ctx, "bylonis_get_fleet_node", err)
	}
	node, err := fleetData[fleetNode](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	_, siteNames, err := h.fleetSites(ctx, client)
	if err != nil {
		return h.fleetError(ctx, "bylonis_get_fleet_node", err)
	}
	card := compactNode(node, siteNames)
	for _, key := range []string{"attributes", "source", "parentNodeId", "fleetId", "createdAt", "updatedAt"} {
		if value, ok := node[key]; ok && value != nil {
			card[key] = value
		}
	}
	out := map[string]any{"node": card}
	// What fails in the parts below is reported with them: the rest of the card still helps.
	partial := func(key string, err error) { out[key] = map[string]any{"error": err.Error()} }

	if raw, err := client.GetFleet(ctx, path+"/labels", nil); err == nil {
		if labels, err := fleetData[map[string]any](raw); err == nil {
			out["labelsBySource"] = labels
		}
	}
	if raw, err := client.GetFleet(ctx, path+"/groups", nil); err == nil {
		if groups, err := fleetData[[]map[string]any](raw); err == nil {
			names := make([]string, 0, len(groups))
			for _, group := range groups {
				if name, ok := group["name"].(string); ok {
					names = append(names, name)
				}
			}
			out["groups"] = names
		}
	}
	if agentID, _ := node["agentId"].(string); agentID != "" {
		if raw, err := client.GetFleet(ctx, "/api/v1/fleet/agents", url.Values{"q": {agentID}, "limit": {"10"}}); err != nil {
			partial("agent", err)
		} else if page, err := fleetData[struct {
			Agents []map[string]any `json:"agents"`
		}](raw); err == nil {
			for _, agent := range page.Agents {
				if agent["instanceUid"] == agentID {
					delete(agent, "tokenId")
					delete(agent, "effectiveConfig")
					out["agent"] = agent
				}
			}
		}
	} else {
		out["agent"] = "the node has no OpAMP agent linked"
	}
	window := args.text("window")
	if window == "" {
		window = "1h"
	}
	if raw, err := client.GetFleet(ctx, path+"/agent_health", url.Values{"window": {window}}); err != nil {
		partial("agentHealth", err)
	} else if health, err := fleetData[map[string]any](raw); err == nil {
		delete(health, "effectiveConfig")
		if agents, ok := health["agents"].([]any); ok {
			for _, agent := range agents {
				if agent, ok := agent.(map[string]any); ok {
					delete(agent, "series")
				}
			}
		}
		out["agentHealth"] = health
	}
	siteName, _ := card["site"].(string)
	out["statusReason"] = h.fleetStatusReason(ctx, client, node, out["agent"], siteName)
	if raw, err := client.GetFleet(ctx, "/api/v1/fleet/jobs", url.Values{"node_id": {id}, "limit": {strconv.Itoa(fleetRecentJobs)}}); err == nil {
		if page, err := fleetData[struct {
			Jobs []map[string]any `json:"jobs"`
		}](raw); err == nil {
			jobs := make([]map[string]any, 0, len(page.Jobs))
			for _, job := range page.Jobs {
				jobs = append(jobs, map[string]any{"kind": job["kind"], "status": job["status"], "createdAt": job["createdAt"],
					"createdBy": job["createdBy"], "finishedAt": job["finishedAt"], "error": job["error"]})
			}
			out["recentJobs"] = jobs
		}
	}
	return fleetResult(out)
}

// fleetStatusReason says in words why a node has its status: what watches it and what that saw. The status alone
// misleads: "unknown" is not "down", it means that nothing has checked the node (bylonis#463).
func (h *Handler) fleetStatusReason(ctx context.Context, client bylonisclient.Client, node fleetNode, agent any, site string) string {
	status, _ := node["status"].(string)
	method, _ := node["livenessMethod"].(string)
	lastSeen, _ := node["lastSeenAt"].(string)
	since, _ := node["statusChangedAt"].(string)
	address, _ := node["primaryIp"].(string)
	switch method {
	case "none":
		return "Nothing watches this node (livenessMethod none), so its status is always unknown; it is not down."
	case "agent":
		agentID, _ := node["agentId"].(string)
		connected := false
		if agent, ok := agent.(map[string]any); ok {
			connected = agent["status"] == "connected"
		}
		switch {
		case agentID == "":
			return "It should be watched by its own agent (livenessMethod agent), but no agent is linked to it: its status is unknown, not down. Install the agent on it (remote install from its card, or the installer)."
		case status == "up":
			return "Up: its agent reported at " + lastSeen + "."
		case !connected:
			return "Its OpAMP agent is disconnected and its last data is from " + orUnknown(lastSeen) + " (status " + status + " since " + orUnknown(since) + "): the agent, its supervisor or the machine stopped, or it cannot reach ByLonis."
		default:
			return "Its agent is connected but its host data stopped at " + orUnknown(lastSeen) + " (status " + status + " since " + orUnknown(since) + "): look at the health of the collector and its exporter."
		}
	case "icmp", "tcp":
		check := "ping"
		if method == "tcp" {
			check = "a TCP connection"
		}
		probes := h.fleetProbes(ctx, client, site)
		switch {
		case status == "up":
			return "Up: the probe of its site reached " + address + " with " + check + " at " + lastSeen + "."
		case status == "down":
			return "Down since " + orUnknown(since) + ": the probe of site " + site + " does not reach " + address + " with " + check + "."
		case probes == 0:
			return "Its status is unknown, not down: it is watched with " + check + " to " + address + ", which a probe agent (role sonda) of site " + site + " must do, and no probe of the site is connected. Nobody has checked it."
		default:
			return "Its status is unknown, not down: the probe of site " + site + " has not reported " + address + " yet."
		}
	}
	return "Status " + status + "."
}

// fleetProbes counts the connected probe agents (role sonda) of a site.
func (h *Handler) fleetProbes(ctx context.Context, client bylonisclient.Client, site string) int {
	raw, err := client.GetFleet(ctx, "/api/v1/fleet/agents", url.Values{"role": {"sonda"}, "status": {"connected"}, "limit": {"100"}})
	if err != nil {
		return 0
	}
	page, err := fleetData[struct {
		Agents []map[string]any `json:"agents"`
	}](raw)
	if err != nil {
		return 0
	}
	count := 0
	for _, agent := range page.Agents {
		if agent["site"] == site && agent["status"] == "connected" {
			count++
		}
	}
	return count
}

func orUnknown(text string) string {
	if text == "" {
		return "never"
	}
	return text
}

// fleetTime reads a time as RFC3339 or milliseconds since epoch.
func fleetTime(text string) (time.Time, error) {
	if ms, err := strconv.ParseInt(text, 10, 64); err == nil {
		return time.UnixMilli(ms).UTC(), nil
	}
	return time.Parse(time.RFC3339, text)
}

func fleetDuration(text string) (time.Duration, error) {
	if strings.HasSuffix(text, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(text, "d"))
		return time.Duration(days) * 24 * time.Hour, err
	}
	return time.ParseDuration(text)
}

func (h *Handler) handleGetFleetNodeHistory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	limit, err := args.number("limit", fleetDefaultLimit, fleetMaxLimit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	end := time.Now().UTC()
	if text := args.text("end"); text != "" {
		if end, err = fleetTime(text); err != nil {
			return mcp.NewToolResultError(`Invalid "end": use RFC3339 (2026-10-29T10:00:00Z) or milliseconds since epoch`), nil
		}
	}
	var start time.Time
	if text := args.text("start"); text != "" {
		if start, err = fleetTime(text); err != nil {
			return mcp.NewToolResultError(`Invalid "start": use RFC3339 (2026-10-29T10:00:00Z) or milliseconds since epoch`), nil
		}
	} else {
		timeRange := args.text("timeRange")
		if timeRange == "" {
			timeRange = "7d"
		}
		span, err := fleetDuration(timeRange)
		if err != nil || span <= 0 {
			return mcp.NewToolResultError(`Invalid "timeRange": use '<n>m', '<n>h' or '<n>d', e.g. '24h' or '7d'`), nil
		}
		start = end.Add(-span)
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := h.fleetNodeID(ctx, client, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetFleet(ctx, "/api/v1/inventory/nodes/"+url.PathEscape(id)+"/changes", url.Values{
		"from": {start.Format(time.RFC3339)}, "to": {end.Format(time.RFC3339)}, "limit": {strconv.Itoa(limit)},
	})
	if err != nil {
		return h.fleetError(ctx, "bylonis_get_fleet_node_history", err)
	}
	page, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	// The ids of the rows, the organization and the node say nothing to the reader: the hostname is there. The
	// count by operation keeps a run of Ansible or an agent linked from hiding among field edits.
	byOperation := map[string]int{}
	if changes, ok := page["changes"].([]any); ok {
		for _, change := range changes {
			if change, ok := change.(map[string]any); ok {
				delete(change, "id")
				delete(change, "orgId")
				delete(change, "nodeId")
				if operation, ok := change["operation"].(string); ok {
					byOperation[operation]++
				}
			}
		}
	}
	page["byOperation"] = byOperation
	page["from"], page["to"] = start.Format(time.RFC3339), end.Format(time.RFC3339)
	return fleetResult(page)
}

func (h *Handler) handleGetFleetCoverage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query := url.Values{}
	if window := args.text("window"); window != "" {
		query.Set("window", window)
	}
	if err := h.siteParam(ctx, client, args, query); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetFleet(ctx, "/api/v1/inventory/coverage", query)
	if err != nil {
		return h.fleetError(ctx, "bylonis_get_fleet_coverage", err)
	}
	coverage, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	_, siteNames, _ := h.fleetSites(ctx, client)
	for _, key := range []string{"silent", "gaps"} {
		if nodes, ok := coverage[key].([]any); ok {
			compact := make([]fleetNode, 0, len(nodes))
			for _, node := range nodes {
				if node, ok := node.(map[string]any); ok {
					compact = append(compact, compactNode(node, siteNames))
				}
			}
			coverage[key] = compact
		}
	}
	return fleetResult(coverage)
}

func (h *Handler) handleListFleetAgentHealth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query := url.Values{}
	if window := args.text("window"); window != "" {
		query.Set("window", window)
	}
	if err := h.siteParam(ctx, client, args, query); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetFleet(ctx, "/api/v1/inventory/agent_health", query)
	if err != nil {
		return h.fleetError(ctx, "bylonis_list_fleet_agent_health", err)
	}
	health, err := fleetData[struct {
		Window string           `json:"window"`
		Nodes  []map[string]any `json:"nodes"`
	}](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if args.text("problemsOnly") == "true" {
		kept := health.Nodes[:0]
		for _, node := range health.Nodes {
			if problems, ok := node["problems"].([]any); ok && len(problems) > 0 {
				kept = append(kept, node)
			}
		}
		health.Nodes = kept
	}
	return fleetResult(map[string]any{"window": health.Window, "nodes": health.Nodes, "total": len(health.Nodes)})
}

func (h *Handler) fleetError(ctx context.Context, tool string, err error) (*mcp.CallToolResult, error) {
	h.logger.ErrorContext(ctx, "Fleet tool failed", slog.String("tool", tool), logpkg.ErrAttr(err))
	message := err.Error()
	if strings.Contains(message, "404") {
		message += " (is Fleet Control enabled in this tenant? It answers 404 while the flag enable_fleet_control is off)"
	}
	return mcp.NewToolResultError(message), nil
}

func fleetArgsOf(req mcp.CallToolRequest) fleetArgs {
	args, _ := req.Params.Arguments.(map[string]any)
	return args
}
