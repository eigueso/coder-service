// WebSocket proxy from the browser to Coder's agent PTY endpoint,
// mirroring backend/app/terminal_proxy.py.
package api

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/malmonte/go-coder-service/internal/coder"
)

const maxTerminalFrame = 256 * 1024

func toWSBase(httpBase string) string {
	base := strings.TrimRight(httpBase, "/")
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://")
	}
	if strings.HasPrefix(base, "http://") {
		return "ws://" + strings.TrimPrefix(base, "http://")
	}
	return base
}

func hostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

type upstreamCandidate struct {
	label string
	url   string
}

// upstreamURLs builds upstream PTY candidates. When the API base is loopback
// but the dashboard is a public access URL (typical try.coder.app tunnel),
// prefer the dashboard: localhost often accepts the WebSocket upgrade but
// cannot reliably stream agent PTY data.
func upstreamURLs(apiBase, dashboard, agentID, query string) []upstreamCandidate {
	loopback := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	apiHost := hostOf(apiBase)
	dashHost := hostOf(dashboard)

	ordered := [][2]string{{"api", apiBase}, {"dashboard", dashboard}}
	if loopback[apiHost] && dashHost != "" && !loopback[dashHost] {
		ordered = [][2]string{{"dashboard", dashboard}, {"api", apiBase}}
	}

	var candidates []upstreamCandidate
	seen := map[string]bool{}
	for _, entry := range ordered {
		base := strings.TrimRight(entry[1], "/")
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		candidates = append(candidates, upstreamCandidate{
			label: entry[0],
			url:   toWSBase(base) + "/api/v2/workspaceagents/" + agentID + "/pty?" + query,
		})
	}
	return candidates
}

func (a *App) connectUpstream(
	ctx context.Context, candidates []upstreamCandidate, sessionToken, origin string,
) (*websocket.Conn, error) {
	var lastErr error
	for _, candidate := range candidates {
		dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		conn, _, err := websocket.Dial(dialCtx, candidate.url, &websocket.DialOptions{
			HTTPClient: a.HTTPClient,
			HTTPHeader: http.Header{
				"Coder-Session-Token": {sessionToken},
				"Origin":              {origin},
			},
			CompressionMode: websocket.CompressionDisabled,
		})
		cancel()
		if err != nil {
			lastErr = err
			log.Printf("terminal upstream %s failed: %v", candidate.label, err)
			continue
		}
		log.Printf("terminal upstream connected via %s", candidate.label)
		return conn, nil
	}
	return nil, lastErr
}

// handleTerminal proxies a browser WebSocket to Coder's agent PTY endpoint.
func (a *App) handleTerminal(c echo.Context) error {
	settings := a.Settings

	client, err := websocket.Accept(c.Response(), c.Request(), &websocket.AcceptOptions{
		InsecureSkipVerify: true, // origin is checked explicitly below, mirroring the Python close codes
	})
	if err != nil {
		return nil
	}
	defer client.Close(websocket.StatusInternalError, "terminal proxy closed")
	client.SetReadLimit(maxTerminalFrame)

	if settings.UsesSSOAccess() {
		client.Close(4404, "Use the Coder terminal URL in SSO mode")
		return nil
	}
	origin := strings.TrimRight(c.Request().Header.Get("Origin"), "/")
	if !settings.AllowedTerminalOrigins()[origin] {
		client.Close(4403, "Origin is not allowed")
		return nil
	}
	sessionToken := strings.TrimSpace(c.QueryParam("token"))
	if sessionToken == "" {
		sessionToken = strings.TrimSpace(c.Request().Header.Get("Coder-Session-Token"))
	}
	if sessionToken == "" {
		// UI extension: same-origin pages authenticate via the session cookie.
		if cookie, err := c.Cookie(SessionCookieName); err == nil {
			sessionToken = strings.TrimSpace(cookie.Value)
		}
	}
	if sessionToken == "" {
		client.Close(4401, "Missing session token")
		return nil
	}

	select {
	case a.TerminalSlots <- struct{}{}:
		defer func() { <-a.TerminalSlots }()
	default:
		client.Close(4429, "Terminal capacity reached")
		return nil
	}

	height := clampInt(c.QueryParam("height"), 48, 1, 500)
	width := clampInt(c.QueryParam("width"), 120, 1, 500)
	requestedAgentID := strings.TrimSpace(c.QueryParam("agent_id"))

	ctx := c.Request().Context()
	coderClient := a.Coder(sessionToken)

	dashboard := settings.CoderAPIBase()
	if settings.ConfiguredDashboardURL() != "" {
		dashboard = settings.ResolveDashboardURL("")
	} else if buildinfo, err := coderClient.BuildInfo(ctx); err == nil {
		dashboard = settings.ResolveDashboardURL(str(buildinfo, "dashboard_url"))
	} else {
		closeWithError(client, err)
		return nil
	}

	workspace, err := coderClient.GetWorkspace(ctx, c.Param("name"))
	if err != nil {
		closeWithError(client, err)
		return nil
	}
	if !coder.IsStarted(workspace) {
		client.Close(4000, "Workspace is not started")
		return nil
	}
	agent := coder.PickAgent(workspace)
	if agent == nil || str(agent, "id") == "" {
		client.Close(4001, "No agent found on workspace")
		return nil
	}
	actualAgentID := str(agent, "id")
	if requestedAgentID != "" {
		if _, err := uuid.Parse(requestedAgentID); err != nil {
			client.Close(websocket.StatusInternalError, "Invalid agent_id")
			return nil
		}
		if requestedAgentID != actualAgentID {
			client.Close(4403, "Agent does not belong to workspace")
			return nil
		}
	}
	if !coder.IsAgentReady(agent) {
		client.Close(4003, "Startup script still running")
		return nil
	}
	if status := str(agent, "status"); status != "" && status != "connected" {
		client.Close(4002, "Agent is "+status+", not connected")
		return nil
	}

	query := url.Values{
		"reconnect": {uuid.NewString()},
		"height":    {strconv.Itoa(height)},
		"width":     {strconv.Itoa(width)},
	}.Encode()
	candidates := upstreamURLs(settings.CoderAPIBase(), dashboard, actualAgentID, query)

	upstream, err := a.connectUpstream(ctx, candidates, sessionToken, dashboard)
	if err != nil {
		reason := err.Error()
		if len(reason) > 120 {
			reason = reason[:120]
		}
		client.Close(websocket.StatusInternalError, reason)
		return nil
	}
	upstream.SetReadLimit(maxTerminalFrame)
	defer upstream.Close(websocket.StatusNormalClosure, "")

	relay(ctx, client, upstream)
	return nil
}

// relay is a bidirectional byte pump between the browser and Coder's PTY.
func relay(ctx context.Context, client, upstream *websocket.Conn) {
	relayCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan struct{}, 2)
	pump := func(from, to *websocket.Conn) {
		defer func() { done <- struct{}{} }()
		for {
			_, data, err := from.Read(relayCtx)
			if err != nil {
				return
			}
			if err := to.Write(relayCtx, websocket.MessageBinary, data); err != nil {
				return
			}
		}
	}
	go pump(client, upstream)
	go pump(upstream, client)
	<-done
	cancel()
	<-done
	client.Close(websocket.StatusNormalClosure, "")
}

func closeWithError(client *websocket.Conn, err error) {
	reason := err.Error()
	if len(reason) > 120 {
		reason = reason[:120]
	}
	client.Close(websocket.StatusInternalError, reason)
}

func clampInt(raw string, fallback, min, max int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
