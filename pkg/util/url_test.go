package util

import "testing"

func TestNormalizeByLonisURLFormInput(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "adds https for protocol-less cloud host",
			raw:  "tough-gecko.us.bylonis.test",
			want: "https://tough-gecko.us.bylonis.test",
		},
		{
			name: "strips path from full URL",
			raw:  "https://tough-gecko.us.bylonis.test/home",
			want: "https://tough-gecko.us.bylonis.test",
		},
		{
			name: "adds https and strips path query and fragment",
			raw:  "tough-gecko.us.bylonis.test/home?orgId=123#logs",
			want: "https://tough-gecko.us.bylonis.test",
		},
		{
			name: "adds https when stripped query contains absolute URL",
			raw:  "tough-gecko.us.bylonis.test/home?next=https://example.com",
			want: "https://tough-gecko.us.bylonis.test",
		},
		{
			name: "preserves explicit http and custom port",
			raw:  "http://tenant.example.com:8080/dashboard",
			want: "http://tenant.example.com:8080",
		},
		{
			name: "strips default https port after path removal",
			raw:  "https://tenant.example.com:443/home",
			want: "https://tenant.example.com",
		},
		{
			name: "accepts protocol-relative URL",
			raw:  "//tenant.example.com/home",
			want: "https://tenant.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeByLonisURLFormInput(tt.raw)
			if err != nil {
				t.Fatalf("NormalizeByLonisURLFormInput(%q) error = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeByLonisURLFormInput(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNormalizeByLonisURLFormInputRejectsInvalidHosts(t *testing.T) {
	tests := []string{
		"",
		"ftp://tenant.example.com/home",
		"http:/tenant.example.com/home",
		"https:/tenant.example.com/home",
		"http:tenant.example.com/home",
		"https:tenant.example.com/home",
		"//http:tenant.example.com/home",
		"//https:tenant.example.com/home",
		"https://tough-gecko.us.bylonis.test@evil.example/home",
		"tough-gecko.us.bylonis.test@evil.example/home",
		"localhost/home",
		"0.0.0.0/home",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if got, err := NormalizeByLonisURLFormInput(raw); err == nil {
				t.Fatalf("NormalizeByLonisURLFormInput(%q) = %q, want error", raw, got)
			}
		})
	}
}

func TestNormalizeByLonisURLRejectsUserinfo(t *testing.T) {
	tests := []string{
		"https://tenant.example.com@evil.example",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if got, err := NormalizeByLonisURL(raw); err == nil {
				t.Fatalf("NormalizeByLonisURL(%q) = %q, want error", raw, got)
			}
		})
	}
}
