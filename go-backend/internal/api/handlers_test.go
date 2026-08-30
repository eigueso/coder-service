package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/malmonte/go-backend/internal/config"
)

const validBuildUUID = "0a68e15c-89ae-4c6c-9a41-3e6f571f9e5f"

// coderStub is a configurable fake for Coder's API v2.
type coderStub struct {
	me             map[string]any
	buildinfo      map[string]any
	workspace      map[string]any
	workspaceList  map[string]any
	build          map[string]any
	buildLogs      []any
	buildLogsText  string
	agentLogs      []any
	users          []any
	tokenKey       string
	buildinfoCalls atomic.Int32
}

func newCoderStub() *coderStub {
	return &coderStub{
		me:        map[string]any{"id": "u1", "username": "alice", "email": "alice@example.com"},
		buildinfo: map[string]any{"dashboard_url": "http://dash.test"},
		workspace: readyWorkspacePayload(),
		build: map[string]any{
			"id": validBuildUUID, "workspace_id": "ws-1", "build_number": float64(4),
			"transition": "delete", "job": map[string]any{"status": "pending"},
		},
		buildLogs:     []any{map[string]any{"id": float64(1), "output": "applying", "stage": "apply"}},
		buildLogsText: "plain logs",
		agentLogs:     []any{map[string]any{"id": float64(2), "output": "startup done", "level": "info"}},
		users:         []any{map[string]any{"id": "u1", "email": "alice@example.com"}},
		tokenKey:      "minted-token",
	}
}

func (s *coderStub) handler(t *testing.T) http.Handler {
	t.Helper()
	writeAny := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/api/v2/users/me":
			writeAny(w, s.me)
		case path == "/api/v2/buildinfo":
			s.buildinfoCalls.Add(1)
			writeAny(w, s.buildinfo)
		case path == "/api/v2/workspaces" && r.Method == http.MethodGet:
			list := s.workspaceList
			if list == nil {
				list = map[string]any{"count": float64(1), "workspaces": []any{s.workspace}}
			}
			writeAny(w, list)
		case path == "/api/v2/users/me/workspaces" && r.Method == http.MethodPost:
			writeAny(w, s.workspace)
		case strings.HasPrefix(path, "/api/v2/users/me/workspace/"):
			writeAny(w, s.workspace)
		case strings.HasPrefix(path, "/api/v2/workspaces/") && strings.HasSuffix(path, "/builds"):
			writeAny(w, s.build)
		case strings.HasSuffix(path, "/logs") && strings.HasPrefix(path, "/api/v2/workspacebuilds/"):
			if r.URL.Query().Get("format") == "text" {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte(s.buildLogsText))
				return
			}
			writeAny(w, s.buildLogs)
		case strings.HasPrefix(path, "/api/v2/workspacebuilds/"):
			writeAny(w, s.build)
		case strings.HasPrefix(path, "/api/v2/workspaceagents/") && strings.HasSuffix(path, "/logs"):
			writeAny(w, s.agentLogs)
		case path == "/api/v2/users" && r.Method == http.MethodGet:
			writeAny(w, map[string]any{"users": s.users})
		case strings.HasSuffix(path, "/keys/tokens") && r.Method == http.MethodPost:
			writeAny(w, map[string]any{"key": s.tokenKey})
		default:
			t.Logf("coder stub: unexpected %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
			writeAny(w, map[string]any{"message": "not found"})
		}
	})
}

type testEnv struct {
	app    *App
	server *httptest.Server
	stub   *coderStub
}

// newTestEnv wires an App to a stub Coder server and serves the full handler.
func newTestEnv(t *testing.T, configure func(*config.Settings, *coderStub)) *testEnv {
	t.Helper()
	stub := newCoderStub()
	coderServer := httptest.NewServer(stub.handler(t))
	t.Cleanup(coderServer.Close)

	settings := &config.Settings{
		CoderURL:                         coderServer.URL,
		CoderSessionToken:                "owner-token",
		CoderAccessAuthMode:              "legacy_token",
		CoderHTTPMaxConnections:          10,
		CoderHTTPMaxKeepaliveConnections: 10,
		CoderHTTPKeepaliveExpirySeconds:  30,
		TerminalMaxConnectionsPerWorker:  4,
		TerminalAllowedOrigins:           "http://ui.test",
		VSCodeDesktopTokenLifetimeHours:  8,
		CORSAllowedOrigins:               "http://ui.test",
		ListenAddr:                       ":0",
	}
	if configure != nil {
		configure(settings, stub)
	}
	app := NewApp(settings)
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	return &testEnv{app: app, server: server, stub: stub}
}

