package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
)

// The themes of the organization (bylonis#704, bylonis#705): the colors of the interface in light and dark mode.
// The MCP has no logic of its own: every tool calls an endpoint of ByLonis, which generates the themes with its
// deterministic engine, checks the colors and measures the WCAG contrast. Generating, patching and validating
// save nothing; only bylonis_save_theme saves, and ByLonis lets only administrators write (bylonis#644 brings
// finer permissions). Natural-language themes (prompt) are bylonis#931 and are not available yet.

// promptUnsupported is the answer to a prompt, until bylonis#931.
const promptUnsupported = "natural-language themes are not available yet (bylonis#931): describe the theme with base_color, palette_from_image and/or personality instead"

func (h *Handler) RegisterThemeHandlers(s *server.MCPServer) {
	h.logger.Debug("Registering theme handlers")

	searchContext := mcp.WithString("searchContext", mcp.Description("The user's original question or search text that triggered this tool call. Always include the user's raw query here for better results."))
	readOnly := func(name, description string, options ...mcp.ToolOption) mcp.Tool {
		return mcp.NewTool(name, append([]mcp.ToolOption{mcp.WithReadOnlyHintAnnotation(true), mcp.WithDestructiveHintAnnotation(false),
			searchContext, mcp.WithDescription(description)}, options...)...)
	}
	// Generating, patching and validating are POSTs that save nothing.
	dryRun := readOnly
	write := func(name, description string, options ...mcp.ToolOption) mcp.Tool {
		return mcp.NewTool(name, append([]mcp.ToolOption{mcp.WithReadOnlyHintAnnotation(false), mcp.WithDestructiveHintAnnotation(false),
			searchContext, mcp.WithDescription(description)}, options...)...)
	}
	themeObject := func(name, description string, options ...mcp.PropertyOption) mcp.ToolOption {
		return mcp.WithObject(name, append([]mcp.PropertyOption{mcp.AdditionalProperties(true), mcp.Description(description)}, options...)...)
	}
	prompt := mcp.WithString("prompt", mcp.Description("Not available yet (bylonis#931): a natural-language description is rejected. Leave it empty."))

	addTool(s, readOnly("bylonis_get_design_surface",
		"Describes what a theme of ByLonis is made of, before generating or editing one: the modes (light, dark), the 48 semantic color tokens with their group (surfaces, text, accent, status, charts), the personalities of the generator, the contrast pairs with their WCAG 2.1 minimum, the accepted color formats (#rrggbb, #rrggbbaa, oklch(L C H[/A])) and the built-in themes."),
		h.handleGetDesignSurface)
	addTool(s, dryRun("bylonis_generate_theme",
		"Generates a theme (both modes, every token) with the deterministic engine of ByLonis from a base color, the colors of a logo and/or a personality; it corrects the lightness until every contrast pair passes. Saves nothing: show it, adjust it with bylonis_patch_theme_tokens and save it with bylonis_save_theme. Give at least one of base_color, palette_from_image, personality. Needs an administrator API key.",
		mcp.WithString("base_color", mcp.Description("Base color: #rrggbb, #rrggbbaa or oklch(L C H). It becomes the accent; e.g. '#3b82f6'.")),
		mcp.WithArray("palette_from_image", mcp.WithStringItems(), mcp.Description("Up to 8 colors extracted from a logo or image (only the hex values, never the image), the dominant first: it is the accent when base_color is absent, and the second is the brand color.")),
		mcp.WithString("personality", mcp.Enum("technical-dark", "clean-light", "corporate", "high-contrast"), mcp.Description("Style of the neutrals and the contrast: technical-dark, clean-light, corporate or high-contrast (text at 7:1). Alone, it uses a default base color.")),
		mcp.WithString("name", mcp.Description("Name for the theme; derived from the input if omitted.")),
		prompt),
		h.handleGenerateTheme)
	addTool(s, dryRun("bylonis_patch_theme_tokens",
		"Changes some tokens of a theme and returns the new modes, the diff and the contrast report. Saves nothing and does not correct the contrast: check contrast_report, then save with bylonis_save_theme. Give theme_id (a saved theme or 'bylonis', the built-in one) or theme. Needs an administrator API key.",
		mcp.WithString("theme_id", mcp.Description("Id of a saved theme, or 'bylonis'. Either this or theme.")),
		themeObject("theme", "A theme as returned by bylonis_generate_theme or bylonis_get_theme: {\"modes\": {\"light\": {...}, \"dark\": {...}}}. Either this or theme_id."),
		themeObject("patch", "The changes: \"mode.token\" → color, e.g. {\"light.accent\": \"#2563eb\", \"dark.chart-3\": \"oklch(0.7 0.12 30)\"}.", mcp.Required()),
		prompt),
		h.handlePatchThemeTokens)
	addTool(s, dryRun("bylonis_validate_theme",
		"Validates a theme without saving it: every token present with an accepted color, and the WCAG 2.1 contrast of every semantic pair in both modes (ratio, minimum, ok). Needs an administrator API key.",
		themeObject("theme", "The theme: {\"name\"?: \"...\", \"modes\": {\"light\": {...}, \"dark\": {...}}}.", mcp.Required())),
		h.handleValidateTheme)
	addTool(s, readOnly("bylonis_list_themes",
		"Lists the themes of the organization (the built-in 'bylonis' first) with whether each passes the contrast checks, the theme of the organization and the one that applies to the caller."),
		h.handleListThemes)
	addTool(s, readOnly("bylonis_get_theme",
		"Gets a theme with its colors in both modes and the pairs that fail the contrast checks.",
		mcp.WithString("theme_id", mcp.Required(), mcp.Description("Id of the theme, or 'bylonis' for the built-in one."))),
		h.handleGetTheme)
	addTool(s, write("bylonis_save_theme",
		"Saves a theme: a new one, or the given theme_id updated (the built-in one cannot change). Failing the contrast does not prevent saving, but such a theme cannot be the theme of the organization. It does not make it the theme of the organization: an administrator picks that in ByLonis. Needs an administrator API key.",
		mcp.WithString("theme_id", mcp.Description("Id of the theme to update; omit it to create a new theme.")),
		mcp.WithString("name", mcp.Required(), mcp.Description("Name of the theme, unique in the organization.")),
		themeObject("modes", "The colors: {\"light\": {...}, \"dark\": {...}} with every token (the modes of bylonis_generate_theme or bylonis_patch_theme_tokens).", mcp.Required())),
		h.handleSaveTheme)
}

