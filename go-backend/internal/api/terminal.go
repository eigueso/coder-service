// WebSocket proxy from the browser to Coder's agent PTY endpoint, mirroring
// backend/app/terminal_proxy.py, including its custom close codes:
//
//	4404 SSO mode, 4403 origin/agent mismatch, 4401 missing token,
//	4429 capacity, 4000 not started, 4001 no agent, 4002 agent not
//	connected, 4003 startup script still running, 1011 internal errors.
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

	"github.com/malmonte/go-backend/internal/coder"
)

const (
	maxTerminalFrame   = 256 * 1024
	upstreamDialTimout = 10 * time.Second
	closeReasonLimit   = 120
)

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
		dialCtx, cancel := context.WithTimeout(ctx, upstreamDialTimout)
		conn, resp, err := websocket.Dial(dialCtx, candidate.url, &websocket.DialOptions{
			HTTPClient: a.WSHTTPClient,
			HTTPHeader: http.Header{
				"Coder-Session-Token": {sessionToken},
				"Origin":              {origin},
			},
			CompressionMode: websocket.CompressionDisabled,
		})
		cancel()
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
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
//
// The handshake is accepted first so close codes reach the browser; the
// Python backend behaves the same way through Starlette.
func (a *App) handleTerminal(w http.ResponseWriter, r *http.Request) {
	settings := a.Settings

	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The origin is checked explicitly below so disallowed origins get
		// the same 4403 close code as the Python backend.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer client.Close(websocket.StatusInternalError, "terminal proxy closed")
	client.SetReadLimit(maxTerminalFrame)

	if settings.UsesSSOAccess() {
		client.Close(4404, "Use the Coder terminal URL in SSO mode")
		return
	}
	origin := strings.TrimRight(r.Header.Get("Origin"), "/")
	if !settings.AllowedTerminalOrigins()[origin] {
		client.Close(4403, "Origin is not allowed")
		return
	}
	sessionToken := strings.TrimSpace(r.URL.Query().Get("token"))
	if sessionToken == "" {
		sessionToken = strings.TrimSpace(r.Header.Get("Coder-Session-Token"))
	}
	if sessionToken == "" {
		client.Close(4401, "Missing session token")
		return
	}

	select {
	case a.terminalSlots <- struct{}{}:
		defer func() { <-a.terminalSlots }()
	default:
		client.Close(4429, "Terminal capacity reached")
		return
	}

	height := clampInt(r.URL.Query().Get("height"), 48, 1, 500)
	width := clampInt(r.URL.Query().Get("width"), 120, 1, 500)
	requestedAgentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))

	ctx := r.Context()
	coderClient := a.Coder(sessionToken)

	dashboard := settings.CoderAPIBase()
	if settings.ConfiguredDashboardURL() != "" {
		dashboard = settings.ResolveDashboardURL("")
	} else if buildinfo, err := coderClient.BuildInfo(ctx); err == nil {
		dashboard = settings.ResolveDashboardURL(str(buildinfo, "dashboard_url"))
	} else {
		closeWithError(client, err)
		return
	}

	workspace, err := coderClient.GetWorkspace(ctx, r.PathValue("name"))
	if err != nil {
		closeWithError(client, err)
		return
	}
	if !coder.IsStarted(workspace) {
		client.Close(4000, "Workspace is not started")
		return
	}
	agent := coder.PickAgent(workspace)
	if agent == nil || str(agent, "id") == "" {
		client.Close(4001, "No agent found on workspace")
		return
	}
	actualAgentID := str(agent, "id")
	if requestedAgentID != "" {
		if _, err := uuid.Parse(requestedAgentID); err != nil {
			client.Close(websocket.StatusInternalError, "Invalid agent_id")
			return
		}
		if requestedAgentID != actualAgentID {
			client.Close(4403, "Agent does not belong to workspace")
			return
		}
	}
	if !coder.IsAgentReady(agent) {
		client.Close(4003, "Startup script still running")
		return
	}
	if status := str(agent, "status"); status != "" && status != "connected" {
		client.Close(4002, "Agent is "+status+", not connected")
		return
	}

	query := url.Values{
		"reconnect": {uuid.NewString()},
		"height":    {strconv.Itoa(height)},
		"width":     {strconv.Itoa(width)},
	}.Encode()
	candidates := upstreamURLs(settings.CoderAPIBase(), dashboard, actualAgentID, query)

	upstream, err := a.connectUpstream(ctx, candidates, sessionToken, dashboard)
	if err != nil {
		closeWithError(client, err)
		return
	}
	upstream.SetReadLimit(maxTerminalFrame)
	defer upstream.Close(websocket.StatusNormalClosure, "")

	relay(ctx, client, upstream)
}

// relay is a bidirectional byte pump between the browser and Coder's PTY.
// It returns when either side closes, then shuts the other side down.
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
	if len(reason) > closeReasonLimit {
		reason = reason[:closeReasonLimit]
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
