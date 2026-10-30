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
