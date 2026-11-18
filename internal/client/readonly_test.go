package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
)

func TestReadOnlyOnlyUsesItsPaths(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"status":"success","data":{}}`))
	}))
	defer server.Close()
	client := NewClient(logpkg.New("error"), server.URL, "key", "BYLONIS-API-KEY", nil)
	ctx := context.Background()

	// Acknowledging, creating a maintenance window or a rule: never.
	for _, path := range []string{"/api/v2/alert_acknowledgements/a1", "/api/v1/downtime_schedules/d1", "/api/v2/rules",
		"/api/v1/events/../rules", "/api/v1/eventsx"} {
		if _, err := client.GetReadOnly(ctx, path, nil); err == nil {
			t.Errorf("GET %s: expected an error", path)
		}
	}
	for _, path := range []string{"/api/v2/alert_acknowledgements", "/api/v1/downtime_schedules", "/api/v1/events",
		"/api/v2/rules", "/api/v1/rules/backtest/../x"} {
		if _, err := client.PostReadOnly(ctx, path, nil, []byte(`{}`)); err == nil {
			t.Errorf("POST %s: expected an error", path)
		}
	}
	if len(got) != 0 {
		t.Fatalf("nothing should have been sent: %v", got)
	}

	for _, path := range []string{"/api/v2/alert_quality/flapping", "/api/v2/alert_acknowledgements", "/api/v1/events", "/api/v1/downtime_schedules"} {
		if _, err := client.GetReadOnly(ctx, path, url.Values{"days": {"7"}}); err != nil {
			t.Errorf("GET %s: %v", path, err)
		}
	}
	for _, path := range []string{"/api/v5/logs/patterns", "/api/v1/rules/backtest"} {
		if _, err := client.PostReadOnly(ctx, path, url.Values{"days": {"7"}}, []byte(`{}`)); err != nil {
			t.Errorf("POST %s: %v", path, err)
		}
	}
	if len(got) != 6 || got[0] != "GET /api/v2/alert_quality/flapping?days=7" || got[5] != "POST /api/v1/rules/backtest?days=7" {
		t.Errorf("unexpected requests %v", got)
	}
}

// An older ByLonis does not know the path, and its web app answers with its page.
func TestReadOnlyNotInThisVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<!doctype html><html><head><title>ByLonis</title></head></html>"))
	}))
	defer server.Close()
	client := NewClient(logpkg.New("error"), server.URL, "key", "BYLONIS-API-KEY", nil)

	_, err := client.GetReadOnly(context.Background(), "/api/v1/events", url.Values{"start": {"1"}, "end": {"2"}})
	var notThere ErrNotInThisVersion
	if !errors.As(err, &notThere) || notThere.Path != "/api/v1/events" {
		t.Errorf("expected ErrNotInThisVersion, got %v", err)
	}
	_, err = client.PostReadOnly(context.Background(), "/api/v5/logs/patterns", nil, []byte(`{}`))
	if !errors.As(err, &notThere) {
		t.Errorf("expected ErrNotInThisVersion, got %v", err)
	}
}
