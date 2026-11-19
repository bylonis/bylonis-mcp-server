package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bylonis/bylonis-mcp-server/pkg/util"
)

func iaOpsText(r *mcp.CallToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].(mcp.TextContent).Text
}

func TestAskIAOps_ForwardsTheKeyAndTheSource(t *testing.T) {
	var got struct {
		path, key, auth, source, question string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.key, got.auth, got.source = r.URL.Path, r.Header.Get("BYLONIS-API-KEY"), r.Header.Get("Authorization"), r.Header.Get("X-ByLonis-Client-Source")
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		got.question = body["question"]
		_, _ = w.Write([]byte(`{"status":"done","answer":"Conclusión: bien","evidence":[],"conversationId":"c1"}`))
	}))
	defer srv.Close()
	h := newTestHandler(nil)
	h.iaOpsURL = srv.URL + "/ia-ops-api"
	ctx := util.SetClientSource(util.SetAuthHeader(util.SetAPIKey(testCtx(), "sa-key"), "BYLONIS-API-KEY"), "paperclip-lum")
	r, err := h.handleAskIAOps(ctx, makeToolRequest("bylonis_ask_ia_ops", map[string]any{"question": "  ¿Cómo va checkout?  "}))
	if err != nil || r.IsError {
		t.Fatalf("unexpected: %v %s", err, iaOpsText(r))
	}
	if got.path != "/ia-ops-api/ask" || got.key != "sa-key" || got.auth != "" || got.source != "paperclip-lum" || got.question != "¿Cómo va checkout?" {
		t.Errorf("request = %+v", got)
	}
	if !strings.Contains(iaOpsText(r), "Conclusión: bien") {
		t.Errorf("result = %s", iaOpsText(r))
	}

	// Con un token de sesión, va como Authorization.
	ctx = util.SetAuthHeader(util.SetAPIKey(testCtx(), "jwt-token"), "Authorization")
	_, _ = h.handleAskIAOps(ctx, makeToolRequest("bylonis_ask_ia_ops", map[string]any{"question": "¿Cómo va checkout?"}))
	if got.auth != "Bearer jwt-token" || got.key != "" {
		t.Errorf("session request = %+v", got)
	}
}

func TestAskIAOps_ErrorsAndValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"at most 10 questions per hour"}`))
	}))
	defer srv.Close()
	h := newTestHandler(nil)
	h.iaOpsURL = srv.URL
	ctx := util.SetAPIKey(testCtx(), "sa-key")
	r, _ := h.handleAskIAOps(ctx, makeToolRequest("bylonis_ask_ia_ops", map[string]any{"question": "¿Cómo va checkout?"}))
	if !r.IsError || !strings.Contains(iaOpsText(r), "429") {
		t.Errorf("expected the 429 to surface: %s", iaOpsText(r))
	}
	for _, q := range []string{"", "ab", strings.Repeat("x", 2001)} {
		r, _ = h.handleAskIAOps(ctx, makeToolRequest("bylonis_ask_ia_ops", map[string]any{"question": q}))
		if !r.IsError {
			t.Errorf("question %q should be rejected", q[:min(len(q), 10)])
		}
	}
	r, _ = h.handleAskIAOps(testCtx(), makeToolRequest("bylonis_ask_ia_ops", map[string]any{"question": "¿Cómo va checkout?"}))
	if !r.IsError {
		t.Error("without a credential it should fail")
	}
}