func (e *testEnv) request(t *testing.T, method, path string, body string, headers map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *testEnv) authed(t *testing.T, method, path, body string) *http.Response {
	return e.request(t, method, path, body, map[string]string{
		"Coder-Session-Token": "user-token",
		"Content-Type":        "application/json",
	})
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var value T
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return value
}

func wantStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d (body: %s)", resp.StatusCode, want, body)
	}
}

func wantDetail(t *testing.T, resp *http.Response, substring string) {
	t.Helper()
	payload := decode[map[string]string](t, resp)
	if !strings.Contains(payload["detail"], substring) {
		t.Errorf("detail = %q, want substring %q", payload["detail"], substring)
	}
}

func TestHealth(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodGet, "/health", "", nil)
	wantStatus(t, resp, http.StatusOK)
	payload := decode[map[string]string](t, resp)
	if payload["status"] != "ok" || payload["coder_url"] == "" {
		t.Errorf("payload = %v", payload)
	}
}

func TestReady(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodGet, "/ready", "", nil)
	wantStatus(t, resp, http.StatusOK)

	env.app.SetReady(false)
	resp = env.request(t, http.MethodGet, "/ready", "", nil)
	wantStatus(t, resp, http.StatusServiceUnavailable)
	wantDetail(t, resp, "Not ready")
}

func TestAuthSuccess(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodPost, "/auth", `{"email":"alice@example.com"}`, nil)
	wantStatus(t, resp, http.StatusCreated)
	payload := decode[AuthResponse](t, resp)
	if payload.SessionToken != "minted-token" {
		t.Errorf("SessionToken = %q", payload.SessionToken)
	}
}

func TestAuthInvalidEmail(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodPost, "/auth", `{"email":"not-an-email"}`, nil)
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	wantDetail(t, resp, "valid email")
}

func TestAuthInvalidBody(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodPost, "/auth", `{{{`, nil)
	wantStatus(t, resp, http.StatusUnprocessableEntity)
}

func TestAuthMissingOwnerToken(t *testing.T) {
	env := newTestEnv(t, func(s *config.Settings, _ *coderStub) {
		s.CoderSessionToken = "  "
	})
	resp := env.request(t, http.MethodPost, "/auth", `{"email":"alice@example.com"}`, nil)
	wantStatus(t, resp, http.StatusInternalServerError)
	wantDetail(t, resp, "Owner token missing")
}

func TestAuthUnknownUser(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stub.users = []any{}
	})
	resp := env.request(t, http.MethodPost, "/auth", `{"email":"ghost@example.com"}`, nil)
	wantStatus(t, resp, http.StatusNotFound)
}

func TestMeRequiresToken(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodGet, "/me", "", nil)
	wantStatus(t, resp, http.StatusUnauthorized)
	wantDetail(t, resp, "Missing Coder-Session-Token")
}

func TestMe(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/me", "")
	wantStatus(t, resp, http.StatusOK)
	payload := decode[MeResponse](t, resp)
	if payload.Username != "alice" || *payload.Email != "alice@example.com" {
		t.Errorf("payload = %+v", payload)
	}
	if payload.DashboardURL != "http://dash.test" {
		t.Errorf("DashboardURL = %q", payload.DashboardURL)
	}
}

func TestDashboardCacheAvoidsRepeatedBuildinfo(t *testing.T) {
	env := newTestEnv(t, nil)
	env.authed(t, http.MethodGet, "/me", "")
	env.authed(t, http.MethodGet, "/me", "")
	if calls := env.stub.buildinfoCalls.Load(); calls != 1 {
		t.Errorf("buildinfo calls = %d, want 1 (cached)", calls)
	}
}

