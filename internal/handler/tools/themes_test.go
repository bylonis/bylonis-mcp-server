package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
)

type themeRequest struct {
	method, path string
	body         map[string]any
}

// themeBackend is a fake ByLonis for the themes: it answers by "METHOD path" with the render envelope and records
// every request.
func themeBackend(t *testing.T, answers map[string]struct {
	status int
	data   string
}) (*Handler, *[]themeRequest) {
	t.Helper()
	requests := []themeRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		requests = append(requests, themeRequest{method: r.Method, path: r.URL.Path, body: body})
		answer, ok := answers[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":"error","error":{"code":"theme_not_found","message":"not found"}}`))
			return
		}
		if answer.status >= 400 {
			w.WriteHeader(answer.status)
			_, _ = w.Write([]byte(answer.data))
			return
		}
		w.WriteHeader(answer.status)
		_, _ = w.Write([]byte(`{"status":"success","data":` + answer.data + `}`))
	}))
	t.Cleanup(server.Close)
	client := bylonisclient.NewClient(logpkg.New("error"), server.URL, "key", "BYLONIS-API-KEY", nil)
	return newTestHandler(client), &requests
}

type answer = struct {
	status int
	data   string
}

const themeContrast = `{"light":[{"fg":"text-primary","bg":"bg-canvas","kind":"text","ratio":15.2,"min":4.5,"ok":true}],` +
	`"dark":[{"fg":"accent-on","bg":"accent","kind":"text","ratio":3.1,"min":4.5,"ok":false}],"ok":false,"failures":1}`

const themeModes = `{"light":{"accent":"#3a81f5"},"dark":{"accent":"#5696ff"}}`

func errorText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.True(t, result.IsError)
	return result.Content[0].(mcp.TextContent).Text
}

func TestGenerateTheme(t *testing.T) {
	h, requests := themeBackend(t, map[string]answer{
		"POST /api/v1/themes/generate": {200, `{"name":"Theme #3b82f6","modes":` + themeModes + `,"contrast":` + themeContrast + `,"warnings":["w"]}`},
	})
	out := resultJSON(t, call(t, h.handleGenerateTheme, map[string]any{
		"base_color": "#3b82f6", "palette_from_image": []any{"#3b82f6", "#ff6600"}, "personality": "corporate",
	}))
	require.Len(t, *requests, 1)
	assert.Equal(t, map[string]any{"baseColor": "#3b82f6", "palette": []any{"#3b82f6", "#ff6600"}, "personality": "corporate", "name": ""}, (*requests)[0].body)
	assert.Equal(t, "Theme #3b82f6", out["theme"].(map[string]any)["name"])
	assert.Equal(t, false, out["saved"])
	report := out["contrast_report"].(map[string]any)
	assert.Equal(t, false, report["ok"])
	require.Len(t, report["failing"], 1, "only the failing pairs")
	assert.Equal(t, "dark", report["failing"].([]any)[0].(map[string]any)["mode"])
	assert.Equal(t, []any{"w"}, out["warnings"])

	// A palette as a comma-separated string works too.
	_ = resultJSON(t, call(t, h.handleGenerateTheme, map[string]any{"palette_from_image": "#111111, #222222"}))
	assert.Equal(t, []any{"#111111", "#222222"}, (*requests)[1].body["palette"])
}

func TestThemePromptIsRejected(t *testing.T) {
	h, requests := themeBackend(t, map[string]answer{})
	text := errorText(t, call(t, h.handleGenerateTheme, map[string]any{"prompt": "un theme otoñal", "base_color": "#3b82f6"}))
	assert.Contains(t, text, "bylonis#931")
	text = errorText(t, call(t, h.handlePatchThemeTokens, map[string]any{"prompt": "más oscuro", "theme_id": "bylonis", "patch": map[string]any{"light.accent": "#000000"}}))
	assert.Contains(t, text, "bylonis#931")
	assert.Empty(t, *requests, "nothing reaches ByLonis")

	text = errorText(t, call(t, h.handleGenerateTheme, map[string]any{}))
	assert.Contains(t, text, "base_color")
}

