package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	logpkg "github.com/bylonis/bylonis-mcp-server/pkg/log"
)

func TestThemePathsOnly(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+string(body))
		_, _ = w.Write([]byte(`{"status":"success","data":{}}`))
	}))
	defer server.Close()
	client := NewClient(logpkg.New("error"), server.URL, "key", "BYLONIS-API-KEY", nil)
	ctx := context.Background()

	for _, path := range []string{"/api/v1/themes/bylonis/export", "/api/v1/themes/../dashboards", "/api/v1/dashboards"} {
		if _, err := client.GetTheme(ctx, path); err == nil {
			t.Errorf("GET %s: expected an error", path)
		}
	}
	for _, write := range [][2]string{
		{http.MethodPost, "/api/v1/themes/t1/org-default"}, {http.MethodPost, "/api/v1/themes/import"},
		{http.MethodDelete, "/api/v1/themes/t1"}, {http.MethodPut, "/api/v1/themes"}, {http.MethodPost, "/api/v1/preferences"},
	} {
		if _, err := client.SendTheme(ctx, write[0], write[1], map[string]any{}); err == nil {
			t.Errorf("%s %s: expected an error", write[0], write[1])
		}
	}
	if len(got) != 0 {
		t.Fatalf("nothing should reach the server, got %v", got)
	}

	for _, path := range []string{"/api/v1/themes", "/api/v1/themes/design-surface", "/api/v1/themes/bylonis"} {
		if _, err := client.GetTheme(ctx, path); err != nil {
			t.Errorf("GET %s: %v", path, err)
		}
	}
	for _, write := range [][2]string{
		{http.MethodPost, "/api/v1/themes"}, {http.MethodPost, "/api/v1/themes/generate"}, {http.MethodPost, "/api/v1/themes/patch"},
		{http.MethodPost, "/api/v1/themes/validate"}, {http.MethodPut, "/api/v1/themes/0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"},
	} {
		if _, err := client.SendTheme(ctx, write[0], write[1], map[string]any{"baseColor": "#3b82f6"}); err != nil {
			t.Errorf("%s %s: %v", write[0], write[1], err)
		}
	}
	if len(got) != 8 {
		t.Fatalf("expected 8 requests, got %v", got)
	}
	if got[4] != `POST /api/v1/themes/generate {"baseColor":"#3b82f6"}` {
		t.Errorf("unexpected request %q", got[4])
	}
}
