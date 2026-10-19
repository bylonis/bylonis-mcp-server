package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	expirable "github.com/hashicorp/golang-lru/v2/expirable"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
	"github.com/bylonis/bylonis-mcp-server/internal/config"
	otelpkg "github.com/bylonis/bylonis-mcp-server/pkg/otel"
	"github.com/bylonis/bylonis-mcp-server/pkg/util"
)

type Handler struct {
	logger        *slog.Logger
	clientCache   *expirable.LRU[string, *bylonisclient.ByLonis]
	configURL     string
	customHeaders map[string]string
	meters        *otelpkg.Meters

	// clientOverride, when non-nil, is returned by GetClient instead of
	// looking up the cache. This exists solely to support unit testing
	// with mock clients.
	clientOverride bylonisclient.Client
}

func (h *Handler) SetMeters(meters *otelpkg.Meters) {
	h.meters = meters
}

func NewHandler(log *slog.Logger, cfg *config.Config) *Handler {
	// Normalize the configured URL so that the URL comparison in GetClient
	// works reliably (e.g. https://example.com:443 == https://example.com).
	normalizedURL := cfg.URL
	if n, err := util.NormalizeByLonisURL(cfg.URL); err == nil {
		normalizedURL = n
	}

	return &Handler{
		logger:        log,
		clientCache:   expirable.NewLRU[string, *bylonisclient.ByLonis](cfg.ClientCacheSize, nil, cfg.ClientCacheTTL),
		configURL:     normalizedURL,
		customHeaders: cfg.CustomHeaders,
	}
}

// GetClient returns a cached ByLonis client for the tenant identified by
// the apiKey and bylonisURL stored in the request context.
// Both stdio and HTTP transports guarantee these values are present
// in the context before any tool handler is called.
func (h *Handler) GetClient(ctx context.Context) (bylonisclient.Client, error) {
	if h.clientOverride != nil {
		return h.clientOverride, nil
	}

	apiKey, _ := util.GetAPIKey(ctx)
	bylonisURL, _ := util.GetByLonisURL(ctx)
	authHeader, _ := util.GetAuthHeader(ctx)

	if apiKey == "" || bylonisURL == "" {
		return nil, fmt.Errorf("missing tenant credentials in context (API key or ByLonis URL)")
	}

	if authHeader == "" {
		authHeader = bylonisclient.APIKeyHeader
	}

	cacheKey := util.HashTenantKey(apiKey, bylonisURL)

	if cachedClient, ok := h.clientCache.Get(cacheKey); ok {
		return cachedClient, nil
	}

	// Only attach custom headers when the tenant URL matches the configured
	// BYLONIS_URL to prevent leaking proxy-auth credentials (e.g. Cloudflare
	// Access tokens) to arbitrary third-party hosts.
	var headers map[string]string
	if strings.EqualFold(bylonisURL, h.configURL) {
		headers = h.customHeaders
	}

	h.logger.DebugContext(ctx, "Creating new ByLonis client for tenant")
	newClient := bylonisclient.NewClient(h.logger, bylonisURL, apiKey, authHeader, headers)
	newClient.SetMeters(h.meters)
	h.clientCache.Add(cacheKey, newClient)
	return newClient, nil
}
