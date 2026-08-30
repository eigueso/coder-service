package coder

import (
	"reflect"
	"testing"
)

func workspaceWithAgents(agents ...map[string]any) map[string]any {
	anyAgents := make([]any, 0, len(agents))
	for _, agent := range agents {
		anyAgents = append(anyAgents, agent)
	}
	return map[string]any{
		"latest_build": map[string]any{
			"resources": []any{
				map[string]any{"agents": anyAgents},
			},
		},
	}
}

func TestIterAgents(t *testing.T) {
	workspace := map[string]any{
		"latest_build": map[string]any{
			"resources": []any{
				map[string]any{"agents": []any{map[string]any{"name": "a"}}},
				map[string]any{"agents": []any{map[string]any{"name": "b"}, map[string]any{"name": "c"}}},
				map[string]any{},
			},
		},
	}
	agents := IterAgents(workspace)
	if len(agents) != 3 {
		t.Fatalf("got %d agents, want 3", len(agents))
	}
	if agents[0]["name"] != "a" || agents[2]["name"] != "c" {
		t.Errorf("agents out of order: %v", agents)
	}
}

func TestIterAgentsEmptyWorkspace(t *testing.T) {
	if agents := IterAgents(map[string]any{}); agents != nil {
		t.Errorf("expected nil, got %v", agents)
	}
	if agents := IterAgents(nil); agents != nil {
		t.Errorf("expected nil for nil workspace, got %v", agents)
	}
}

func TestPickAgentPrefersMain(t *testing.T) {
	workspace := workspaceWithAgents(
		map[string]any{"name": "other"},
		map[string]any{"name": "main"},
	)
	if agent := PickAgent(workspace); getString(agent, "name") != "main" {
		t.Errorf("expected main agent, got %v", agent)
	}
}

func TestPickAgentFallsBackToFirst(t *testing.T) {
	workspace := workspaceWithAgents(
		map[string]any{"name": "first"},
		map[string]any{"name": "second"},
	)
	if agent := PickAgent(workspace); getString(agent, "name") != "first" {
		t.Errorf("expected first agent, got %v", agent)
	}
}

func TestPickAgentNoAgents(t *testing.T) {
	if agent := PickAgent(map[string]any{}); agent != nil {
		t.Errorf("expected nil, got %v", agent)
	}
}

func TestFindApp(t *testing.T) {
	agent := map[string]any{
		"apps": []any{
			map[string]any{"slug": "other"},
			map[string]any{"slug": "code-server", "url": "http://x"},
		},
	}
	if app := FindApp(agent, "code-server"); getString(app, "url") != "http://x" {
		t.Errorf("unexpected app: %v", app)
	}
	if app := FindApp(agent, "missing"); app != nil {
		t.Errorf("expected nil, got %v", app)
	}
	if app := FindApp(nil, "code-server"); app != nil {
		t.Errorf("expected nil for nil agent, got %v", app)
	}
}

func TestDisplayApps(t *testing.T) {
	agent := map[string]any{"display_apps": []any{"vscode", 42, "web_terminal"}}
	got := DisplayApps(agent)
	want := []string{"vscode", "web_terminal"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DisplayApps = %v, want %v", got, want)
	}
	if apps := DisplayApps(nil); apps != nil {
		t.Errorf("expected nil for nil agent, got %v", apps)
	}
}

func TestIsStarted(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		transition string
		want       bool
	}{
		{"started", "succeeded", "start", true},
		{"stopped", "succeeded", "stop", false},
		{"failed", "failed", "start", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := map[string]any{
				"latest_build": map[string]any{
					"transition": tt.transition,
					"job":        map[string]any{"status": tt.status},
				},
			}
			if got := IsStarted(workspace); got != tt.want {
				t.Errorf("IsStarted = %v, want %v", got, tt.want)
			}
		})
	}
	if IsStarted(map[string]any{}) {
		t.Error("IsStarted on empty workspace should be false")
	}
}

func TestAgentReady(t *testing.T) {
	if !IsAgentReady(map[string]any{"lifecycle_state": "ready"}) {
		t.Error("ready agent should be ready")
	}
	if IsAgentReady(map[string]any{"lifecycle_state": "starting"}) {
		t.Error("starting agent should not be ready")
	}
	if IsAgentReady(nil) {
		t.Error("nil agent should not be ready")
	}
	if AgentLifecycleState(map[string]any{"lifecycle_state": "starting"}) != "starting" {
		t.Error("unexpected lifecycle state")
	}
	if AgentLifecycleState(nil) != "" {
		t.Error("nil agent lifecycle state should be empty")
	}
}

func TestBuildVSCodeBrowserURL(t *testing.T) {
	got := BuildVSCodeBrowserURL("http://base/", "alice", "dev", "main", "code-server")
	want := "http://base/@alice/dev.main/apps/code-server/"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildVSCodeBrowserURLEscapesAndDefaultsSlug(t *testing.T) {
	got := BuildVSCodeBrowserURL("http://base", "a b", "w s", "ag ent", "")
	want := "http://base/@a%20b/w%20s.ag%20ent/apps/code-server/"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildWebTerminalURL(t *testing.T) {
	got := BuildWebTerminalURL("http://base/", "alice", "dev", "main")
	want := "http://base/@alice/dev.main/terminal"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got = BuildWebTerminalURL("http://base", "alice", "dev", "")
	want = "http://base/@alice/dev/terminal"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPreferCoderAppBase(t *testing.T) {
	if got := PreferCoderAppBase("http://api/", "http://dash"); got != "http://api" {
		t.Errorf("got %q, want api base", got)
	}
	if got := PreferCoderAppBase("", "http://dash/"); got != "http://dash" {
		t.Errorf("got %q, want dashboard", got)
	}
}

func TestBuildVSCodeDesktopURI(t *testing.T) {
	got := BuildVSCodeDesktopURI(
		"http://coder/", "alice", "dev", "tok", "main", "/home/alice", "")
	want := "vscode://coder.coder-remote/open?" +
		"agent=main&folder=%2Fhome%2Falice&openRecent=true&owner=alice&token=tok&url=http%3A%2F%2Fcoder&workspace=dev"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildVSCodeDesktopURIWithoutOptionals(t *testing.T) {
	got := BuildVSCodeDesktopURI("http://coder", "alice", "dev", "tok", "", "", "vscode")
	want := "vscode://coder.coder-remote/open?" +
		"openRecent=true&owner=alice&token=tok&url=http%3A%2F%2Fcoder&workspace=dev"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