// objectArg reads an object argument; a JSON string with the object works too, as some clients send it so.
func objectArg(args map[string]any, name string) (map[string]any, error) {
	switch value := args[name].(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return value, nil
	case string:
		if strings.TrimSpace(value) == "" {
			return nil, nil
		}
		object := map[string]any{}
		if err := json.Unmarshal([]byte(value), &object); err != nil {
			return nil, fmt.Errorf("Parameter validation failed: %q must be an object: %v", name, err)
		}
		return object, nil
	}
	return nil, fmt.Errorf("Parameter validation failed: %q must be an object", name)
}

// modesOf takes the modes of a theme given as {"modes": {...}} or straight as {"light": {...}, "dark": {...}}.
func modesOf(name string, theme map[string]any) (map[string]any, error) {
	if modes, ok := theme["modes"].(map[string]any); ok {
		return modes, nil
	}
	if theme["light"] != nil || theme["dark"] != nil {
		return map[string]any{"light": theme["light"], "dark": theme["dark"]}, nil
	}
	return nil, fmt.Errorf(`Parameter validation failed: %q needs the modes: {"modes": {"light": {...}, "dark": {...}}}`, name)
}

// stringList reads an array of strings; a comma-separated string works too.
func stringList(args map[string]any, name string) ([]string, error) {
	switch value := args[name].(type) {
	case nil:
		return nil, nil
	case string:
		out := []string{}
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("Parameter validation failed: %q must be a list of colors", name)
			}
			out = append(out, strings.TrimSpace(text))
		}
		return out, nil
	}
	return nil, fmt.Errorf("Parameter validation failed: %q must be a list of colors", name)
}

