package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
)

// The diagnostics (flare) of the agents of the fleet (bylonis#461): what ByLonis knows of the agent of a node, and the
// host part its site runner collected (logs of the supervisor and the collector, configurations, versions, services,
// connectivity), in sections, with the secrets censored by ByLonis. Read only: a person asks for one in the card of
// the node. Reading one needs the permission diagnose, and ByLonis records the read in its audit.

const (
	diagnosticDefaultChars = 20000
	diagnosticMaxChars     = 200000
)

func (h *Handler) registerFleetDiagnosticHandlers(s *server.MCPServer, searchContext mcp.ToolOption) {
	addTool(s, mcp.NewTool("bylonis_list_fleet_diagnostics",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		searchContext,
		mcp.WithDescription("Lists the diagnostics (flares) of the agent of a node, by hostname or nodeId, or of an agent of the OpAMP Bridge of a cluster (agentId), newest first.\n\n"+
			"Returns per diagnostic: id, when and who, status (collecting: the site runner is collecting the host part; ready; partial: only what ByLonis knows, 'error' says why), whether it has a host part (fromHost), its note, size and the names of its sections. They expire in 7 days.\n\n"+
			"Use bylonis_get_fleet_diagnostic to read one."),
		mcp.WithString("hostname", mcp.Description("Hostname of the node. Either this, nodeId or agentId.")),
		mcp.WithString("nodeId", mcp.Description("Id of the node.")),
		mcp.WithString("agentId", mcp.Description("Id of an agent of the OpAMP Bridge (a cluster), from bylonis_get_fleet_node or the agents of a fleet.")),
	), h.handleListFleetDiagnostics)

	addTool(s, mcp.NewTool("bylonis_get_fleet_diagnostic",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		searchContext,
		mcp.WithDescription("Reads a diagnostic (flare) of an agent: by diagnosticId, or the newest one of a node by hostname or nodeId. Use it for 'why does the agent of X not send data / not connect / fail?' after bylonis_get_fleet_node.\n\n"+
			"Sections from ByLonis (source 'server'): node, agent (status, versions, health of each component), opamp_effective_config, config_status (what its fleet wants versus what it runs), own_metrics (last hour), history, runner_jobs. "+
			"Sections from the host (source 'host'): versions, system, services, supervisor_config, base_config, effective_config, state_files, connectivity (whether the host reaches ByLonis and the ingestion), supervisor_log, collector_log; or, for a cluster, the kubectl output (pods, events, daemonset_log, bridge_log, configmaps).\n\n"+
			"Each section is cut to 'maxChars' (a log keeps its end, where the failure usually is); 'sections' picks some by name. The secrets come censored as <censurado>: never ask the user for them. "+
			"Quote the lines of the logs that explain the problem. If there is no diagnostic, or it has no host part, tell the user to ask for one in the card of the node (tab Diagnóstico): there is no tool to create one."),
		mcp.WithString("diagnosticId", mcp.Description("Id of the diagnostic. Or hostname / nodeId for the newest of a node.")),
		mcp.WithString("hostname", mcp.Description("Hostname of the node: its newest diagnostic.")),
		mcp.WithString("nodeId", mcp.Description("Id of the node: its newest diagnostic.")),
		mcp.WithString("sections", mcp.Description("Comma-separated names of the sections to return, e.g. 'supervisor_log,connectivity'. All by default.")),
		mcp.WithString("maxChars", mcp.Description("Maximum characters of each section. Default 20000, max 200000.")),
	), h.handleGetFleetDiagnostic)
}

// diagnosticError adds to an error of the API what explains it for a diagnostic.
func (h *Handler) diagnosticError(ctx context.Context, tool string, err error) (*mcp.CallToolResult, error) {
	if strings.Contains(err.Error(), "403") {
		err = fmt.Errorf("%w (reading diagnostics needs the permission diagnose: an admin grants it to the key of IA OPS in Inventario → Permisos, for the organization, a site, a group or a fleet)", err)
	}
	return h.fleetError(ctx, tool, err)
}

func (h *Handler) diagnosticQuery(ctx context.Context, client bylonisclient.Client, args fleetArgs) (url.Values, error) {
	if agentID := args.text("agentId"); agentID != "" {
		return url.Values{"agentId": {agentID}}, nil
	}
	nodeID, err := h.fleetNodeID(ctx, client, args)
	if err != nil {
		return nil, err
	}
	return url.Values{"nodeId": {nodeID}}, nil
}