func TestConfiguredDashboardSkipsBuildinfo(t *testing.T) {
	env := newTestEnv(t, func(s *config.Settings, _ *coderStub) {
		s.CoderDashboardURL = "http://configured.test"
	})
	resp := env.authed(t, http.MethodGet, "/me", "")
	payload := decode[MeResponse](t, resp)
	if payload.DashboardURL != "http://configured.test" {
		t.Errorf("DashboardURL = %q", payload.DashboardURL)
	}
	if calls := env.stub.buildinfoCalls.Load(); calls != 0 {
		t.Errorf("buildinfo calls = %d, want 0", calls)
	}
}

func TestCreateWorkspace(t *testing.T) {
	env := newTestEnv(t, nil)
	body := `{"name":"dev","template_id":"` + testTemplateUUID + `"}`
	resp := env.authed(t, http.MethodPost, "/workspaces", body)
	wantStatus(t, resp, http.StatusCreated)
	payload := decode[WorkspaceResponse](t, resp)
	if payload.Name != "dev" || payload.ID != "ws-1" {
		t.Errorf("payload = %+v", payload)
	}
}

func TestCreateWorkspaceValidationError(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodPost, "/workspaces", `{"name":"_bad_"}`)
	wantStatus(t, resp, http.StatusUnprocessableEntity)
}

func TestCreateWorkspaceRequiresToken(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodPost, "/workspaces", `{"name":"dev"}`, nil)
	wantStatus(t, resp, http.StatusUnauthorized)
}

func TestListWorkspaces(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/workspaces", "")
	wantStatus(t, resp, http.StatusOK)
	payload := decode[WorkspaceListResponse](t, resp)
	if payload.Count != 1 || len(payload.Workspaces) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	access := payload.Workspaces[0].Access
	if access == nil || !access.HasTerminal || access.Username != "alice" {
		t.Errorf("access = %+v", access)
	}
}

func TestListWorkspacesEmpty(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stub.workspaceList = map[string]any{"workspaces": []any{}}
	})
	resp := env.authed(t, http.MethodGet, "/workspaces", "")
	payload := decode[map[string]any](t, resp)
	// Count falls back to len(workspaces); workspaces must be [], not null.
	if payload["count"] != float64(0) {
		t.Errorf("count = %v", payload["count"])
	}
	if workspaces, ok := payload["workspaces"].([]any); !ok || len(workspaces) != 0 {
		t.Errorf("workspaces = %v (must be empty array, not null)", payload["workspaces"])
	}
}

func TestGetWorkspace(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/workspaces/dev", "")
	wantStatus(t, resp, http.StatusOK)
	payload := decode[WorkspaceResponse](t, resp)
	if payload.Name != "dev" || !payload.StartupReady {
		t.Errorf("payload = %+v", payload)
	}
}

func TestWorkspaceAccessEndpoint(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/workspaces/dev/access", "")
	wantStatus(t, resp, http.StatusOK)
	payload := decode[WorkspaceAccessResponse](t, resp)
	if payload.WorkspaceName != "dev" || !payload.StartupReady {
		t.Errorf("payload = %+v", payload)
	}
}

func TestOpenCodeServerRedirect(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodGet, "/workspaces/dev/open/code-server?token=user-token", "", nil)
	wantStatus(t, resp, http.StatusTemporaryRedirect)
	location := resp.Header.Get("Location")
	if !strings.Contains(location, "/@alice/dev.main/apps/code-server/") {
		t.Errorf("Location = %q", location)
	}
	if !strings.Contains(location, "?coder_session_token=user-token") {
		t.Errorf("Location missing token: %q", location)
	}
}

func TestOpenCodeServerMissingToken(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodGet, "/workspaces/dev/open/code-server", "", nil)
	wantStatus(t, resp, http.StatusUnauthorized)
}

func TestOpenCodeServerNotStarted(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stub.workspace["latest_build"].(map[string]any)["transition"] = "stop"
	})
	resp := env.authed(t, http.MethodGet, "/workspaces/dev/open/code-server", "")
	wantStatus(t, resp, http.StatusConflict)
	wantDetail(t, resp, "not started")
}

func TestOpenCodeServerAgentNotReady(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stubAgent(stub)["lifecycle_state"] = "starting"
	})
	resp := env.authed(t, http.MethodGet, "/workspaces/dev/open/code-server", "")
	wantStatus(t, resp, http.StatusConflict)
	wantDetail(t, resp, "Startup script is still running")
}

