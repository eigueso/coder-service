package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/malmonte/go-backend/internal/config"
)

func TestToWSBase(t *testing.T) {
	tests := []struct{ in, want string }{
		{"http://host:3000/", "ws://host:3000"},
		{"https://host", "wss://host"},
		{"ftp://host", "ftp://host"},
	}
	for _, tt := range tests {
		if got := toWSBase(tt.in); got != tt.want {
			t.Errorf("toWSBase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestClampInt(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"", 48},
		{"junk", 48},
		{"0", 1},
		{"1000", 500},
		{"80", 80},
	}
	for _, tt := range tests {
		if got := clampInt(tt.raw, 48, 1, 500); got != tt.want {
			t.Errorf("clampInt(%q) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

func TestUpstreamURLsPrefersAPIByDefault(t *testing.T) {
	candidates := upstreamURLs("http://coder.internal:3000", "http://dash.test", "agent-1", "q=1")
	if len(candidates) != 2 {
		t.Fatalf("got %d candidates", len(candidates))
	}
	if candidates[0].label != "api" || candidates[1].label != "dashboard" {
		t.Errorf("order = %s, %s", candidates[0].label, candidates[1].label)
	}
	want := "ws://coder.internal:3000/api/v2/workspaceagents/agent-1/pty?q=1"
	if candidates[0].url != want {
		t.Errorf("url = %q, want %q", candidates[0].url, want)
	}
}

func TestUpstreamURLsPrefersDashboardForLoopbackAPI(t *testing.T) {
	candidates := upstreamURLs("http://localhost:3000", "https://x.try.coder.app", "agent-1", "q=1")
	if candidates[0].label != "dashboard" {
		t.Errorf("first candidate = %s, want dashboard", candidates[0].label)
	}
}

func TestUpstreamURLsDeduplicates(t *testing.T) {
	candidates := upstreamURLs("http://same:3000", "http://same:3000/", "agent-1", "q=1")
	if len(candidates) != 1 {
		t.Errorf("got %d candidates, want 1", len(candidates))
	}
}

// dialTerminal opens a client WebSocket against the test server's terminal
// endpoint and returns the close code observed on the first failed read.
func dialTerminal(t *testing.T, env *testEnv, path, origin string) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(env.server.URL, "http") + path
	headers := http.Header{}
	if origin != "" {
		headers.Set("Origin", origin)
	}
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Fatal("expected the server to close the connection")
	}
	return websocket.CloseStatus(err)
}

func TestTerminalSSOModeRejected(t *testing.T) {
	env := newTestEnv(t, func(s *config.Settings, _ *coderStub) {
		s.CoderAccessAuthMode = "sso"
	})
	if code := dialTerminal(t, env, "/workspaces/dev/terminal?token=user-token", "http://ui.test"); code != 4404 {
		t.Errorf("close code = %d, want 4404", code)
	}
}

func TestTerminalDisallowedOrigin(t *testing.T) {
	env := newTestEnv(t, nil)
	if code := dialTerminal(t, env, "/workspaces/dev/terminal?token=user-token", "http://evil.test"); code != 4403 {
		t.Errorf("close code = %d, want 4403", code)
	}
}

func TestTerminalMissingToken(t *testing.T) {
	env := newTestEnv(t, nil)
	if code := dialTerminal(t, env, "/workspaces/dev/terminal", "http://ui.test"); code != 4401 {
		t.Errorf("close code = %d, want 4401", code)
	}
}

func TestTerminalCapacityReached(t *testing.T) {
	env := newTestEnv(t, func(s *config.Settings, _ *coderStub) {
		s.TerminalMaxConnectionsPerWorker = 0
	})
	if code := dialTerminal(t, env, "/workspaces/dev/terminal?token=user-token", "http://ui.test"); code != 4429 {
		t.Errorf("close code = %d, want 4429", code)
	}
}

func TestTerminalWorkspaceNotStarted(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stub.workspace["latest_build"].(map[string]any)["transition"] = "stop"
	})
	if code := dialTerminal(t, env, "/workspaces/dev/terminal?token=user-token", "http://ui.test"); code != 4000 {
		t.Errorf("close code = %d, want 4000", code)
	}
}

func TestTerminalNoAgent(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stub.workspace["latest_build"].(map[string]any)["resources"] = []any{}
	})
	if code := dialTerminal(t, env, "/workspaces/dev/terminal?token=user-token", "http://ui.test"); code != 4001 {
		t.Errorf("close code = %d, want 4001", code)
	}
}

