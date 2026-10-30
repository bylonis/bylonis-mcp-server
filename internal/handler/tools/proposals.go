package tools

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
)

// The proposals of IA OPS (bylonis#459): a configuration of the collector for a fleet and an Ansible playbook,
// checked before a person runs them. A proposal is a new version (of the fleet or of the playbook) or the
// --check --diff of a playbook: nothing is deployed nor run. Deploying a version and approving the run of a
// playbook are for a person in ByLonis, and ByLonis refuses the approval with an API key.

// proposalNote marks what IA OPS saves, in the history of the versions.
const proposalNote = "Propuesta de IA OPS: "

func (h *Handler) RegisterProposalHandlers(s *server.MCPServer) {
	h.logger.Debug("Registering fleet proposal handlers")

	searchContext := mcp.WithString("searchContext", mcp.Description("The user's original question or search text that triggered this tool call. Always include the user's raw query here for better results."))
	readOnly := func(name, description string, options ...mcp.ToolOption) mcp.Tool {
		return mcp.NewTool(name, append([]mcp.ToolOption{mcp.WithReadOnlyHintAnnotation(true), mcp.WithDestructiveHintAnnotation(false),
			searchContext, mcp.WithDescription(description)}, options...)...)
	}
	proposal := func(name, description string, options ...mcp.ToolOption) mcp.Tool {
		return mcp.NewTool(name, append([]mcp.ToolOption{mcp.WithReadOnlyHintAnnotation(false), mcp.WithDestructiveHintAnnotation(false),
			searchContext, mcp.WithDescription(description)}, options...)...)
	}

	addTool(s, readOnly("bylonis_list_fleets",
		"Lists the fleets of Fleet Control: groups of agents that share a configuration of the collector. Returns name, kind (linux_host, kubernetes), members, collector version, and the newest and stable versions of its configuration."),
		h.handleListFleets)
	addTool(s, readOnly("bylonis_get_fleet_config",
		"Gets a configuration of the collector of a fleet: its template (text/template over YAML of the OpenTelemetry Collector, with .Node, .Labels and .Tenant), its note and who wrote it. Read it before proposing a change, to start from it.",
		mcp.WithString("fleet", mcp.Required(), mcp.Description("Name or id of the fleet.")),
		mcp.WithString("version", mcp.Description("Version number; the newest if omitted."))),
		h.handleGetFleetConfig)
	addTool(s, readOnly("bylonis_validate_fleet_config",
		"Validates a configuration of the collector for a fleet in dry run, as saving it would: the template renders for every member and the collector accepts it (otelcol validate), with the components the policy allows. Returns valid, errors, warnings and the rendering for one member. Saves nothing: use it while writing a proposal.",
		mcp.WithString("fleet", mcp.Required(), mcp.Description("Name or id of the fleet.")),
		mcp.WithString("template", mcp.Required(), mcp.Description("The whole template of the configuration.")),
		mcp.WithString("hostname", mcp.Description("A member to render the preview for; the first one if omitted."))),
		h.handleValidateFleetConfig)
	addTool(s, proposal("bylonis_propose_fleet_config",
		"Proposes a configuration of the collector for a fleet: saves it as the next version of the fleet, only if it validates for every member. It is NOT deployed: a person deploys it in ByLonis (Inventario → Flotas), with canaries and rollback. Validate it first with bylonis_validate_fleet_config, start from bylonis_get_fleet_config, and tell the user the version to deploy.",
		mcp.WithString("fleet", mcp.Required(), mcp.Description("Name or id of the fleet.")),
		mcp.WithString("template", mcp.Required(), mcp.Description("The whole template of the configuration.")),
		mcp.WithString("note", mcp.Required(), mcp.Description("What changes and why, for the person who reviews it."))),
		h.handleProposeFleetConfig)
	addTool(s, readOnly("bylonis_list_playbooks",
		"Lists the Ansible playbooks of the organization, with their newest version."),
		h.handleListPlaybooks)
	addTool(s, readOnly("bylonis_get_playbook",
		"Gets an Ansible playbook with the YAML of a version (the newest if omitted) and the list of its versions.",
		mcp.WithString("playbook", mcp.Required(), mcp.Description("Name or id of the playbook.")),
		mcp.WithString("version", mcp.Description("Version number; the newest if omitted."))),
		h.handleGetPlaybook)
	addTool(s, proposal("bylonis_propose_playbook",
		"Proposes an Ansible playbook: one YAML file, a list of plays each with hosts (all, or groups of the inventory such as site_lumina or class_escritorio), without import_playbook. Saves it in the library of ByLonis as a new playbook, or as the next version of the playbook with that name. Nothing runs: check it with bylonis_check_playbook; a person approves the real run in ByLonis.",
		mcp.WithString("name", mcp.Required(), mcp.Description("Name of the playbook; an existing one gets a new version.")),
		mcp.WithString("content", mcp.Required(), mcp.Description("The YAML of the playbook.")),
		mcp.WithString("note", mcp.Required(), mcp.Description("What it does and why, for the person who reviews it.")),
		mcp.WithString("description", mcp.Description("Description of a new playbook."))),
		h.handleProposePlaybook)
	addTool(s, proposal("bylonis_check_playbook",
		"Checks a version of a playbook on the nodes of a label selector or a group: the site runners run --syntax-check, ansible-lint if they have it, and --check --diff, which changes nothing. Returns the run; read its result with bylonis_get_playbook_run (it takes from seconds to minutes). The real run is approved by a person in ByLonis (Inventario → Ansible), never by this tool. Prefer a test node or a small selector.",
		mcp.WithString("playbook", mcp.Required(), mcp.Description("Name or id of the playbook.")),
		mcp.WithString("version", mcp.Description("Version number; the newest if omitted.")),
		mcp.WithString("selector", mcp.Description("Label selector of the nodes, e.g. 'class=prueba'. Either this or group.")),
		mcp.WithString("group", mcp.Description("Name of a group of the inventory. Either this or selector."))),
		h.handleCheckPlaybook)
	addTool(s, readOnly("bylonis_get_playbook_run",
		"Gets a run of a playbook: its status (checking, checked = waiting for the approval of a person, check_failed, running, succeeded, failed), the recap of each host in the check and in the run (ok, changed, failed, unreachable, or why it was not used), the warnings of ansible-lint and who approved it.",
		mcp.WithString("runId", mcp.Required(), mcp.Description("Id of the run."))),
		h.handleGetPlaybookRun)
}

