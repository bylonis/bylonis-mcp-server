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

// What ByLonis tells about the alerts, the changes and the logs (bylonis#1024): the rules that flap, who
// acknowledged the alerts, the change events, the maintenance windows, the log patterns and the replay of a rule.
// Only these paths, and nothing that changes ByLonis: acknowledging an alert and creating or removing a maintenance
// window have their own write methods and tools (bylonis#1052).

// readOnlyGets are the only paths GetReadOnly reads.
var readOnlyGets = []*regexp.Regexp{
	regexp.MustCompile(`^/api/v2/alert_quality/flapping$`),
	regexp.MustCompile(`^/api/v2/alert_acknowledgements$`),
	regexp.MustCompile(`^/api/v1/events$`),
	regexp.MustCompile(`^/api/v1/downtime_schedules$`),
}

// readOnlyPosts are the only paths PostReadOnly sends to: POSTs that save nothing. The patterns of the logs are
// a query, and the backtest replays a rule over its history without creating it or notifying.
var readOnlyPosts = []*regexp.Regexp{
	regexp.MustCompile(`^/api/v5/logs/patterns$`),
	regexp.MustCompile(`^/api/v1/rules/backtest$`),
}

func matchesPath(patterns []*regexp.Regexp, path string) bool {
	if strings.Contains(path, "..") {
		return false
	}
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}
	return false
}

// ErrNotInThisVersion is the error of an endpoint that the ByLonis of the tenant does not have yet. Its web app
// answers the paths the API does not know with its page, 200 and HTML, instead of a 404.
type ErrNotInThisVersion struct{ Path string }

func (e ErrNotInThisVersion) Error() string {
	return fmt.Sprintf("this ByLonis does not have %s yet: it is in a newer version of ByLonis than the one of this tenant", e.Path)
}

// notInThisVersion tells an answer of the web app (or a 404/405 of the router) from one of the API.
func notInThisVersion(path string, raw json.RawMessage, err error) (json.RawMessage, error) {
	if err != nil {
		message := err.Error()
		if strings.HasPrefix(message, "unexpected status 404: 404 page not found") || strings.HasPrefix(message, "unexpected status 405") {
			return nil, ErrNotInThisVersion{Path: path}
		}
		return nil, err
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '<' {
		return nil, ErrNotInThisVersion{Path: path}
	}
	return raw, nil
}

func (s *ByLonis) GetReadOnly(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	if !matchesPath(readOnlyGets, path) {
		return nil, fmt.Errorf("not a read-only path of the MCP: %s", path)
	}
	reqURL := s.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}
	s.logger.DebugContext(s.ensureTenantContext(ctx), "Reading from ByLonis", slog.String("path", path))
	raw, err := s.doRequest(ctx, http.MethodGet, reqURL, nil, DefaultQueryTimeout)
	return notInThisVersion(path, raw, err)
}

func (s *ByLonis) PostReadOnly(ctx context.Context, path string, query url.Values, body []byte) (json.RawMessage, error) {
	if !matchesPath(readOnlyPosts, path) {
		return nil, fmt.Errorf("not a read-only path of the MCP: %s", path)
	}
	reqURL := s.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}
	s.logger.DebugContext(s.ensureTenantContext(ctx), "Asking ByLonis", slog.String("path", path))
	// DefaultQueryTimeout is longer than the two minutes ByLonis gives the replay of a rule.
	raw, err := s.doRequest(ctx, http.MethodPost, reqURL, bytes.NewReader(body), DefaultQueryTimeout)
	return notInThisVersion(path, raw, err)
}
