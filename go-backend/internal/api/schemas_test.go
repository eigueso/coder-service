package api

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/malmonte/go-backend/internal/coder"
	"github.com/malmonte/go-backend/internal/config"
)

func ptr[T any](v T) *T { return &v }

const testTemplateUUID = "3b1f2a4c-1111-2222-3333-444455556666"

func validCreateRequest() CreateWorkspaceRequest {
	return CreateWorkspaceRequest{
		Name:       "my-workspace",
		TemplateID: ptr(testTemplateUUID),
	}
}

func TestCreateWorkspaceRequestValid(t *testing.T) {
	req := validCreateRequest()
	if err := req.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
}

func TestCreateWorkspaceRequestValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CreateWorkspaceRequest)
		detail string
	}{
		{"empty name", func(r *CreateWorkspaceRequest) { r.Name = "" },
			"Workspace name must be"},
		{"name too long", func(r *CreateWorkspaceRequest) { r.Name = strings.Repeat("a", 33) },
			"Workspace name must be"},
		{"name starts with hyphen", func(r *CreateWorkspaceRequest) { r.Name = "-bad" },
			"Workspace name must be"},
		{"name has invalid chars", func(r *CreateWorkspaceRequest) { r.Name = "bad_name" },
			"Workspace name must be"},
		{"reserved name new", func(r *CreateWorkspaceRequest) { r.Name = "New" },
			"cannot be 'new' or 'create'"},
		{"reserved name create", func(r *CreateWorkspaceRequest) { r.Name = "create" },
			"cannot be 'new' or 'create'"},
		{"no template", func(r *CreateWorkspaceRequest) { r.TemplateID = nil },
			"exactly one of template_id"},
		{"both templates", func(r *CreateWorkspaceRequest) {
			r.TemplateVersionID = ptr(testTemplateUUID)
		}, "exactly one of template_id"},
		{"invalid template uuid", func(r *CreateWorkspaceRequest) { r.TemplateID = ptr("not-a-uuid") },
			"Invalid UUID"},
		{"empty rich param", func(r *CreateWorkspaceRequest) {
			r.RichParameterValues = []RichParameterValue{{Name: "cpu", Value: ""}}
		}, "non-empty name and value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validCreateRequest()
			tt.mutate(&req)
			err := req.Validate()
			var apiErr *coder.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected APIError, got %v", err)
			}
			if apiErr.Status != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422", apiErr.Status)
			}
			if !strings.Contains(apiErr.Detail, tt.detail) {
				t.Errorf("detail = %q, want substring %q", apiErr.Detail, tt.detail)
			}
		})
	}
}

