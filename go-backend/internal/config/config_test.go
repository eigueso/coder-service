package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFromEnvDefaults(t *testing.T) {
	s := FromEnv()

	if s.CoderURL != "http://localhost:3000" {
		t.Errorf("CoderURL = %q", s.CoderURL)
	}
	if s.CoderDashboardURL != "" || s.CoderSessionToken != "" {
		t.Errorf("expected empty dashboard/session token, got %q / %q",
			s.CoderDashboardURL, s.CoderSessionToken)
	}
	if s.CoderAccessAuthMode != "legacy_token" {
		t.Errorf("CoderAccessAuthMode = %q", s.CoderAccessAuthMode)
	}
	if s.CoderHTTPMaxConnections != 200 {
		t.Errorf("CoderHTTPMaxConnections = %d", s.CoderHTTPMaxConnections)
	}
	if s.CoderHTTPMaxKeepaliveConnections != 50 {
		t.Errorf("CoderHTTPMaxKeepaliveConnections = %d", s.CoderHTTPMaxKeepaliveConnections)
	}
	if s.CoderHTTPKeepaliveExpirySeconds != 30.0 {
		t.Errorf("CoderHTTPKeepaliveExpirySeconds = %f", s.CoderHTTPKeepaliveExpirySeconds)
	}
	if s.TerminalMaxConnectionsPerWorker != 1000 {
		t.Errorf("TerminalMaxConnectionsPerWorker = %d", s.TerminalMaxConnectionsPerWorker)
	}
	if s.VSCodeDesktopTokenLifetimeHours != 8 {
		t.Errorf("VSCodeDesktopTokenLifetimeHours = %d", s.VSCodeDesktopTokenLifetimeHours)
	}
	if s.ListenAddr != ":8000" {
		t.Errorf("ListenAddr = %q", s.ListenAddr)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("CODER_URL", "https://coder.example.com/")
	t.Setenv("coder_dashboard_url", "https://dash.example.com")
	t.Setenv("CODER_SESSION_TOKEN", "  owner-token  ")
	t.Setenv("CODER_ACCESS_AUTH_MODE", "SSO")
	t.Setenv("CODER_HTTP_MAX_CONNECTIONS", "10")
	t.Setenv("CODER_HTTP_KEEPALIVE_EXPIRY_SECONDS", "1.5")
	t.Setenv("LISTEN_ADDR", ":9999")

	s := FromEnv()

	if s.CoderAPIBase() != "https://coder.example.com" {
		t.Errorf("CoderAPIBase = %q", s.CoderAPIBase())
	}
	if s.ConfiguredDashboardURL() != "https://dash.example.com" {
		t.Errorf("ConfiguredDashboardURL = %q", s.ConfiguredDashboardURL())
	}
	if s.OwnerSessionToken() != "owner-token" {
		t.Errorf("OwnerSessionToken = %q", s.OwnerSessionToken())
	}
	if !s.UsesSSOAccess() {
		t.Error("UsesSSOAccess should be true for SSO (case-insensitive)")
	}
	if s.CoderHTTPMaxConnections != 10 {
		t.Errorf("CoderHTTPMaxConnections = %d", s.CoderHTTPMaxConnections)
	}
	if s.CoderHTTPKeepaliveExpirySeconds != 1.5 {
		t.Errorf("CoderHTTPKeepaliveExpirySeconds = %f", s.CoderHTTPKeepaliveExpirySeconds)
	}
	if s.ListenAddr != ":9999" {
		t.Errorf("ListenAddr = %q", s.ListenAddr)
	}
}

func TestFromEnvInvalidNumbersFallBack(t *testing.T) {
	t.Setenv("CODER_HTTP_MAX_CONNECTIONS", "not-a-number")
	t.Setenv("CODER_HTTP_KEEPALIVE_EXPIRY_SECONDS", "nope")

	s := FromEnv()

	if s.CoderHTTPMaxConnections != 200 {
		t.Errorf("CoderHTTPMaxConnections = %d, want default 200", s.CoderHTTPMaxConnections)
	}
	if s.CoderHTTPKeepaliveExpirySeconds != 30.0 {
		t.Errorf("CoderHTTPKeepaliveExpirySeconds = %f, want default 30", s.CoderHTTPKeepaliveExpirySeconds)
	}
}

func TestLoadReadsEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("coder_url=https://from-file.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	// godotenv only sets variables that are not already in the environment.
	if err := os.Unsetenv("coder_url"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODER_URL", "")
	if err := os.Unsetenv("CODER_URL"); err != nil {
		t.Fatal(err)
	}

	s := Load()

	if s.CoderURL != "https://from-file.example.com" {
		t.Errorf("CoderURL = %q, want value from .env file", s.CoderURL)
	}
}

func TestUsesSSOAccessFalseForLegacy(t *testing.T) {
	s := &Settings{CoderAccessAuthMode: "legacy_token"}
	if s.UsesSSOAccess() {
		t.Error("legacy_token must not enable SSO access")
	}
}

func TestAllowedTerminalOrigins(t *testing.T) {
	s := &Settings{TerminalAllowedOrigins: " http://a.test/ , http://b.test ,, "}
	got := s.AllowedTerminalOrigins()
	want := map[string]bool{"http://a.test": true, "http://b.test": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllowedTerminalOrigins = %v, want %v", got, want)
	}
}

func TestAllowedCORSOrigins(t *testing.T) {
	s := &Settings{CORSAllowedOrigins: "http://a.test/,  ,http://b.test"}
	got := s.AllowedCORSOrigins()
	want := []string{"http://a.test", "http://b.test"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllowedCORSOrigins = %v, want %v", got, want)
	}
}

func TestResolveDashboardURL(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		buildinfo  string
		want       string
	}{
		{"prefers configured", "https://dash.test/", "https://build.test", "https://dash.test"},
		{"falls back to buildinfo", "", "https://build.test/", "https://build.test"},
		{"falls back to api base", "", "", "https://api.test"},
		{"ignores whitespace buildinfo", "", "   ", "https://api.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Settings{CoderURL: "https://api.test/", CoderDashboardURL: tt.configured}
			if got := s.ResolveDashboardURL(tt.buildinfo); got != tt.want {
				t.Errorf("ResolveDashboardURL = %q, want %q", got, tt.want)
			}
		})
	}
}
