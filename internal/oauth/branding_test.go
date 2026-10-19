package oauth

import (
	"bytes"
	"strings"
	"testing"
)

// The authorization page is what a person sees when connecting an MCP client
// through OAuth: it must not show SigNoz's name, logo or links.
func TestAuthorizePageDoesNotMentionSigNoz(t *testing.T) {
	var buf bytes.Buffer
	data := authorizeTemplateData{
		ClientName:   "Example client",
		ByLonisURL:   "https://houston.bylonis.ai",
		ErrorMessage: "Enter your ByLonis API key to continue.",
	}
	if err := authorizePageTemplate.Execute(&buf, data); err != nil {
		t.Fatalf("render authorize page: %v", err)
	}
	page := buf.String()
	if strings.Contains(strings.ToLower(page), "signoz") {
		t.Fatalf("authorize page mentions SigNoz:\n%s", page)
	}
	for _, want := range []string{"ByLonis", `name="bylonis_url"`} {
		if !strings.Contains(page, want) {
			t.Errorf("authorize page should contain %q", want)
		}
	}
}
