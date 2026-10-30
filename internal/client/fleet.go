package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// GetFleet reads the inventory and Fleet Control of ByLonis (bylonis#463). Only GETs under /api/v1/inventory/ and
// /api/v1/fleet/: the tools of the fleet are read only, and the API never returns the secrets of the credentials.
func (s *ByLonis) GetFleet(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	if !strings.HasPrefix(path, "/api/v1/inventory/") && !strings.HasPrefix(path, "/api/v1/fleet/") || strings.Contains(path, "..") {
		return nil, fmt.Errorf("not a path of the fleet: %s", path)
	}
	reqURL := s.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}
	s.logger.DebugContext(s.ensureTenantContext(ctx), "Fetching fleet data from ByLonis", slog.String("path", path))
	return s.doRequest(ctx, http.MethodGet, reqURL, nil, DefaultQueryTimeout)
}