func (h *Handler) handleListFleetDiagnostics(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query, err := h.diagnosticQuery(ctx, client, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetFleet(ctx, "/api/v1/fleet/diagnostics", query)
	if err != nil {
		return h.diagnosticError(ctx, "bylonis_list_fleet_diagnostics", err)
	}
	diagnostics, err := fleetData[[]map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]map[string]any, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		item := map[string]any{}
		for _, key := range []string{"id", "name", "createdAt", "createdBy", "status", "fromHost", "note", "error", "size", "expiresAt", "jobId"} {
			if value, ok := diagnostic[key]; ok && value != nil && value != "" {
				item[key] = value
			}
		}
		sections := []string{}
		info, _ := diagnostic["sectionInfo"].([]any)
		for _, raw := range info {
			if section, ok := raw.(map[string]any); ok {
				name, _ := section["name"].(string)
				sections = append(sections, name)
			}
		}
		item["sections"] = sections
		out = append(out, item)
	}
	return fleetResult(map[string]any{"diagnostics": out, "total": len(out)})
}

func (h *Handler) handleGetFleetDiagnostic(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	maxChars, err := args.number("maxChars", diagnosticDefaultChars, diagnosticMaxChars)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if maxChars == 0 {
		maxChars = diagnosticDefaultChars
	}
	id := args.text("diagnosticId")
	if id == "" {
		query, err := h.diagnosticQuery(ctx, client, args)
		if err != nil {
			return mcp.NewToolResultError(`give "diagnosticId", or "hostname" / "nodeId" for the newest diagnostic of a node: ` + err.Error()), nil
		}
		raw, err := client.GetFleet(ctx, "/api/v1/fleet/diagnostics", query)
		if err != nil {
			return h.diagnosticError(ctx, "bylonis_get_fleet_diagnostic", err)
		}
		list, err := fleetData[[]map[string]any](raw)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(list) == 0 {
			return mcp.NewToolResultError("the node has no diagnostic: ask the user to request one in the card of the node (tab Diagnóstico), with the host part"), nil
		}
		id, _ = list[0]["id"].(string)
	}
	raw, err := client.GetFleet(ctx, "/api/v1/fleet/diagnostics/"+url.PathEscape(id), nil)
	if err != nil {
		return h.diagnosticError(ctx, "bylonis_get_fleet_diagnostic", err)
	}
	diagnostic, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	wanted := map[string]bool{}
	for _, name := range strings.Split(args.text("sections"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			wanted[name] = true
		}
	}
	out := map[string]any{}
	for _, key := range []string{"id", "name", "nodeId", "agentId", "createdAt", "createdBy", "status", "fromHost", "note", "error", "expiresAt"} {
		if value, ok := diagnostic[key]; ok && value != nil && value != "" {
			out[key] = value
		}
	}
	available := []string{}
	sections := []map[string]any{}
	all, _ := diagnostic["sections"].([]any)
	for _, raw := range all {
		section, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := section["name"].(string)
		available = append(available, name)
		if len(wanted) > 0 && !wanted[name] {
			continue
		}
		content, _ := section["content"].(string)
		cut, omitted := cutDiagnosticSection(name, content, maxChars)
		item := map[string]any{"name": name, "source": section["source"], "content": cut}
		if truncated, _ := section["truncated"].(bool); truncated || omitted > 0 {
			item["truncated"] = true
		}
		if omitted > 0 {
			item["omittedChars"] = omitted
		}
		sections = append(sections, item)
	}
	out["sections"] = sections
	out["availableSections"] = available
	return fleetResult(out)
}

// cutDiagnosticSection keeps maxChars of a section: the end of a log, the start of the rest. It returns how many
// characters it left out.
func cutDiagnosticSection(name string, content string, maxChars int) (string, int) {
	runes := utf8.RuneCountInString(content)
	if runes <= maxChars {
		return content, 0
	}
	text := []rune(content)
	if strings.HasSuffix(name, "_log") {
		return string(text[len(text)-maxChars:]), runes - maxChars
	}
	return string(text[:maxChars]), runes - maxChars
}