// fleetByName finds a fleet by its name (without case) or its id.
func (h *Handler) fleetByName(ctx context.Context, client bylonisclient.Client, nameOrID string) (map[string]any, error) {
	raw, err := client.GetFleet(ctx, "/api/v1/fleet/fleets", nil)
	if err != nil {
		return nil, err
	}
	fleets, err := fleetData[[]map[string]any](raw)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(fleets))
	for _, fleet := range fleets {
		name, _ := fleet["name"].(string)
		if fleet["id"] == nameOrID || strings.EqualFold(name, nameOrID) {
			return fleet, nil
		}
		names = append(names, name)
	}
	return nil, fmt.Errorf("there is no fleet %q; the fleets are: %s", nameOrID, strings.Join(names, ", "))
}

// playbookByName finds a playbook by its name (without case) or its id; nil without error when there is none.
func (h *Handler) playbookByName(ctx context.Context, client bylonisclient.Client, nameOrID string) (map[string]any, []string, error) {
	raw, err := client.GetFleet(ctx, "/api/v1/fleet/playbooks", nil)
	if err != nil {
		return nil, nil, err
	}
	playbooks, err := fleetData[[]map[string]any](raw)
	if err != nil {
		return nil, nil, err
	}
	names := make([]string, 0, len(playbooks))
	for _, playbook := range playbooks {
		name, _ := playbook["name"].(string)
		if playbook["id"] == nameOrID || strings.EqualFold(name, nameOrID) {
			return playbook, nil, nil
		}
		names = append(names, name)
	}
	return nil, names, nil
}

func required(args fleetArgs, names ...string) error {
	for _, name := range names {
		if args.text(name) == "" {
			return fmt.Errorf("Parameter validation failed: %q is required", name)
		}
	}
	return nil
}

