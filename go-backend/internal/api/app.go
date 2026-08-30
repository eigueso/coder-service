// Package api exposes the JSON API, mirroring backend/app/main.py.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/malmonte/go-backend/internal/coder"
	"github.com/malmonte/go-backend/internal/config"
)

// dashboardCacheTTL matches the Python backend's 5-minute buildinfo cache.
const dashboardCacheTTL = 5 * time.Minute

// App holds shared state, mirroring FastAPI's app.state.
type App struct {
	Settings *config.Settings
	// HTTPClient is the pooled client for Coder API calls (30s timeout).
	HTTPClient *http.Client
	// WSHTTPClient shares the transport but has no client timeout: a client
	// timeout would sever long-lived terminal WebSockets mid-session.
	WSHTTPClient *http.Client

	ready atomic.Bool

	dashboardMu        sync.Mutex
	dashboardCache     string
	dashboardExpiresAt time.Time

	// terminalSlots bounds concurrent terminal proxies per process.
	terminalSlots chan struct{}
}

// NewApp builds the shared application state from settings.
func NewApp(settings *config.Settings) *App {
	transport := &http.Transport{
		MaxConnsPerHost:     settings.CoderHTTPMaxConnections,
		MaxIdleConns:        settings.CoderHTTPMaxKeepaliveConnections,
		MaxIdleConnsPerHost: settings.CoderHTTPMaxKeepaliveConnections,
		IdleConnTimeout:     time.Duration(settings.CoderHTTPKeepaliveExpirySeconds * float64(time.Second)),
	}
	app := &App{
		Settings:      settings,
		HTTPClient:    &http.Client{Transport: transport, Timeout: 30 * time.Second},
		WSHTTPClient:  &http.Client{Transport: transport},
		terminalSlots: make(chan struct{}, settings.TerminalMaxConnectionsPerWorker),
	}
	app.ready.Store(true)
	return app
}

// SetReady toggles the readiness probe, mirroring app.state.ready.
func (a *App) SetReady(ready bool) { a.ready.Store(ready) }

// Ready reports whether the service accepts traffic.
func (a *App) Ready() bool { return a.ready.Load() }

// Coder builds a Coder API client bound to one session token.
func (a *App) Coder(sessionToken string) *coder.Client {
	return coder.NewClient(a.Settings, sessionToken, a.HTTPClient)
}

// DashboardURL uses coder_dashboard_url when set; otherwise discovers it via
// buildinfo with a 5-minute cache.
func (a *App) DashboardURL(ctx context.Context, client *coder.Client) (string, error) {
	if a.Settings.ConfiguredDashboardURL() != "" {
		return a.Settings.ResolveDashboardURL(""), nil
	}
	a.dashboardMu.Lock()
	defer a.dashboardMu.Unlock()
	if a.dashboardCache != "" && time.Now().Before(a.dashboardExpiresAt) {
		return a.dashboardCache, nil
	}
	buildinfo, err := client.BuildInfo(ctx)
	if err != nil {
		return "", err
	}
	dashboard := a.Settings.ResolveDashboardURL(str(buildinfo, "dashboard_url"))
	a.dashboardCache = dashboard
	a.dashboardExpiresAt = time.Now().Add(dashboardCacheTTL)
	return dashboard, nil
}

// sessionToken pulls the Coder session token from the request header.
func sessionToken(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("Coder-Session-Token"))
}

// requireSessionToken mirrors deps.require_session_token.
func requireSessionToken(r *http.Request) (string, error) {
	token := sessionToken(r)
	if token == "" {
		return "", coder.NewAPIError(http.StatusUnauthorized,
			"Missing Coder-Session-Token header. Authenticate via POST /auth first.")
	}
	return token, nil
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError renders errors the way FastAPI does: {"detail": "..."}.
func writeError(w http.ResponseWriter, err error) {
	var apiErr *coder.APIError
	if errors.As(err, &apiErr) {
		writeJSON(w, apiErr.Status, map[string]string{"detail": apiErr.Detail})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": "Internal server error"})
}
