// Package config loads runtime settings from environment variables and the
// shared coder-service/.env file, mirroring backend/app/config.py.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Settings holds runtime configuration for the backend. Field defaults and
// environment variable names match the Python backend's Settings model.
type Settings struct {
	// CoderURL is the base URL of the Coder API (no trailing slash).
	CoderURL string
	// CoderDashboardURL is the public Coder dashboard / access URL. When set,
	// it overrides the dashboard_url from GET /api/v2/buildinfo.
	CoderDashboardURL string
	// CoderSessionToken is the Owner/admin API token used by POST /auth to
	// mint per-user tokens (SSO simulation).
	CoderSessionToken string
	// CoderAccessAuthMode selects workspace access authentication:
	// "legacy_token" keeps the local demo token flow; "sso" uses Coder's
	// browser SSO session and never places tokens in access URLs.
	CoderAccessAuthMode              string
	CoderHTTPMaxConnections          int
	CoderHTTPMaxKeepaliveConnections int
	CoderHTTPKeepaliveExpirySeconds  float64
	TerminalMaxConnectionsPerWorker  int
	// TerminalAllowedOrigins is a comma-separated list of browser origins
	// allowed to open the legacy terminal proxy.
	TerminalAllowedOrigins          string
	VSCodeDesktopTokenLifetimeHours int
	// CORSAllowedOrigins is a comma-separated list of origins allowed to call
	// the API.
	CORSAllowedOrigins string
	// ListenAddr is the address the HTTP server binds to. The default matches
	// the Python backend's uvicorn port.
	ListenAddr string
}

// resolveEnvFile finds coder-service/.env from the usual layouts or the cwd.
func resolveEnvFile() string {
	var candidates []string
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, ".env"),
			filepath.Join(cwd, "..", ".env"),
		)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, ".env"),
			filepath.Join(dir, "..", ".env"),
		)
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// env looks up key case-insensitively (lowercase first, then uppercase) so the
// same .env file works for both the Python and Go backends.
func env(key, fallback string) string {
	for _, k := range []string{key, strings.ToUpper(key)} {
		if value, ok := os.LookupEnv(k); ok {
			return value
		}
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if raw := strings.TrimSpace(env(key, "")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			return parsed
		}
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if raw := strings.TrimSpace(env(key, "")); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

// Load reads settings from the .env file (if found) and the process
// environment. Process environment variables take precedence over the file.
func Load() *Settings {
	if path := resolveEnvFile(); path != "" {
		_ = godotenv.Load(path)
	}
	return FromEnv()
}

// FromEnv builds Settings from the current process environment only.
func FromEnv() *Settings {
	return &Settings{
		CoderURL:                         env("coder_url", "http://localhost:3000"),
		CoderDashboardURL:                env("coder_dashboard_url", ""),
		CoderSessionToken:                env("coder_session_token", ""),
		CoderAccessAuthMode:              env("coder_access_auth_mode", "legacy_token"),
		CoderHTTPMaxConnections:          envInt("coder_http_max_connections", 200),
		CoderHTTPMaxKeepaliveConnections: envInt("coder_http_max_keepalive_connections", 50),
		CoderHTTPKeepaliveExpirySeconds:  envFloat("coder_http_keepalive_expiry_seconds", 30.0),
		TerminalMaxConnectionsPerWorker:  envInt("terminal_max_connections_per_worker", 1000),
		TerminalAllowedOrigins: env("terminal_allowed_origins",
			"http://localhost:5173,http://127.0.0.1:5173"),
		VSCodeDesktopTokenLifetimeHours: envInt("vscode_desktop_token_lifetime_hours", 8),
		CORSAllowedOrigins: env("cors_allowed_origins",
			"http://localhost:5173,http://127.0.0.1:5173"),
		ListenAddr: env("listen_addr", ":8000"),
	}
}

// CoderAPIBase returns the Coder API base URL without a trailing slash.
func (s *Settings) CoderAPIBase() string {
	return strings.TrimRight(s.CoderURL, "/")
}

// OwnerSessionToken returns the configured owner token, trimmed. Empty means
// POST /auth cannot mint tokens.
func (s *Settings) OwnerSessionToken() string {
	return strings.TrimSpace(s.CoderSessionToken)
}

// UsesSSOAccess reports whether workspace access uses Coder's browser SSO
// session instead of the legacy token flow.
func (s *Settings) UsesSSOAccess() bool {
	return strings.EqualFold(strings.TrimSpace(s.CoderAccessAuthMode), "sso")
}

func splitOrigins(raw string) []string {
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimRight(strings.TrimSpace(part), "/")
		if part != "" {
			origins = append(origins, part)
		}
	}
	return origins
}

// AllowedTerminalOrigins returns the set of origins allowed to open the
// terminal proxy WebSocket.
func (s *Settings) AllowedTerminalOrigins() map[string]bool {
	allowed := make(map[string]bool)
	for _, origin := range splitOrigins(s.TerminalAllowedOrigins) {
		allowed[origin] = true
	}
	return allowed
}

// AllowedCORSOrigins returns the list of origins allowed to call the API.
func (s *Settings) AllowedCORSOrigins() []string {
	return splitOrigins(s.CORSAllowedOrigins)
}

// ConfiguredDashboardURL returns the explicitly configured dashboard URL, or
// empty when discovery via buildinfo should be used.
func (s *Settings) ConfiguredDashboardURL() string {
	return strings.TrimRight(strings.TrimSpace(s.CoderDashboardURL), "/")
}

// ResolveDashboardURL prefers explicit config, then Coder buildinfo, then the
// API base URL.
func (s *Settings) ResolveDashboardURL(buildinfoDashboard string) string {
	if configured := s.ConfiguredDashboardURL(); configured != "" {
		return configured
	}
	if discovered := strings.TrimRight(strings.TrimSpace(buildinfoDashboard), "/"); discovered != "" {
		return discovered
	}
	return s.CoderAPIBase()
}