func (h *Handler) handleListFleets(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetFleet(ctx, "/api/v1/fleet/fleets", nil)
	if err != nil {
		return h.fleetError(ctx, "bylonis_list_fleets", err)
	}
	fleets, err := fleetData[[]map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := make([]map[string]any, 0, len(fleets))
	for _, fleet := range fleets {
		row := map[string]any{}
		for _, key := range []string{"id", "name", "description", "kind", "collectorVersion", "members", "latestVersion", "stableVersion"} {
			if value, ok := fleet[key]; ok {
				row[key] = value
			}
		}
		out = append(out, row)
	}
	return fleetResult(map[string]any{"fleets": out, "total": len(out)})
}

func (h *Handler) handleGetFleetConfig(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if err := required(args, "fleet"); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	fleet, err := h.fleetByName(ctx, client, args.text("fleet"))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	version := args.text("version")
	if version == "" {
		latest, _ := fleet["latestVersion"].(float64)
		if latest == 0 {
			return mcp.NewToolResultError(fmt.Sprintf("fleet %v has no configuration yet: propose its first one", fleet["name"])), nil
		}
		version = strconv.Itoa(int(latest))
	}
	raw, err := client.GetFleet(ctx, fmt.Sprintf("/api/v1/fleet/fleets/%v/configs/%s", fleet["id"], url.PathEscape(version)), nil)
	if err != nil {
		return h.fleetError(ctx, "bylonis_get_fleet_config", err)
	}
	config, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return fleetResult(map[string]any{"fleet": fleet["name"], "fleetId": fleet["id"], "version": config["version"],
		"template": config["template"], "note": config["note"], "createdBy": config["createdBy"], "createdAt": config["createdAt"],
		"stableVersion": fleet["stableVersion"], "latestVersion": fleet["latestVersion"]})
}

func (h *Handler) handleValidateFleetConfig(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if err := required(args, "fleet", "template"); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	fleet, err := h.fleetByName(ctx, client, args.text("fleet"))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	body := map[string]any{"template": args["template"]}
	if args.text("hostname") != "" {
		id, err := h.fleetNodeID(ctx, client, fleetArgs{"hostname": args.text("hostname")})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body["nodeId"] = id
	}
	raw, err := client.PostFleet(ctx, fmt.Sprintf("/api/v1/fleet/fleets/%v/configs/validate", fleet["id"]), body)
	if err != nil {
		return h.fleetError(ctx, "bylonis_validate_fleet_config", err)
	}
	validation, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return fleetResult(validation)
}

func (h *Handler) handleProposeFleetConfig(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if err := required(args, "fleet", "template", "note"); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	fleet, err := h.fleetByName(ctx, client, args.text("fleet"))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.PostFleet(ctx, fmt.Sprintf("/api/v1/fleet/fleets/%v/configs", fleet["id"]),
		map[string]any{"template": args["template"], "note": proposalNote + args.text("note")})
	if err != nil {
		return h.fleetError(ctx, "bylonis_propose_fleet_config", err)
	}
	version, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return fleetResult(map[string]any{
		"fleet": fleet["name"], "version": version["version"], "note": version["note"], "deployed": false,
		"next": fmt.Sprintf("Saved as version %v of fleet %v, not deployed. A person reviews and deploys it in ByLonis: Inventario → Flotas → %v → Desplegar.", version["version"], fleet["name"], fleet["name"]),
	})
}

func (h *Handler) handleListPlaybooks(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetFleet(ctx, "/api/v1/fleet/playbooks", nil)
	if err != nil {
		return h.fleetError(ctx, "bylonis_list_playbooks", err)
	}
	playbooks, err := fleetData[[]map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return fleetResult(map[string]any{"playbooks": playbooks, "total": len(playbooks)})
}

func (h *Handler) handleGetPlaybook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if err := required(args, "playbook"); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	playbook, names, err := h.playbookByName(ctx, client, args.text("playbook"))
	if err != nil {
		return h.fleetError(ctx, "bylonis_get_playbook", err)
	}
	if playbook == nil {
		return mcp.NewToolResultError(fmt.Sprintf("there is no playbook %q; the playbooks are: %s", args.text("playbook"), strings.Join(names, ", "))), nil
	}
	version := args.text("version")
	if version == "" {
		latest, _ := playbook["latestVersion"].(float64)
		version = strconv.Itoa(int(latest))
	}
	base := fmt.Sprintf("/api/v1/fleet/playbooks/%v/versions", playbook["id"])
	raw, err := client.GetFleet(ctx, base+"/"+url.PathEscape(version), nil)
	if err != nil {
		return h.fleetError(ctx, "bylonis_get_playbook", err)
	}
	content, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := map[string]any{"playbook": playbook, "version": content["version"], "content": content["content"], "note": content["note"],
		"createdBy": content["createdBy"]}
	if raw, err := client.GetFleet(ctx, base, nil); err == nil {
		if versions, err := fleetData[[]map[string]any](raw); err == nil {
			list := make([]map[string]any, 0, len(versions))
			for _, v := range versions {
				list = append(list, map[string]any{"version": v["version"], "note": v["note"], "createdBy": v["createdBy"], "createdAt": v["createdAt"]})
			}
			out["versions"] = list
		}
	}
	return fleetResult(out)
}

func (h *Handler) handleProposePlaybook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if err := required(args, "name", "content", "note"); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	existing, _, err := h.playbookByName(ctx, client, args.text("name"))
	if err != nil {
		return h.fleetError(ctx, "bylonis_propose_playbook", err)
	}
	note := proposalNote + args.text("note")
	var raw []byte
	if existing != nil {
		raw, err = client.PostFleet(ctx, fmt.Sprintf("/api/v1/fleet/playbooks/%v/versions", existing["id"]), map[string]any{"content": args["content"], "note": note})
	} else {
		raw, err = client.PostFleet(ctx, "/api/v1/fleet/playbooks", map[string]any{"name": args.text("name"),
			"description": args.text("description"), "content": args["content"], "note": note})
	}
	if err != nil {
		return h.fleetError(ctx, "bylonis_propose_playbook", err)
	}
	saved, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	version := saved["version"]
	if latest, ok := saved["latest"].(map[string]any); ok {
		version = latest["version"]
	}
	return fleetResult(map[string]any{"playbook": args.text("name"), "version": version, "new": existing == nil, "ran": false,
		"next": fmt.Sprintf("Saved as version %v of playbook %s. Nothing ran: check it with bylonis_check_playbook; a person approves the real run in ByLonis (Inventario → Ansible).", version, args.text("name"))})
}

