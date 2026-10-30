package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
)

func TestGetFleetOnlyReadsTheFleet(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"status":"success","data":{}}`))
	}))
	defer server.Close()
	client := NewClient(logpkg.New("error"), server.URL, "key", "BYLONIS-API-KEY", nil)

	for _, path := range []string{"/api/v1/rules", "/api/v1/inventory/../users", "/api/v2/sessions", "/api/v1/inventoryx"} {
		if _, err := client.GetFleet(context.Background(), path, nil); err == nil {
			t.Errorf("%s: expected an error", path)
		}
	}
	if _, err := client.GetFleet(context.Background(), "/api/v1/inventory/nodes", url.Values{"q": {"compute n07"}}); err != nil {
		t.Fatal(err)
	}
	if got != "GET /api/v1/inventory/nodes?q=compute+n07" {
		t.Errorf("unexpected request %q", got)
	}
}

func TestPostFleetOnlySendsProposals(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		_, _ = w.Write([]byte(`{"status":"success","data":{}}`))
	}))
	defer server.Close()
	client := NewClient(logpkg.New("error"), server.URL, "key", "BYLONIS-API-KEY", nil)
	for _, path := range []string{
		"/api/v1/fleet/deployments", "/api/v1/fleet/ansible_runs/r1/approve", "/api/v1/fleet/fleets/f1",
		"/api/v1/fleet/playbooks/p1", "/api/v1/fleet/fleets/../policy/configs", "/api/v1/inventory/nodes",
	} {
		if _, err := client.PostFleet(context.Background(), path, map[string]any{}); err == nil {
			t.Errorf("%s: expected an error", path)
		}
	}
	for _, path := range []string{"/api/v1/fleet/fleets/f1/configs", "/api/v1/fleet/fleets/f1/configs/validate",
		"/api/v1/fleet/playbooks", "/api/v1/fleet/playbooks/p1/versions", "/api/v1/fleet/ansible_runs"} {
		if _, err := client.PostFleet(context.Background(), path, map[string]any{"a": 1}); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
	if len(got) != 5 {
		t.Errorf("expected 5 requests, got %v", got)
	}
}
