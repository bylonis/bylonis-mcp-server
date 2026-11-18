package tools

// Preguntar a IA OPS desde un agente (bylonis#1053): la pregunta va a la API de IA OPS del tenant (IA_OPS_URL/ask)
// con la misma credencial con que llegó el cliente, y vuelve la conclusión de IA OPS con su evidencia. IA OPS
// investiga en modo lectura; el coste (tokens) lo paga el tenant, así que la herramienta lo dice.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
	"github.com/bylonis/bylonis-mcp-server/pkg/util"
)

// iaOpsAskTimeout cubre el plazo más largo de IA OPS (el modelo local, 300 s) y un margen.
const iaOpsAskTimeout = 330 * time.Second

func (h *Handler) RegisterIAOpsHandlers(s *server.MCPServer) {
	if h.iaOpsURL == "" {
		h.logger.Debug("IA_OPS_URL not set: bylonis_ask_ia_ops is not registered")
		return
	}
	addTool(s, mcp.NewTool("bylonis_ask_ia_ops",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithDescription("Asks IA OPS, the SRE agent of ByLonis, a question about this tenant and returns its conclusion with the evidence it used (queries and results). "+
			"Use it for a diagnosis that needs several steps (e.g. 'why did checkout latency rise at 14:00?', 'is this alert a real problem?') instead of repeating the investigation yourself.\n\n"+
			"IA OPS investigates read-only and changes nothing. It COSTS model tokens to the tenant and takes from seconds up to 5 minutes: ask one precise question with service, time range and what you saw. "+
			"Limits: one question at a time in the tenant and a few per hour per account (429 when exceeded). "+
			"The answer has status (done, timeout or error), answer, evidence and conversationId (a person can open the conversation in IA OPS)."),
		mcp.WithString("question", mcp.Required(), mcp.Description("The question, with the service, time range and what you observed (3 to 2000 characters).")),
	), h.handleAskIAOps)
}

func (h *Handler) handleAskIAOps(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	question := trimmedQuestion(args)
	if n := len([]rune(question)); n < 3 || n > 2000 {
		return mcp.NewToolResultError(`"question" is required (3 to 2000 characters).`), nil
	}
	key, _ := util.GetAPIKey(ctx)
	if key == "" {
		return mcp.NewToolResultError("no credential to ask IA OPS with"), nil
	}
	body, _ := json.Marshal(map[string]string{"question": question})
	ctx, cancel := context.WithTimeout(ctx, iaOpsAskTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.iaOpsURL+"/ask", bytes.NewReader(body))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// La misma credencial con que llegó el cliente: una clave de cuenta de servicio, o un token de sesión.
	if header, _ := util.GetAuthHeader(ctx); strings.EqualFold(header, "Authorization") {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(key, "Bearer "))
	} else {
		httpReq.Header.Set(bylonisclient.APIKeyHeader, key)
	}
	if source, ok := util.GetClientSource(ctx); ok && source != "" {
		httpReq.Header.Set(bylonisclient.ClientSourceHeader, source)
	}
	h.logger.InfoContext(ctx, "Tool called: bylonis_ask_ia_ops", slog.Int("questionChars", len(question)))
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		h.logger.ErrorContext(ctx, "IA OPS ask failed", logpkg.ErrAttr(err))
		return mcp.NewToolResultError(fmt.Sprintf("IA OPS did not answer: %s", err.Error())), nil
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if resp.StatusCode != http.StatusOK {
		return mcp.NewToolResultError(fmt.Sprintf("IA OPS answered %d: %s", resp.StatusCode, logpkg.TruncBody(out))), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

func trimmedQuestion(args map[string]any) string {
	v, _ := args["question"].(string)
	return strings.TrimSpace(v)
}
