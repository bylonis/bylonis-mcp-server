package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
)

// The themes of the organization (bylonis#704, bylonis#705). The MCP has no logic of its own for them: it reads,
// generates, patches, validates and saves through the endpoints of ByLonis, which check the colors and the
// contrast. It never deletes a theme nor picks the theme of the organization.

// themeReads are the paths GetTheme reads.
var themeReads = []*regexp.Regexp{
	regexp.MustCompile(`^/api/v1/themes$`),
	regexp.MustCompile(`^/api/v1/themes/design-surface$`),
	regexp.MustCompile(`^/api/v1/themes/[0-9a-zA-Z-]+$`),
}

// themeWrites are the methods and paths SendTheme sends to: generation, patch and validation (they save nothing),
// and creating or updating a theme.
var themeWrites = map[string][]*regexp.Regexp{
	http.MethodPost: {
		regexp.MustCompile(`^/api/v1/themes$`),
		regexp.MustCompile(`^/api/v1/themes/(generate|patch|validate)$`),
	},
	http.MethodPut: {
		regexp.MustCompile(`^/api/v1/themes/[0-9a-zA-Z-]+$`),
	},
}

func matchesAny(patterns []*regexp.Regexp, path string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}
	return false
}

func (s *ByLonis) GetTheme(ctx context.Context, path string) (json.RawMessage, error) {
	if !matchesAny(themeReads, path) {
		return nil, fmt.Errorf("not a path of the themes: %s", path)
	}
	s.logger.DebugContext(s.ensureTenantContext(ctx), "Fetching themes from ByLonis", slog.String("path", path))
	return s.doRequest(ctx, http.MethodGet, s.baseURL+path, nil, DefaultQueryTimeout)
}

func (s *ByLonis) SendTheme(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	if !matchesAny(themeWrites[method], path) {
		return nil, fmt.Errorf("not a write of the themes: %s %s", method, path)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the body: %w", err)
	}
	s.logger.DebugContext(s.ensureTenantContext(ctx), "Sending a theme to ByLonis", slog.String("method", method), slog.String("path", path))
	return s.doRequest(ctx, method, s.baseURL+path, bytes.NewReader(raw), DefaultQueryTimeout)
}
