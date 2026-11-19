package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
)

// Capacity Planning (bylonis#893, bylonis#1091): what fills up and when, from the capacity view of ByLonis. Read
// only: IA OPS answers «what fills up first?» in the chat without the operator opening the view.

const (
	capacityDefaultLimit = 20
	capacityMaxLimit     = 100
)

var capacityKinds = []string{"filesystem", "node_filesystem", "pvc", "host_memory", "node_memory", "workload_memory", "workload_cpu"}

func (h *Handler) RegisterCapacityHandlers(s *server.MCPServer) {
	h.logger.Debug("Registering capacity handlers")
	addTool(s, mcp.NewTool("bylonis_list_capacity_resources",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("searchContext", mcp.Description("The user's original question or search text that triggered this tool call. Always include the user's raw query here for better results.")),
		mcp.WithDescription("List the resources with a limit that ByLonis forecasts (filesystems, Kubernetes node disks, PVCs, host, node and pod memory, and pod CPU against its request), "+
			"sorted by when they fill up, soonest first: the capacity view (Observar → Capacidad). Each resource has its usage and limit, the ratio (0–1), the trend in points per day, "+
			"daysToFull and fullAt when it is forecast full within its horizon (90 days with the short scale, 365 with the long one), and whether the forecast is reliable (valid, or the reason "+
			"it is not: insufficient_history, too_many_gaps, level_shift, high_error). The snapshot is computed every hour. Use it to answer «what fills up first?», to find what to watch "+
			"before proposing a predictive alert (threshold kind time_to_limit), and to explain a «time to limit» alert. withinDays keeps only the ones forecast full within that many days."),
		mcp.WithString("kind", mcp.Description("Only resources of this kind: one of filesystem, node_filesystem, pvc, host_memory, node_memory, workload_memory, workload_cpu (CPU is informative: it does not fill up).")),
		mcp.WithString("search", mcp.Description("Only resources whose labels contain this text (host, mountpoint, namespace, pod…), case-insensitive.")),
		mcp.WithString("withinDays", mcp.Description("Only resources with a reliable forecast full within this many days, e.g. '7'.")),
		mcp.WithString("limit", mcp.Description(fmt.Sprintf("How many resources to return, soonest first. Default %d, at most %d.", capacityDefaultLimit, capacityMaxLimit))),
	), h.handleListCapacityResources)
}

// capacityResource is what the model reads of a resource of the view.
type capacityResource struct {
	Kind        string            `json:"kind"`
	Labels      map[string]string `json:"labels"`
	Ratio       float64           `json:"ratio"`
	Usage       float64           `json:"usage"`
	Limit       float64           `json:"limit"`
	TrendPerDay float64           `json:"trendPerDay"`
	DaysToFull  *float64          `json:"daysToFull,omitempty"`
	FullAt      string            `json:"fullAt,omitempty"`
	HorizonDays int               `json:"horizonDays,omitempty"`
	Scale       string            `json:"scale,omitempty"`
	Valid       bool              `json:"valid"`
	Reason      string            `json:"reason,omitempty"`
	Informative bool              `json:"informative,omitempty"`
}

func (h *Handler) handleListCapacityResources(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := req.Params.Arguments.(map[string]any)
	text := func(k string) string { v, _ := args[k].(string); return v }
	limit := capacityDefaultLimit
	if v := text("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return mcp.NewToolResultError(`Parameter validation failed: "limit" must be a positive integer`), nil
		}
		limit = min(n, capacityMaxLimit)
	}
	within := math.Inf(1)
	if v := text("withinDays"); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil || n <= 0 {
			return mcp.NewToolResultError(`Parameter validation failed: "withinDays" must be a positive number of days`), nil
		}
		within = n
	}
	query := url.Values{}
	if kind := text("kind"); kind != "" {
		known := false
		for _, k := range capacityKinds {
			known = known || k == kind
		}
		if !known {
			return mcp.NewToolResultError(fmt.Sprintf(`Parameter validation failed: "kind" must be one of %v`, capacityKinds)), nil
		}
		query.Set("kind", kind)
	}
	if search := text("search"); search != "" {
		query.Set("q", search)
	}

	client, err := h.GetClient(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := client.GetCapacityResources(ctx, query)
	if err != nil {
		h.logger.ErrorContext(ctx, "Capacity tool failed", slog.String("tool", "bylonis_list_capacity_resources"), logpkg.ErrAttr(err))
		return mcp.NewToolResultError(err.Error()), nil
	}
	var envelope struct {
		Data struct {
			Resources []capacityResource `json:"resources"`
			Kinds     []json.RawMessage  `json:"kinds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return mcp.NewToolResultError("failed to parse response: " + err.Error()), nil
	}
	out := make([]capacityResource, 0, limit)
	matched := 0
	for _, r := range envelope.Data.Resources {
		if !math.IsInf(within, 1) && (!r.Valid || r.Informative || r.DaysToFull == nil || *r.DaysToFull > within) {
			continue
		}
		matched++
		if len(out) < limit {
			out = append(out, r)
		}
	}
	return fleetResult(map[string]any{"resources": out, "total": matched, "kinds": envelope.Data.Kinds})
}