func (h *Handler) handleCheckPlaybook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if err := required(args, "playbook"); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if (args.text("selector") == "") == (args.text("group") == "") {
		return mcp.NewToolResultError(`Parameter validation failed: give "selector" or "group", one of them. Example: {"playbook": "chrony", "selector": "class=prueba"}`), nil
	}
	version := 0
	if text := args.text("version"); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil || parsed < 1 {
			return mcp.NewToolResultError(`Parameter validation failed: "version" is a number from 1`), nil
		}
		version = parsed
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	playbook, names, err := h.playbookByName(ctx, client, args.text("playbook"))
	if err != nil {
		return h.fleetError(ctx, "bylonis_check_playbook", err)
	}
	if playbook == nil {
		return mcp.NewToolResultError(fmt.Sprintf("there is no playbook %q; the playbooks are: %s", args.text("playbook"), strings.Join(names, ", "))), nil
	}
	body := map[string]any{"playbookId": playbook["id"], "version": version, "selector": args.text("selector")}
	if group := args.text("group"); group != "" {
		raw, err := client.GetFleet(ctx, "/api/v1/inventory/groups", nil)
		if err != nil {
			return h.fleetError(ctx, "bylonis_check_playbook", err)
		}
		groups, err := fleetData[[]map[string]any](raw)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		for _, g := range groups {
			if name, _ := g["name"].(string); strings.EqualFold(name, group) {
				body["groupId"] = g["id"]
			}
		}
		if body["groupId"] == nil {
			return mcp.NewToolResultError(fmt.Sprintf("there is no group %q", group)), nil
		}
	}
	raw, err := client.PostFleet(ctx, "/api/v1/fleet/ansible_runs", body)
	if err != nil {
		return h.fleetError(ctx, "bylonis_check_playbook", err)
	}
	run, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	nodes, _ := run["nodeIds"].([]any)
	return fleetResult(map[string]any{"runId": run["id"], "playbook": run["playbookName"], "version": run["version"], "nodes": len(nodes),
		"status": run["status"],
		"next":   "The check runs on the site runners (--syntax-check, ansible-lint, --check --diff); read its result with bylonis_get_playbook_run. Nothing changes on the nodes. A person approves the real run in ByLonis (Inventario → Ansible)."})
}

func (h *Handler) handleGetPlaybookRun(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if err := required(args, "runId"); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetFleet(ctx, "/api/v1/fleet/ansible_runs/"+url.PathEscape(args.text("runId")), nil)
	if err != nil {
		return h.fleetError(ctx, "bylonis_get_playbook_run", err)
	}
	run, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := map[string]any{}
	for _, key := range []string{"id", "playbookName", "version", "selector", "groupId", "status", "checkHosts", "runHosts", "lint", "error",
		"createdBy", "createdAt", "approvedBy", "approvedAt"} {
		if value, ok := run[key]; ok {
			out[key] = value
		}
	}
	if run["status"] == "checked" {
		out["next"] = "The check ended well. A person reviews it and approves the real run in ByLonis (Inventario → Ansible); this tool cannot."
	}
	return fleetResult(out)
}