func TestOpenCodeServerNoApp(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stubAgent(stub)["apps"] = []any{}
	})
	resp := env.authed(t, http.MethodGet, "/workspaces/dev/open/code-server", "")
	wantStatus(t, resp, http.StatusNotFound)
	wantDetail(t, resp, "code-server app not found")
}

// stubAgent returns the single agent inside the stub's workspace payload.
func stubAgent(stub *coderStub) map[string]any {
	latest := stub.workspace["latest_build"].(map[string]any)
	resource := latest["resources"].([]any)[0].(map[string]any)
	return resource["agents"].([]any)[0].(map[string]any)
}

func TestVSCodeDesktop(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodPost, "/workspaces/dev/vscode-desktop", "")
	wantStatus(t, resp, http.StatusOK)
	payload := decode[VSCodeDesktopResponse](t, resp)
	if !strings.HasPrefix(payload.URI, "vscode://coder.coder-remote/open?") {
		t.Errorf("URI = %q", payload.URI)
	}
	if !strings.Contains(payload.URI, "token=minted-token") {
		t.Errorf("URI missing minted token: %q", payload.URI)
	}
	if !strings.Contains(payload.URI, "folder=%2Fhome%2Fcoder%2Fproject") {
		t.Errorf("URI missing folder: %q", payload.URI)
	}
}

func TestVSCodeDesktopNotEnabled(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stubAgent(stub)["display_apps"] = []any{"web_terminal"}
	})
	resp := env.authed(t, http.MethodPost, "/workspaces/dev/vscode-desktop", "")
	wantStatus(t, resp, http.StatusNotFound)
	wantDetail(t, resp, "not enabled")
}

func TestDeleteWorkspace(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodDelete, "/workspaces/dev", "")
	wantStatus(t, resp, http.StatusAccepted)
	payload := decode[WorkspaceBuildResponse](t, resp)
	if payload.ID != validBuildUUID || *payload.Transition != "delete" {
		t.Errorf("payload = %+v", payload)
	}
}

func TestGetWorkspaceBuild(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/workspacebuilds/"+validBuildUUID, "")
	wantStatus(t, resp, http.StatusOK)
	payload := decode[WorkspaceBuildResponse](t, resp)
	if payload.BuildNumber != 4 || payload.Status != "pending" {
		t.Errorf("payload = %+v", payload)
	}
}

func TestGetWorkspaceBuildInvalidUUID(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/workspacebuilds/not-a-uuid", "")
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	wantDetail(t, resp, "Invalid UUID")
}

func TestWorkspaceBuildLogsJSON(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/workspacebuilds/"+validBuildUUID+"/logs?after=1", "")
	wantStatus(t, resp, http.StatusOK)
	logs := decode[[]ProvisionerJobLog](t, resp)
	if len(logs) != 1 || logs[0].ID != 1 || *logs[0].Stage != "apply" {
		t.Errorf("logs = %+v", logs)
	}
}

func TestWorkspaceBuildLogsText(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/workspacebuilds/"+validBuildUUID+"/logs?format=text", "")
	wantStatus(t, resp, http.StatusOK)
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "plain logs" {
		t.Errorf("body = %q", body)
	}
}

func TestWorkspaceBuildLogsInvalidFormat(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/workspacebuilds/"+validBuildUUID+"/logs?format=xml", "")
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	wantDetail(t, resp, "format must be")
}

func TestWorkspaceBuildLogsInvalidAfter(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/workspacebuilds/"+validBuildUUID+"/logs?after=abc", "")
	wantStatus(t, resp, http.StatusUnprocessableEntity)
	wantDetail(t, resp, "Invalid integer")
}

func TestStartupLogs(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.authed(t, http.MethodGet, "/workspaces/dev/startup-logs", "")
	wantStatus(t, resp, http.StatusOK)
	logs := decode[[]WorkspaceAgentLog](t, resp)
	if len(logs) != 1 || logs[0].ID != 2 || *logs[0].Level != "info" {
		t.Errorf("logs = %+v", logs)
	}
}

func TestStartupLogsNoAgent(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stub.workspace["latest_build"].(map[string]any)["resources"] = []any{}
	})
	resp := env.authed(t, http.MethodGet, "/workspaces/dev/startup-logs", "")
	wantStatus(t, resp, http.StatusNotFound)
	wantDetail(t, resp, "No agent found on workspace")
}