func TestToCoderBody(t *testing.T) {
	req := CreateWorkspaceRequest{
		Name:              "dev",
		TemplateVersionID: ptr(testTemplateUUID),
		RichParameterValues: []RichParameterValue{
			{Name: "cpu", Value: "4"},
		},
	}
	got := req.ToCoderBody()
	want := map[string]any{
		"name":                "dev",
		"template_version_id": testTemplateUUID,
		"rich_parameter_values": []map[string]any{
			{"name": "cpu", "value": "4"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestToCoderBodyMinimal(t *testing.T) {
	req := CreateWorkspaceRequest{Name: "dev", TemplateID: ptr(testTemplateUUID)}
	got := req.ToCoderBody()
	want := map[string]any{"name": "dev", "template_id": testTemplateUUID}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestWorkspaceResponseFromCoder(t *testing.T) {
	payload := map[string]any{
		"id":          "ws-1",
		"name":        "dev",
		"template_id": "tpl-1",
		"latest_build": map[string]any{
			"id":           "build-1",
			"build_number": float64(7),
			"transition":   "start",
			"job":          map[string]any{"status": "succeeded"},
			"resources": []any{map[string]any{"agents": []any{
				map[string]any{"name": "main", "lifecycle_state": "ready"},
			}}},
		},
	}
	got := WorkspaceResponseFromCoder(payload)
	if got.ID != "ws-1" || got.Name != "dev" {
		t.Errorf("identity fields wrong: %+v", got)
	}
	if *got.TemplateID != "tpl-1" {
		t.Errorf("TemplateID = %v", got.TemplateID)
	}
	if got.LatestBuild.ID != "build-1" || got.LatestBuild.Status != "succeeded" {
		t.Errorf("LatestBuild = %+v", got.LatestBuild)
	}
	if *got.LatestBuild.BuildNumber != 7 || *got.LatestBuild.Transition != "start" {
		t.Errorf("LatestBuild = %+v", got.LatestBuild)
	}
	if *got.AgentLifecycleState != "ready" || !got.StartupReady {
		t.Errorf("agent fields wrong: %+v", got)
	}
}

func TestWorkspaceResponseFromCoderDefaults(t *testing.T) {
	got := WorkspaceResponseFromCoder(map[string]any{"id": "ws-1", "name": "dev"})
	if got.LatestBuild.Status != "unknown" {
		t.Errorf("Status = %q, want unknown", got.LatestBuild.Status)
	}
	if got.TemplateID != nil || got.AgentLifecycleState != nil || got.StartupReady {
		t.Errorf("expected empty optionals: %+v", got)
	}
}

func TestWorkspaceBuildResponseFromCoder(t *testing.T) {
	payload := map[string]any{
		"id":           "build-1",
		"workspace_id": "ws-1",
		"build_number": float64(2),
		"transition":   "delete",
		"created_at":   "2026-01-01T00:00:00Z",
		"job":          map[string]any{"status": "failed", "error": "boom"},
	}
	got := WorkspaceBuildResponseFromCoder(payload)
	if got.ID != "build-1" || got.WorkspaceID != "ws-1" || got.BuildNumber != 2 {
		t.Errorf("identity fields wrong: %+v", got)
	}
	if got.Status != "failed" || *got.JobError != "boom" {
		t.Errorf("job fields wrong: %+v", got)
	}
	if *got.Transition != "delete" || *got.CreatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("optional fields wrong: %+v", got)
	}
}

func TestLogMappers(t *testing.T) {
	job := ProvisionerJobLogFromCoder(map[string]any{
		"id": float64(9), "created_at": "t", "log_level": "info",
		"log_source": "provisioner", "output": "applying", "stage": "apply",
	})
	if job.ID != 9 || *job.LogLevel != "info" || *job.Stage != "apply" {
		t.Errorf("job log = %+v", job)
	}
	agent := WorkspaceAgentLogFromCoder(map[string]any{
		"id": float64(3), "level": "error", "output": "oops", "source_id": "src-1",
	})
	if agent.ID != 3 || *agent.Level != "error" || *agent.SourceID != "src-1" {
		t.Errorf("agent log = %+v", agent)
	}
	if agent.CreatedAt != nil {
		t.Errorf("CreatedAt should be nil, got %v", agent.CreatedAt)
	}
}

func readyWorkspacePayload() map[string]any {
	return map[string]any{
		"id":   "ws-1",
		"name": "dev",
		"latest_build": map[string]any{
			"id":         "build-1",
			"transition": "start",
			"job":        map[string]any{"status": "succeeded"},
			"resources": []any{map[string]any{"agents": []any{map[string]any{
				"id":                 "agent-1",
				"name":               "main",
				"lifecycle_state":    "ready",
				"status":             "connected",
				"expanded_directory": "/home/coder/project",
				"apps":               []any{map[string]any{"slug": "code-server"}},
				"display_apps":       []any{"vscode"},
			}}}},
		},
	}
}

func TestWorkspaceAccessLegacyMode(t *testing.T) {
	settings := &config.Settings{CoderURL: "http://coder:3000", CoderAccessAuthMode: "legacy_token"}
	user := map[string]any{"username": "alice"}

	access := WorkspaceAccess(readyWorkspacePayload(), user, "http://dash", settings)

	if !access.Started || !access.StartupReady || !access.HasTerminal {
		t.Errorf("flags wrong: %+v", access)
	}
	if !access.HasVSCodeBrowser || !access.HasVSCodeDesktop {
		t.Errorf("vscode flags wrong: %+v", access)
	}
	if access.TerminalURL != nil {
		t.Error("legacy mode must not expose a Coder terminal URL")
	}
	// Legacy mode prefers the API base for app links.
	wantBrowser := "http://coder:3000/@alice/dev.main/apps/code-server/"
	if access.VSCodeBrowserURL == nil || *access.VSCodeBrowserURL != wantBrowser {
		t.Errorf("VSCodeBrowserURL = %v, want %q", access.VSCodeBrowserURL, wantBrowser)
	}
	if *access.AgentDirectory != "/home/coder/project" {
		t.Errorf("AgentDirectory = %v", access.AgentDirectory)
	}
	if *access.CodeServerSlug != "code-server" {
		t.Errorf("CodeServerSlug = %v", access.CodeServerSlug)
	}
}

func TestWorkspaceAccessSSOMode(t *testing.T) {
	settings := &config.Settings{CoderURL: "http://coder:3000", CoderAccessAuthMode: "sso"}
	user := map[string]any{"username": "alice"}

	access := WorkspaceAccess(readyWorkspacePayload(), user, "http://dash", settings)

	wantTerminal := "http://dash/@alice/dev.main/terminal"
	if access.TerminalURL == nil || *access.TerminalURL != wantTerminal {
		t.Errorf("TerminalURL = %v, want %q", access.TerminalURL, wantTerminal)
	}
	// SSO mode uses the dashboard for app links.
	wantBrowser := "http://dash/@alice/dev.main/apps/code-server/"
	if access.VSCodeBrowserURL == nil || *access.VSCodeBrowserURL != wantBrowser {
		t.Errorf("VSCodeBrowserURL = %v, want %q", access.VSCodeBrowserURL, wantBrowser)
	}
}

func TestWorkspaceAccessNotStarted(t *testing.T) {
	settings := &config.Settings{CoderURL: "http://coder:3000"}
	workspace := map[string]any{
		"id": "ws-1", "name": "dev",
		"latest_build": map[string]any{
			"transition": "stop",
			"job":        map[string]any{"status": "succeeded"},
		},
	}
	access := WorkspaceAccess(workspace, map[string]any{"username": "alice"}, "http://dash", settings)
	if access.Started || access.StartupReady || access.HasTerminal ||
		access.HasVSCodeBrowser || access.HasVSCodeDesktop {
		t.Errorf("stopped workspace must expose nothing: %+v", access)
	}
	if access.AgentID != nil || access.VSCodeBrowserURL != nil {
		t.Errorf("no agent data expected: %+v", access)
	}
}

func TestWorkspaceAccessAgentDirectoryFallback(t *testing.T) {
	settings := &config.Settings{CoderURL: "http://coder:3000"}
	workspace := readyWorkspacePayload()
	latest := workspace["latest_build"].(map[string]any)
	agent := latest["resources"].([]any)[0].(map[string]any)["agents"].([]any)[0].(map[string]any)
	delete(agent, "expanded_directory")
	agent["directory"] = "/workspace"

	access := WorkspaceAccess(workspace, map[string]any{"username": "alice"}, "http://dash", settings)
	if access.AgentDirectory == nil || *access.AgentDirectory != "/workspace" {
		t.Errorf("AgentDirectory = %v, want /workspace", access.AgentDirectory)
	}
}