func TestThemeBackendPromptErrorIsExplained(t *testing.T) {
	h, _ := themeBackend(t, map[string]answer{
		"POST /api/v1/themes/generate": {400, `{"status":"error","error":{"code":"theme_prompt_unsupported","message":"natural-language themes are not available yet (#931)"}}`},
	})
	assert.Contains(t, errorText(t, call(t, h.handleGenerateTheme, map[string]any{"personality": "corporate"})), "bylonis#931")
}

func TestPatchThemeTokens(t *testing.T) {
	h, requests := themeBackend(t, map[string]answer{
		"POST /api/v1/themes/patch": {200, `{"modes":` + themeModes + `,"diff":[{"path":"light.accent","from":"#000000","to":"#3a81f5"}],"contrast":` + themeContrast + `}`},
	})
	out := resultJSON(t, call(t, h.handlePatchThemeTokens, map[string]any{"theme_id": "bylonis", "patch": map[string]any{"light.accent": "#3a81f5"}}))
	assert.Equal(t, map[string]any{"themeId": "bylonis", "patch": map[string]any{"light.accent": "#3a81f5"}}, (*requests)[0].body)
	assert.Len(t, out["diff"], 1)
	assert.Equal(t, "bylonis", out["theme_id"])

	// A theme with its modes, as a JSON string, and straight light/dark.
	_ = resultJSON(t, call(t, h.handlePatchThemeTokens, map[string]any{"theme": `{"name":"x","modes":` + themeModes + `}`, "patch": `{"dark.accent":"#ffffff"}`}))
	assert.Equal(t, "#5696ff", (*requests)[1].body["modes"].(map[string]any)["dark"].(map[string]any)["accent"])
	_ = resultJSON(t, call(t, h.handlePatchThemeTokens, map[string]any{"theme": map[string]any{"light": map[string]any{}, "dark": map[string]any{}}, "patch": map[string]any{"dark.accent": "#ffffff"}}))
	assert.NotNil(t, (*requests)[2].body["modes"])

	assert.Contains(t, errorText(t, call(t, h.handlePatchThemeTokens, map[string]any{"patch": map[string]any{"light.accent": "#000000"}})), "theme_id")
	assert.Contains(t, errorText(t, call(t, h.handlePatchThemeTokens, map[string]any{"theme_id": "bylonis"})), "patch")
	assert.Len(t, *requests, 3)
}

func TestValidateListGetTheme(t *testing.T) {
	h, requests := themeBackend(t, map[string]answer{
		"POST /api/v1/themes/validate": {200, `{"valid":true,"errors":[],"contrast":` + themeContrast + `}`},
		"GET /api/v1/themes": {200, `{"themes":[{"id":"bylonis","name":"ByLonis","source":"builtin","builtin":true,"modes":` + themeModes + `,"contrast":{"ok":true,"failures":0}}],` +
			`"orgThemeId":"bylonis","userThemesAllowed":false,"userThemeId":"","effectiveThemeId":"bylonis"}`},
		"GET /api/v1/themes/bylonis":        {200, `{"id":"bylonis","name":"ByLonis","modes":` + themeModes + `,"contrast":` + themeContrast + `}`},
		"GET /api/v1/themes/design-surface": {200, `{"schemaVersion":"1.0.0","modes":["light","dark"],"builtin":["bylonis"]}`},
	})
	out := resultJSON(t, call(t, h.handleValidateTheme, map[string]any{"theme": map[string]any{"name": "Mío", "modes": map[string]any{"light": map[string]any{}, "dark": map[string]any{}}}}))
	assert.Equal(t, true, out["valid"])
	assert.NotNil(t, out["contrast"], "the full report, pair by pair")
	assert.Equal(t, "Mío", (*requests)[0].body["name"])

	out = resultJSON(t, call(t, h.handleListThemes, map[string]any{}))
	themes := out["themes"].([]any)
	require.Len(t, themes, 1)
	assert.Equal(t, true, themes[0].(map[string]any)["contrastOk"])
	assert.Nil(t, themes[0].(map[string]any)["modes"], "the list is compact")
	assert.Equal(t, "bylonis", out["effectiveThemeId"])

	out = resultJSON(t, call(t, h.handleGetTheme, map[string]any{"theme_id": "bylonis"}))
	assert.NotNil(t, out["modes"])
	assert.Nil(t, out["contrast"])
	assert.EqualValues(t, 1, out["contrast_report"].(map[string]any)["failures"])

	out = resultJSON(t, call(t, h.handleGetDesignSurface, map[string]any{}))
	assert.Equal(t, "1.0.0", out["schemaVersion"])

	assert.Contains(t, errorText(t, call(t, h.handleGetTheme, map[string]any{"theme_id": "nope"})), "bylonis_list_themes")
}