// themeError explains the errors of ByLonis that a client can act on.
func (h *Handler) themeError(ctx context.Context, tool string, err error) (*mcp.CallToolResult, error) {
	h.logger.ErrorContext(ctx, "Theme tool failed", slog.String("tool", tool), logpkg.ErrAttr(err))
	message := err.Error()
	switch {
	case strings.Contains(message, "status 403"):
		message += " — this needs the API key of an administrator (a service account with the admin role); finer permissions arrive with bylonis#644"
	case strings.Contains(message, "theme_prompt_unsupported"):
		message = promptUnsupported
	case strings.Contains(message, "status 404"):
		message += " (the theme does not exist; bylonis_list_themes lists them)"
	}
	return mcp.NewToolResultError(message), nil
}

// contrastReport summarizes the contrast of ByLonis: whether it passes and only the pairs that fail.
func contrastReport(contrast map[string]any) map[string]any {
	failing := []map[string]any{}
	for _, mode := range []string{"light", "dark"} {
		checks, _ := contrast[mode].([]any)
		for _, item := range checks {
			check, _ := item.(map[string]any)
			if ok, _ := check["ok"].(bool); !ok && check != nil {
				failing = append(failing, map[string]any{"mode": mode, "fg": check["fg"], "bg": check["bg"], "kind": check["kind"],
					"ratio": check["ratio"], "min": check["min"]})
			}
		}
	}
	return map[string]any{"ok": contrast["ok"], "failures": contrast["failures"], "failing": failing}
}

func asMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

func (h *Handler) handleGetDesignSurface(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetTheme(ctx, "/api/v1/themes/design-surface")
	if err != nil {
		return h.themeError(ctx, "bylonis_get_design_surface", err)
	}
	surface, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return fleetResult(surface)
}