func TestTerminalAgentNotReady(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stubAgent(stub)["lifecycle_state"] = "starting"
	})
	if code := dialTerminal(t, env, "/workspaces/dev/terminal?token=user-token", "http://ui.test"); code != 4003 {
		t.Errorf("close code = %d, want 4003", code)
	}
}

func TestTerminalAgentNotConnected(t *testing.T) {
	env := newTestEnv(t, func(_ *config.Settings, stub *coderStub) {
		stubAgent(stub)["status"] = "disconnected"
	})
	if code := dialTerminal(t, env, "/workspaces/dev/terminal?token=user-token", "http://ui.test"); code != 4002 {
		t.Errorf("close code = %d, want 4002", code)
	}
}

func TestTerminalAgentIDMismatch(t *testing.T) {
	env := newTestEnv(t, nil)
	path := "/workspaces/dev/terminal?token=user-token&agent_id=" + validBuildUUID
	if code := dialTerminal(t, env, path, "http://ui.test"); code != 4403 {
		t.Errorf("close code = %d, want 4403", code)
	}
}

func TestTerminalInvalidAgentID(t *testing.T) {
	env := newTestEnv(t, nil)
	path := "/workspaces/dev/terminal?token=user-token&agent_id=not-a-uuid"
	if code := dialTerminal(t, env, path, "http://ui.test"); code != websocket.StatusInternalError {
		t.Errorf("close code = %d, want 1011", code)
	}
}

// TestTerminalRelay exercises the full proxy path: the stub Coder server
// hosts an echoing PTY WebSocket and the client sees its bytes come back.
func TestTerminalRelay(t *testing.T) {
	env := newTestEnvWithPTY(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(env.server.URL, "http") +
		"/workspaces/dev/terminal?token=user-token&height=24&width=80"
	headers := http.Header{}
	headers.Set("Origin", "http://ui.test")
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := conn.Write(ctx, websocket.MessageBinary, []byte("ls -la\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "echo: ls -la\n" {
		t.Errorf("data = %q", data)
	}
}

// newTestEnvWithPTY builds a test env whose Coder stub also serves an
// echoing agent PTY WebSocket at the standard endpoint.
func newTestEnvWithPTY(t *testing.T) *testEnv {
	t.Helper()
	env := newTestEnv(t, nil)

	stubHandler := env.stub.handler(t)
	ptyMux := http.NewServeMux()
	ptyMux.HandleFunc("/api/v2/workspaceagents/agent-1/pty", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Coder-Session-Token") != "user-token" {
			t.Errorf("pty missing session token")
		}
		if r.URL.Query().Get("height") != "24" || r.URL.Query().Get("width") != "80" {
			t.Errorf("pty query = %v", r.URL.Query())
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx := r.Context()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if err := conn.Write(ctx, websocket.MessageBinary, append([]byte("echo: "), data...)); err != nil {
				return
			}
		}
	})
	ptyMux.Handle("/", stubHandler)

	// Replace the plain stub server with one that also speaks WebSocket,
	// then point a fresh app at it.
	coderServer := httptest.NewServer(ptyMux)
	t.Cleanup(coderServer.Close)
	settings := *env.app.Settings
	settings.CoderURL = coderServer.URL
	app := NewApp(&settings)
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	return &testEnv{app: app, server: server, stub: env.stub}
}

// TestTerminalDashboardDiscoveryFailure covers closeWithError: buildinfo
// fails during dashboard discovery, so the socket closes with 1011.
func TestTerminalDashboardDiscoveryFailure(t *testing.T) {
	stub := newCoderStub()
	coderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/buildinfo" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		stub.handler(t).ServeHTTP(w, r)
	}))
	t.Cleanup(coderServer.Close)
	app := NewApp(&config.Settings{
		CoderURL:                        coderServer.URL,
		TerminalMaxConnectionsPerWorker: 2,
		TerminalAllowedOrigins:          "http://ui.test",
	})
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	env := &testEnv{app: app, server: server, stub: stub}

	code := dialTerminal(t, env, "/workspaces/dev/terminal?token=user-token", "http://ui.test")
	if code != websocket.StatusInternalError {
		t.Errorf("close code = %d, want 1011", code)
	}
}
