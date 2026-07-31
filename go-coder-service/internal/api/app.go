package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/malmonte/go-coder-service/internal/coder"
	"github.com/malmonte/go-coder-service/internal/config"
)

// SessionCookieName is used by the server-rendered UI; the JSON API keeps the
// Coder-Session-Token header for 1:1 compatibility with the Python backend.
const SessionCookieName = "coder_session_token"

// App holds shared state, mirroring FastAPI's app.state.
type App struct {
	Settings   *config.Settings
	HTTPClient *http.Client

	dashboardMu        sync.Mutex
	dashboardCache     string
	dashboardExpiresAt time.Time

	TerminalSlots chan struct{}
}

func NewApp(settings *config.Settings) *App {
	transport := &http.Transport{
		MaxConnsPerHost:     settings.CoderHTTPMaxConnections,
		MaxIdleConns:        settings.CoderHTTPMaxKeepaliveConnections,
		MaxIdleConnsPerHost: settings.CoderHTTPMaxKeepaliveConnections,
		IdleConnTimeout:     time.Duration(settings.CoderHTTPKeepaliveExpirySeconds * float64(time.Second)),
	}
	return &App{
		Settings:      settings,
		HTTPClient:    &http.Client{Transport: transport, Timeout: 30 * time.Second},
		TerminalSlots: make(chan struct{}, settings.TerminalMaxConnectionsPerWorker),
	}
}

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
	a.dashboardExpiresAt = time.Now().Add(5 * time.Minute)
	return dashboard, nil
}

// SessionToken pulls the Coder session token from the request: the
// Coder-Session-Token header first (JSON API contract), then the UI cookie.
func SessionToken(c echo.Context) string {
	if token := strings.TrimSpace(c.Request().Header.Get("Coder-Session-Token")); token != "" {
		return token
	}
	if cookie, err := c.Cookie(SessionCookieName); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

// RequireSessionToken mirrors deps.require_session_token.
func RequireSessionToken(c echo.Context) (string, error) {
	token := SessionToken(c)
	if token == "" {
		return "", coder.NewAPIError(http.StatusUnauthorized,
			"Missing Coder-Session-Token header. Authenticate via POST /auth first.")
	}
	return token, nil
}

// WriteError renders errors the way FastAPI does: {"detail": "..."}.
func WriteError(c echo.Context, err error) error {
	var apiErr *coder.APIError
	if errors.As(err, &apiErr) {
		return c.JSON(apiErr.Status, map[string]string{"detail": apiErr.Detail})
	}
	return c.JSON(http.StatusInternalServerError, map[string]string{"detail": "Internal server error"})
}
