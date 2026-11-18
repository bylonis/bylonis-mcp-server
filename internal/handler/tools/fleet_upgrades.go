package tools

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// The versions and the upgrades of the collector of the fleets (bylonis#460, bylonis#602), read only (bylonis#1024):
// starting, pausing, resuming or cancelling an upgrade is for a person in ByLonis.

const (
	fleetUpgradesDefaultLimit = 20
	fleetUpgradesMaxLimit     = 100
)

func (h *Handler) registerFleetUpgradeHandlers(s *server.MCPServer, readOnly func(name, description string, options ...mcp.ToolOption) mcp.Tool) {
	addTool(s, readOnly("bylonis_list_collector_versions",
		"Lists the versions of the collector (otelcol-contrib) the fleets can run, newest first: per version its status (current, previous, obsolete or vulnerable), supervisorVersion, releasedAt, notes and notesUrl, and its security advisories. "+
			"Also gives per fleet the version it runs (collectorVersion), its policy (versionPolicy: latest, or pinned to pinnedVersion), the version it should run (targetVersion) and versionStatus.\n\n"+
			"Use it for 'which version does the collector run?', 'is it vulnerable?' or 'is there a newer one?'. Upgrading is for a person in ByLonis (Fleet → Versiones); bylonis_list_fleet_upgrades tells how an upgrade goes."),
		h.handleListCollectorVersions)

	addTool(s, readOnly("bylonis_list_fleet_upgrades",
		"Lists the upgrades of the collector of the fleets, newest first: fleet, version, status (running, scheduled: waiting for its upgrade window, paused by someone, halted because a host failed, completed, cancelled), "+
			"reason, batch of batches, progress, nextWindowAt and who started it.\n\n"+
			"With 'fleet', it also gives the upgrade windows its hosts wait for: that of the fleet, those of the groups of its hosts, and neverOpen, the hosts whose windows never overlap (an upgrade waits forever for them). "+
			"bylonis_get_fleet_upgrade gives the hosts of an upgrade and its history.",
		mcp.WithString("fleet", mcp.Description("Name or id of a fleet; all the fleets if omitted.")),
		mcp.WithString("limit", mcp.Description("Maximum upgrades. Default 20, max 100."))),
		h.handleListFleetUpgrades)

	addTool(s, readOnly("bylonis_get_fleet_upgrade",
		"One upgrade of the collector with its hosts, in their batches, and its history, oldest first. Use it for 'why did the upgrade stop?' or 'what is it waiting for?'.\n\n"+
			"Per host: name, batch, state, previousVersion, sentAt, appliedAt, observeUntil, unhealthySince and error. "+
			"The history says when each batch started, when it waited for the upgrade window or a deployment, and what happened to each host: sent (job queued), observing, promoted, failed, rolled_back (back to its version), skipped, held (its node is in a maintenance window). "+
			"A host that fails goes back to its version and halts the upgrade: quote its error.",
		mcp.WithString("upgradeId", mcp.Required(), mcp.Description("Id of the upgrade, from bylonis_list_fleet_upgrades.")),
		mcp.WithString("hostname", mcp.Description("Keep the history of this host."))),
		h.handleGetFleetUpgrade)
}

func (h *Handler) handleListCollectorVersions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetFleet(ctx, "/api/v1/fleet/collector_versions", nil)
	if err != nil {
		return h.fleetError(ctx, "bylonis_list_collector_versions", err)
	}
	versions, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if releases, ok := versions["releases"].([]any); ok {
		for _, release := range releases {
			if release, ok := release.(map[string]any); ok {
				delete(release, "imageDigest")
			}
		}
	}
	out := map[string]any{"releases": versions["releases"], "advisories": versions["advisories"]}
	if raw, err := client.GetFleet(ctx, "/api/v1/fleet/fleets", nil); err != nil {
		out["fleets"] = map[string]any{"error": err.Error()}
	} else if fleets, err := fleetData[[]map[string]any](raw); err == nil {
		rows := make([]map[string]any, 0, len(fleets))
		for _, fleet := range fleets {
			row := map[string]any{}
			for _, key := range []string{"name", "kind", "collectorVersion", "versionPolicy", "pinnedVersion", "targetVersion", "versionStatus"} {
				if value, ok := fleet[key]; ok && value != nil && value != "" {
					row[key] = value
				}
			}
			rows = append(rows, row)
		}
		out["fleets"] = rows
	}
	return fleetResult(out)
}

func (h *Handler) handleListFleetUpgrades(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	limit, err := args.number("limit", fleetUpgradesDefaultLimit, fleetUpgradesMaxLimit)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	var fleetID string
	if name := args.text("fleet"); name != "" {
		fleet, err := h.fleetByName(ctx, client, name)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		fleetID, _ = fleet["id"].(string)
		query.Set("fleetId", fleetID)
	}
	raw, err := client.GetFleet(ctx, "/api/v1/fleet/upgrades", query)
	if err != nil {
		return h.fleetError(ctx, "bylonis_list_fleet_upgrades", err)
	}
	upgrades, err := fleetData[[]map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	for _, upgrade := range upgrades {
		delete(upgrade, "orgId")
	}
	if upgrades == nil {
		upgrades = []map[string]any{}
	}
	out := map[string]any{"upgrades": upgrades, "total": len(upgrades)}
	if fleetID != "" {
		if raw, err := client.GetFleet(ctx, "/api/v1/fleet/fleets/"+url.PathEscape(fleetID)+"/upgrade_windows", nil); err != nil {
			out["upgradeWindows"] = map[string]any{"error": err.Error()}
		} else if windows, err := fleetData[map[string]any](raw); err == nil {
			delete(windows, "fleetId")
			out["upgradeWindows"] = windows
		}
	}
	return fleetResult(out)
}

func (h *Handler) handleGetFleetUpgrade(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if err := required(args, "upgradeId"); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	path := "/api/v1/fleet/upgrades/" + url.PathEscape(args.text("upgradeId"))
	raw, err := client.GetFleet(ctx, path, nil)
	if err != nil {
		return h.fleetError(ctx, "bylonis_get_fleet_upgrade", err)
	}
	upgrade, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	delete(upgrade, "orgId")
	// The hosts by the id of their row, for the history; the ids of their jobs say nothing to the reader.
	hosts := map[string]string{}
	hostname := args.text("hostname")
	targetID := ""
	if targets, ok := upgrade["targets"].([]any); ok {
		for _, target := range targets {
			if target, ok := target.(map[string]any); ok {
				id, _ := target["id"].(string)
				name, _ := target["name"].(string)
				hosts[id] = name
				if hostname != "" && strings.EqualFold(name, hostname) {
					targetID = id
				}
				for _, key := range []string{"orgId", "upgradeId", "instanceUid", "jobId", "revertJobId", "nodeId", "id"} {
					delete(target, key)
				}
			}
		}
	}
	if hostname != "" && targetID == "" {
		return mcp.NewToolResultError("the host " + hostname + " is not in this upgrade"), nil
	}
	query := url.Values{}
	if targetID != "" {
		query.Set("targetId", targetID)
	}
	if raw, err := client.GetFleet(ctx, path+"/events", query); err != nil {
		upgrade["history"] = map[string]any{"error": err.Error()}
	} else if events, err := fleetData[[]map[string]any](raw); err == nil {
		for _, event := range events {
			if id, ok := event["targetId"].(string); ok {
				event["host"] = hosts[id]
			}
			for _, key := range []string{"id", "orgId", "upgradeId", "targetId"} {
				delete(event, key)
			}
		}
		upgrade["history"] = events
	}
	return fleetResult(upgrade)
}
