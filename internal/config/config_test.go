package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_CustomHeaders(t *testing.T) {
	tests := []struct {
		name            string
		envValue        string
		expectedHeaders map[string]string
	}{
		{
			name:            "empty env var produces empty map",
			envValue:        "",
			expectedHeaders: map[string]string{},
		},
		{
			name:     "single header pair",
			envValue: "X-Custom-Auth:my-token",
			expectedHeaders: map[string]string{
				"X-Custom-Auth": "my-token",
			},
		},
		{
			name:     "multiple header pairs",
			envValue: "CF-Access-Client-Id:abc123.access,CF-Access-Client-Secret:secret456",
			expectedHeaders: map[string]string{
				"CF-Access-Client-Id":     "abc123.access",
				"CF-Access-Client-Secret": "secret456",
			},
		},
		{
			name:     "whitespace is trimmed",
			envValue: " Key1 : Value1 , Key2 : Value2 ",
			expectedHeaders: map[string]string{
				"Key1": "Value1",
				"Key2": "Value2",
			},
		},
		{
			name:     "value containing colon is preserved",
			envValue: "Authorization:Bearer my-jwt-token:with:colons",
			expectedHeaders: map[string]string{
				"Authorization": "Bearer my-jwt-token:with:colons",
			},
		},
		{
			name:     "malformed entry without colon is skipped",
			envValue: "ValidKey:ValidValue,MalformedEntry",
			expectedHeaders: map[string]string{
				"ValidKey": "ValidValue",
			},
		},
		{
			name:     "empty header name is skipped",
			envValue: ":some-value,ValidKey:ValidValue",
			expectedHeaders: map[string]string{
				"ValidKey": "ValidValue",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BYLONIS_URL", "http://localhost:8080")
			t.Setenv("BYLONIS_API_KEY", "test-key")

			if tt.envValue != "" {
				t.Setenv("BYLONIS_CUSTOM_HEADERS", tt.envValue)
			}

			cfg, err := LoadConfig()
			require.NoError(t, err)
			assert.Equal(t, tt.expectedHeaders, cfg.CustomHeaders)
		})
	}
}

func TestValidateConfig_HTTPAllowsCredentialsFromHeaders(t *testing.T) {
	cfg := &Config{
		TransportMode: "http",
		Port:          "8000",
	}

	require.NoError(t, cfg.ValidateConfig())
}

func TestValidateConfig_StdioRequiresConfiguredCredentials(t *testing.T) {
	cfg := &Config{
		TransportMode: "stdio",
	}

	require.ErrorContains(t, cfg.ValidateConfig(), "BYLONIS_API_KEY is required")
}

func TestLoadConfigLegacySignozEnv(t *testing.T) {
	t.Setenv("SIGNOZ_URL", "http://legacy:8080/")
	t.Setenv("SIGNOZ_API_KEY", "legacy-key")
	t.Setenv("SIGNOZ_DOCS_REFRESH_INTERVAL", "2h")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.URL != "http://legacy:8080" || cfg.APIKey != "legacy-key" || cfg.DocsRefreshInterval != 2*time.Hour {
		t.Fatalf("legacy SIGNOZ_ vars not read: url=%q key=%q docs=%s", cfg.URL, cfg.APIKey, cfg.DocsRefreshInterval)
	}
}

func TestLoadConfigBylonisWinsOverSignoz(t *testing.T) {
	t.Setenv("SIGNOZ_URL", "http://legacy:8080")
	t.Setenv("BYLONIS_URL", "http://bylonis:8080")
	t.Setenv("SIGNOZ_API_KEY", "legacy-key")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.URL != "http://bylonis:8080" {
		t.Fatalf("BYLONIS_URL must win, got %q", cfg.URL)
	}
	if cfg.APIKey != "legacy-key" {
		t.Fatalf("SIGNOZ_API_KEY fallback when BYLONIS_API_KEY is unset, got %q", cfg.APIKey)
	}
}

func TestLookupEnvOnlyBylonisKeysFallBack(t *testing.T) {
	t.Setenv("SIGNOZ_LOG_LEVEL", "debug")
	if got := lookupEnv("LOG_LEVEL"); got != "" {
		t.Fatalf("non-BYLONIS_ keys must not fall back, got %q", got)
	}
}