func TestSaveTheme(t *testing.T) {
	h, requests := themeBackend(t, map[string]answer{
		"POST /api/v1/themes":     {201, `{"id":"t1","name":"Marca","source":"manual","contrast":{"ok":true,"failures":0,"light":[],"dark":[]}}`},
		"PUT /api/v1/themes/t1":   {200, `{"id":"t1","name":"Marca 2","source":"manual","contrast":` + themeContrast + `}`},
		"PUT /api/v1/themes/t403": {403, `{"status":"error","error":{"code":"forbidden","message":"only admins can access this resource"}}`},
	})
	out := resultJSON(t, call(t, h.handleSaveTheme, map[string]any{"name": "Marca", "modes": map[string]any{"modes": map[string]any{"light": map[string]any{}, "dark": map[string]any{}}}}))
	assert.Equal(t, "POST", (*requests)[0].method)
	assert.Equal(t, true, out["created"])
	assert.Contains(t, out["next"], "not the theme of the organization")

	out = resultJSON(t, call(t, h.handleSaveTheme, map[string]any{"theme_id": "t1", "name": "Marca 2", "modes": map[string]any{"light": map[string]any{}, "dark": map[string]any{}}}))
	assert.Equal(t, "PUT", (*requests)[1].method)
	assert.Equal(t, "Marca 2", (*requests)[1].body["name"])
	assert.Contains(t, out["next"], "fails the contrast")

	text := errorText(t, call(t, h.handleSaveTheme, map[string]any{"theme_id": "t403", "name": "x", "modes": map[string]any{"light": map[string]any{}, "dark": map[string]any{}}}))
	assert.Contains(t, text, "403")
	assert.Contains(t, text, "administrator")

	assert.True(t, call(t, h.handleSaveTheme, map[string]any{"name": "x"}).IsError, "modes are required")
	assert.True(t, call(t, h.handleSaveTheme, map[string]any{"modes": map[string]any{"light": map[string]any{}}}).IsError, "a name is required")
	assert.Len(t, *requests, 3)
}

// The theme tools never delete a theme nor pick the theme of the organization.
func TestThemeToolsNeverDeleteNorPickTheOrgTheme(t *testing.T) {
	h, requests := themeBackend(t, map[string]answer{})
	for _, handle := range []func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){
		h.handleGenerateTheme, h.handlePatchThemeTokens, h.handleValidateTheme, h.handleListThemes, h.handleGetTheme, h.handleSaveTheme, h.handleGetDesignSurface,
	} {
		_ = call(t, handle, map[string]any{"theme_id": "t1", "name": "x", "base_color": "#000000", "patch": map[string]any{"light.accent": "#000000"},
			"theme": map[string]any{"light": map[string]any{}, "dark": map[string]any{}}, "modes": map[string]any{"light": map[string]any{}, "dark": map[string]any{}}})
	}
	require.NotEmpty(t, *requests)
	for _, r := range *requests {
		assert.NotEqual(t, http.MethodDelete, r.method)
		assert.NotContains(t, r.path, "org-default")
	}
}