func TestCoderErrorsPassThrough(t *testing.T) {
	coderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "workspace missing"})
	}))
	t.Cleanup(coderServer.Close)
	app := NewApp(&config.Settings{CoderURL: coderServer.URL})
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	env := &testEnv{app: app, server: server}

	resp := env.authed(t, http.MethodGet, "/workspaces/missing", "")
	wantStatus(t, resp, http.StatusNotFound)
	wantDetail(t, resp, "workspace missing")
}

func TestUnknownRouteReturnsJSON404(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodGet, "/nope", "", nil)
	wantStatus(t, resp, http.StatusNotFound)
	wantDetail(t, resp, "Not Found")
}

func TestSecurityHeaders(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodGet, "/health", "", nil)
	headers := map[string]string{
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Permissions-Policy":     "camera=(), microphone=(), geolocation=()",
	}
	for key, want := range headers {
		if got := resp.Header.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestCORSPreflight(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodOptions, "/workspaces", "", map[string]string{
		"Origin":                         "http://ui.test",
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "coder-session-token",
	})
	wantStatus(t, resp, http.StatusOK)
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://ui.test" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); got != "coder-session-token" {
		t.Errorf("Allow-Headers = %q", got)
	}
}

func TestCORSActualRequest(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodGet, "/health", "", map[string]string{
		"Origin": "http://ui.test",
	})
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://ui.test" {
		t.Errorf("Allow-Origin = %q", got)
	}
}

func TestCORSDisallowedOrigin(t *testing.T) {
	env := newTestEnv(t, nil)
	resp := env.request(t, http.MethodGet, "/health", "", map[string]string{
		"Origin": "http://evil.test",
	})
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty", got)
	}
}

// newFailingEnv points an app at a Coder server that rejects every call,
// exercising the error-propagation branch of each handler.
func newFailingEnv(t *testing.T, status int, body string) *testEnv {
	t.Helper()
	coderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(coderServer.Close)
	app := NewApp(&config.Settings{
		CoderURL:          coderServer.URL,
		CoderSessionToken: "owner-token",
	})
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	return &testEnv{app: app, server: server}
}

func TestEndpointsPropagateCoderAuthErrors(t *testing.T) {
	endpoints := []struct{ method, path, body string }{
		{http.MethodGet, "/me", ""},
		{http.MethodGet, "/workspaces", ""},
		{http.MethodPost, "/workspaces", `{"name":"dev","template_id":"` + testTemplateUUID + `"}`},
		{http.MethodGet, "/workspaces/dev", ""},
		{http.MethodGet, "/workspaces/dev/access", ""},
		{http.MethodGet, "/workspaces/dev/open/code-server?token=user-token", ""},
		{http.MethodPost, "/workspaces/dev/vscode-desktop", ""},
		{http.MethodDelete, "/workspaces/dev", ""},
		{http.MethodGet, "/workspacebuilds/" + validBuildUUID, ""},
		{http.MethodGet, "/workspacebuilds/" + validBuildUUID + "/logs", ""},
		{http.MethodGet, "/workspaces/dev/startup-logs", ""},
		{http.MethodPost, "/auth", `{"email":"alice@example.com"}`},
	}
	env := newFailingEnv(t, http.StatusUnauthorized, `{}`)
	for _, endpoint := range endpoints {
		t.Run(endpoint.method+" "+endpoint.path, func(t *testing.T) {
			resp := env.authed(t, endpoint.method, endpoint.path, endpoint.body)
			wantStatus(t, resp, http.StatusUnauthorized)
			wantDetail(t, resp, "Invalid or expired session token")
		})
	}
}

func TestMeDashboardDiscoveryFailure(t *testing.T) {
	stubDown := false
	stub := newCoderStub()
	coderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stubDown && r.URL.Path == "/api/v2/buildinfo" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		stub.handler(t).ServeHTTP(w, r)
	}))
	t.Cleanup(coderServer.Close)
	app := NewApp(&config.Settings{CoderURL: coderServer.URL})
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	env := &testEnv{app: app, server: server, stub: stub}

	stubDown = true
	resp := env.authed(t, http.MethodGet, "/me", "")
	wantStatus(t, resp, http.StatusBadGateway)
}
