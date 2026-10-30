package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
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

// fleetWrites are the only paths PostFleet sends to: proposals, never what runs them (bylonis#459).
var fleetWrites = []*regexp.Regexp{
	regexp.MustCompile(`^/api/v1/fleet/fleets/[0-9a-zA-Z-]+/configs/validate$`),
	regexp.MustCompile(`^/api/v1/fleet/fleets/[0-9a-zA-Z-]+/configs$`),
	regexp.MustCompile(`^/api/v1/fleet/playbooks$`),
	regexp.MustCompile(`^/api/v1/fleet/playbooks/[0-9a-zA-Z-]+/versions$`),
	regexp.MustCompile(`^/api/v1/fleet/ansible_runs$`),
}

func (s *ByLonis) PostFleet(ctx context.Context, path string, body any) (json.RawMessage, error) {
	allowed := false
	for _, pattern := range fleetWrites {
		allowed = allowed || pattern.MatchString(path)
	}
	if !allowed {
		return nil, fmt.Errorf("not a path of the proposals of the fleet: %s", path)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the body: %w", err)
	}
	s.logger.DebugContext(s.ensureTenantContext(ctx), "Sending a fleet proposal to ByLonis", slog.String("path", path))
	return s.doRequest(ctx, http.MethodPost, s.baseURL+path, bytes.NewReader(raw), DefaultQueryTimeout)
}