func (h *Handler) handleGenerateTheme(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if args.text("prompt") != "" {
		return mcp.NewToolResultError(promptUnsupported), nil
	}
	palette, err := stringList(args, "palette_from_image")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	body := map[string]any{"baseColor": args.text("base_color"), "personality": args.text("personality"), "name": args.text("name")}
	if len(palette) > 0 {
		body["palette"] = palette
	}
	if body["baseColor"] == "" && body["personality"] == "" && len(palette) == 0 {
		return mcp.NewToolResultError(`Parameter validation failed: give base_color, palette_from_image or personality. Example: {"base_color": "#3b82f6", "personality": "corporate"}`), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.SendTheme(ctx, http.MethodPost, "/api/v1/themes/generate", body)
	if err != nil {
		return h.themeError(ctx, "bylonis_generate_theme", err)
	}
	generated, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return fleetResult(map[string]any{
		"theme":           map[string]any{"name": generated["name"], "modes": generated["modes"]},
		"contrast_report": contrastReport(asMap(generated["contrast"])),
		"warnings":        generated["warnings"],
		"saved":           false,
		"next":            "Not saved. Adjust tokens with bylonis_patch_theme_tokens and save it with bylonis_save_theme.",
	})
}

func (h *Handler) handlePatchThemeTokens(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if args.text("prompt") != "" {
		return mcp.NewToolResultError(promptUnsupported), nil
	}
	patch, err := objectArg(args, "patch")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(patch) == 0 {
		return mcp.NewToolResultError(`Parameter validation failed: "patch" is required, e.g. {"light.accent": "#2563eb"}`), nil
	}
	theme, err := objectArg(args, "theme")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	themeID := args.text("theme_id")
	if (themeID == "") == (theme == nil) {
		return mcp.NewToolResultError(`Parameter validation failed: give "theme_id" or "theme", one of them`), nil
	}
	body := map[string]any{"patch": patch}
	if themeID != "" {
		body["themeId"] = themeID
	} else {
		modes, err := modesOf("theme", theme)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body["modes"] = modes
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.SendTheme(ctx, http.MethodPost, "/api/v1/themes/patch", body)
	if err != nil {
		return h.themeError(ctx, "bylonis_patch_theme_tokens", err)
	}
	patched, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := map[string]any{
		"theme":           map[string]any{"modes": patched["modes"]},
		"diff":            patched["diff"],
		"contrast_report": contrastReport(asMap(patched["contrast"])),
		"saved":           false,
		"next":            "Not saved and the contrast is not corrected: check contrast_report, then save it with bylonis_save_theme.",
	}
	if themeID != "" {
		out["theme_id"] = themeID
	}
	return fleetResult(out)
}

func (h *Handler) handleValidateTheme(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	theme, err := objectArg(args, "theme")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if theme == nil {
		return mcp.NewToolResultError(`Parameter validation failed: "theme" is required: {"modes": {"light": {...}, "dark": {...}}}`), nil
	}
	modes, err := modesOf("theme", theme)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	name, _ := theme["name"].(string)
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.SendTheme(ctx, http.MethodPost, "/api/v1/themes/validate", map[string]any{"name": name, "modes": modes})
	if err != nil {
		return h.themeError(ctx, "bylonis_validate_theme", err)
	}
	validation, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return fleetResult(validation)
}

func (h *Handler) handleListThemes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetTheme(ctx, "/api/v1/themes")
	if err != nil {
		return h.themeError(ctx, "bylonis_list_themes", err)
	}
	list, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	themes, _ := list["themes"].([]any)
	rows := make([]map[string]any, 0, len(themes))
	for _, item := range themes {
		theme := asMap(item)
		contrast := asMap(theme["contrast"])
		rows = append(rows, map[string]any{"id": theme["id"], "name": theme["name"], "source": theme["source"], "builtin": theme["builtin"],
			"contrastOk": contrast["ok"], "contrastFailures": contrast["failures"], "updatedAt": theme["updatedAt"]})
	}
	return fleetResult(map[string]any{"themes": rows, "total": len(rows), "orgThemeId": list["orgThemeId"],
		"userThemesAllowed": list["userThemesAllowed"], "effectiveThemeId": list["effectiveThemeId"]})
}

func (h *Handler) handleGetTheme(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if err := required(args, "theme_id"); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetTheme(ctx, "/api/v1/themes/"+url.PathEscape(args.text("theme_id")))
	if err != nil {
		return h.themeError(ctx, "bylonis_get_theme", err)
	}
	theme, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	theme["contrast_report"] = contrastReport(asMap(theme["contrast"]))
	delete(theme, "contrast")
	return fleetResult(theme)
}

func (h *Handler) handleSaveTheme(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := fleetArgsOf(req)
	if err := required(args, "name"); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	given, err := objectArg(args, "modes")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if given == nil {
		return mcp.NewToolResultError(`Parameter validation failed: "modes" is required: {"light": {...}, "dark": {...}}`), nil
	}
	modes, err := modesOf("modes", given)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	body := map[string]any{"name": args.text("name"), "modes": modes}
	themeID := args.text("theme_id")
	var raw json.RawMessage
	if themeID == "" {
		raw, err = client.SendTheme(ctx, http.MethodPost, "/api/v1/themes", body)
	} else {
		raw, err = client.SendTheme(ctx, http.MethodPut, "/api/v1/themes/"+url.PathEscape(themeID), body)
	}
	if err != nil {
		return h.themeError(ctx, "bylonis_save_theme", err)
	}
	theme, err := fleetData[map[string]any](raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	report := contrastReport(asMap(theme["contrast"]))
	next := "Saved. It is not the theme of the organization: an administrator picks that in ByLonis."
	if ok, _ := report["ok"].(bool); !ok {
		next = "Saved, but it fails the contrast checks: it cannot be the theme of the organization until they pass (fix the failing pairs with bylonis_patch_theme_tokens)."
	}
	return fleetResult(map[string]any{"id": theme["id"], "name": theme["name"], "source": theme["source"], "created": themeID == "",
		"contrast_report": report, "saved": true, "next": next})
}
