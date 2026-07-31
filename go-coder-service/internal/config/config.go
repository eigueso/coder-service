// Package config mirrors backend/app/config.py: runtime settings loaded from
// the shared coder-service/.env file plus process environment variables.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Settings struct {
	CoderURL                         string
	CoderDashboardURL                string
	CoderSessionToken                string
	CoderAccessAuthMode              string
	CoderHTTPMaxConnections          int
	CoderHTTPMaxKeepaliveConnections int
	CoderHTTPKeepaliveExpirySeconds  float64
	TerminalMaxConnectionsPerWorker  int
	TerminalAllowedOrigins           string
	VSCodeDesktopTokenLifetimeHours  int
	CORSAllowedOrigins               string

	// Go-port additions (the SPA read these from Vite env vars).
	ListenAddr          string
	DefaultTemplateID   string
	DefaultTemplateName string
}

// resolveEnvFile finds coder-service/.env from the usual layouts or the cwd.
func resolveEnvFile() string {
	cwd, _ := os.Getwd()
	candidates := []string{
		filepath.Join(cwd, ".env"),
		filepath.Join(cwd, "..", ".env"),
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

func env(key, fallback string) string {
	for _, k := range []string{key, strings.ToUpper(key)} {
		if value, ok := os.LookupEnv(k); ok {
			return value
		}
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if raw := env(key, ""); raw != "" {
		if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			return parsed
		}
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if raw := env(key, ""); raw != "" {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func Load() *Settings {
	if path := resolveEnvFile(); path != "" {
		_ = godotenv.Load(path)
	}
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
			"http://localhost:5173,http://127.0.0.1:5173,http://localhost:8080,http://127.0.0.1:8080"),
		VSCodeDesktopTokenLifetimeHours: envInt("vscode_desktop_token_lifetime_hours", 8),
		CORSAllowedOrigins: env("cors_allowed_origins",
			"http://localhost:5173,http://127.0.0.1:5173,http://localhost:8080,http://127.0.0.1:8080"),
		ListenAddr:          env("listen_addr", ":8080"),
		DefaultTemplateID:   env("default_template_id", "93c85ebe-899b-4adb-8f68-d01ed67ca304"),
		DefaultTemplateName: env("default_template_name", "Kubernetes workspace"),
	}
}

func (s *Settings) CoderAPIBase() string {
	return strings.TrimRight(s.CoderURL, "/")
}

func (s *Settings) OwnerSessionToken() string {
	return strings.TrimSpace(s.CoderSessionToken)
}

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

func (s *Settings) AllowedTerminalOrigins() map[string]bool {
	allowed := map[string]bool{}
	for _, origin := range splitOrigins(s.TerminalAllowedOrigins) {
		allowed[origin] = true
	}
	return allowed
}

func (s *Settings) AllowedCORSOrigins() []string {
	return splitOrigins(s.CORSAllowedOrigins)
}

func (s *Settings) ConfiguredDashboardURL() string {
	return strings.TrimRight(strings.TrimSpace(s.CoderDashboardURL), "/")
}

// ResolveDashboardURL prefers explicit config, then Coder buildinfo, then the API base.
func (s *Settings) ResolveDashboardURL(buildinfoDashboard string) string {
	if configured := s.ConfiguredDashboardURL(); configured != "" {
		return configured
	}
	if discovered := strings.TrimRight(strings.TrimSpace(buildinfoDashboard), "/"); discovered != "" {
		return discovered
	}
	return s.CoderAPIBase()
}
