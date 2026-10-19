package util

import (
	"strings"
	"testing"
)

func TestInstanceURLAllowlistUnconfiguredAllowsAll(t *testing.T) {
	al := ParseInstanceURLAllowlist("")
	if al.Configured() {
		t.Fatalf("empty allowlist should not be configured")
	}
	for _, host := range []string{"demo.us.bylonis.test", "1.1.1.1", "bylonis.example.com", ""} {
		if !al.AllowsHost(host) {
			t.Errorf("unconfigured allowlist should allow %q", host)
		}
	}
	if !al.AllowsURL("https://anything.example.com") {
		t.Errorf("unconfigured allowlist should allow any URL")
	}
}

func TestInstanceURLAllowlistWildcard(t *testing.T) {
	al := ParseInstanceURLAllowlist("*.us.bylonis.test")
	if !al.Configured() {
		t.Fatalf("allowlist should be configured")
	}

	allowed := []string{
		"demo.us.bylonis.test",
		"DEMO.US.BYLONIS.TEST", // case-insensitive
		"tough-gecko.us.bylonis.test",
		"a.b.us.bylonis.test", // wildcard spans multiple labels
	}
	for _, host := range allowed {
		if !al.AllowsHost(host) {
			t.Errorf("expected %q to be allowed", host)
		}
	}

	denied := []string{
		"us.bylonis.test",               // apex is not matched by its own wildcard
		"evil.eu.bylonis.test",          // different region
		"notus.bylonis.test",            // no dot boundary before suffix
		"demo.us.bylonis.test.evil.com", // suffix not at the end
		"demo.us.bylonis.test.",         // trailing-dot FQDN fails closed
		"1.1.1.1",
		"",
	}
	for _, host := range denied {
		if al.AllowsHost(host) {
			t.Errorf("expected %q to be denied", host)
		}
	}
}

func TestInstanceURLAllowlistExactAndMultiple(t *testing.T) {
	al := ParseInstanceURLAllowlist("bylonis.example.com, *.eu.bylonis.test ,https://*.us.bylonis.test/")

	allowed := []string{
		"bylonis.example.com",
		"demo.eu.bylonis.test",
		"demo.us.bylonis.test", // scheme/path in the pattern is stripped
	}
	for _, host := range allowed {
		if !al.AllowsHost(host) {
			t.Errorf("expected %q to be allowed", host)
		}
	}

	denied := []string{
		"sub.bylonis.example.com", // exact entry does not cover subdomains
		"bylonis.example.org",
		"169.254.169.254", // SSRF target blocked once an allowlist is set
	}
	for _, host := range denied {
		if al.AllowsHost(host) {
			t.Errorf("expected %q to be denied", host)
		}
	}
}

func TestInstanceURLAllowlistStripsPortFromEntry(t *testing.T) {
	// A pasted full URL with scheme/port/path is reduced to the bare host so it
	// still matches (url.Hostname() never carries a port). Avoids a silent
	// all-403 footgun for operators who paste a full URL.
	if !ParseInstanceURLAllowlist("https://bylonis.example.com:8080/").AllowsHost("bylonis.example.com") {
		t.Errorf("entry with scheme/port/path should match the bare host")
	}
	if !ParseInstanceURLAllowlist("*.us.bylonis.test:443").AllowsHost("demo.us.bylonis.test") {
		t.Errorf("wildcard entry with a port should match a subdomain")
	}
	// Port on the checked URL is likewise ignored.
	if !ParseInstanceURLAllowlist("bylonis.example.com").AllowsURL("https://bylonis.example.com:8443") {
		t.Errorf("port on the checked URL should be ignored when matching a bare-host entry")
	}
}

func TestInstanceURLNotPermittedMessage(t *testing.T) {
	msg := InstanceURLNotPermittedMessage()
	if !strings.Contains(msg, "Settings → MCP Server") {
		t.Errorf("message should point to the MCP Server settings page, got: %s", msg)
	}
	if strings.Contains(strings.ToLower(msg), "signoz") {
		t.Errorf("message must not mention SigNoz, got: %s", msg)
	}
}

func TestInstanceURLAllowlistAllowsURL(t *testing.T) {
	al := ParseInstanceURLAllowlist("*.us.bylonis.test")

	if !al.AllowsURL("https://demo.us.bylonis.test") {
		t.Errorf("expected normalized cloud URL to be allowed")
	}
	if !al.AllowsURL("https://demo.us.bylonis.test:443") {
		t.Errorf("port should not affect host matching")
	}
	if al.AllowsURL("https://1.1.1.1") {
		t.Errorf("expected non-cloud URL to be denied")
	}
	if al.AllowsURL("://bad-url") {
		t.Errorf("malformed URL should be denied when allowlist is configured")
	}
}
