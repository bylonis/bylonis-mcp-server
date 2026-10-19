package config

import (
	"testing"

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

// The upstream SIGNOZ_ names were accepted as aliases until v1.0; they must
// stay ignored so a deployment still using them fails loudly.
func TestLoadConfigIgnoresSignozEnv(t *testing.T) {
	t.Setenv("BYLONIS_URL", "")
	t.Setenv("BYLONIS_API_KEY", "")
	t.Setenv("SIGNOZ_URL", "http://legacy:8080/")
	t.Setenv("SIGNOZ_API_KEY", "legacy-key")
	t.Setenv("SIGNOZ_CUSTOM_HEADERS", "X-Legacy:1")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.URL != "" || cfg.APIKey != "" || len(cfg.CustomHeaders) != 0 {
		t.Fatalf("SIGNOZ_ vars must be ignored: url=%q key set=%t headers=%v", cfg.URL, cfg.APIKey != "", cfg.CustomHeaders)
	}
}
