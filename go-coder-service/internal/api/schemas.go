// Package api exposes the JSON API, mirroring backend/app/{schemas,main}.py.
package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/malmonte/go-coder-service/internal/coder"
	"github.com/malmonte/go-coder-service/internal/config"
)

type AuthRequest struct {
	Email string `json:"email"`
}

type AuthResponse struct {
	SessionToken string `json:"session_token"`
}

type MeResponse struct {
	Username     string  `json:"username"`
	Email        *string `json:"email"`
	DashboardURL string  `json:"dashboard_url"`
}

type WorkspaceAccessResponse struct {
	WorkspaceID         string  `json:"workspace_id"`
	WorkspaceName       string  `json:"workspace_name"`
	Username            string  `json:"username"`
	Started             bool    `json:"started"`
	StartupReady        bool    `json:"startup_ready"`
	AgentLifecycleState *string `json:"agent_lifecycle_state"`
	AgentID             *string `json:"agent_id"`
	AgentName           *string `json:"agent_name"`
	AgentDirectory      *string `json:"agent_directory"`
	HasTerminal         bool    `json:"has_terminal"`
	TerminalURL         *string `json:"terminal_url"`
	HasVSCodeBrowser    bool    `json:"has_vscode_browser"`
	HasVSCodeDesktop    bool    `json:"has_vscode_desktop"`
	VSCodeBrowserURL    *string `json:"vscode_browser_url"`
	CodeServerSlug      *string `json:"code_server_slug"`
}

type VSCodeDesktopResponse struct {
	URI string `json:"uri"`
}

type RichParameterValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type CreateWorkspaceRequest struct {
	Name                string               `json:"name"`
	TemplateID          *string              `json:"template_id"`
	TemplateVersionID   *string              `json:"template_version_id"`
	RichParameterValues []RichParameterValue `json:"rich_parameter_values"`
}

var workspaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,31})$`)

// Validate replicates the pydantic model constraints on CreateWorkspaceRequest.
func (r *CreateWorkspaceRequest) Validate() error {
	if len(r.Name) < 1 || len(r.Name) > 32 || !workspaceNamePattern.MatchString(r.Name) {
		return coder.NewAPIError(http.StatusUnprocessableEntity,
			"Workspace name must be letters, numbers, or hyphens (max 32 chars)")
	}
	lower := strings.ToLower(r.Name)
	if lower == "new" || lower == "create" {
		return coder.NewAPIError(http.StatusUnprocessableEntity,
			"Workspace name cannot be 'new' or 'create'")
	}
	hasTemplate := r.TemplateID != nil && *r.TemplateID != ""
	hasVersion := r.TemplateVersionID != nil && *r.TemplateVersionID != ""
	if hasTemplate == hasVersion {
		return coder.NewAPIError(http.StatusUnprocessableEntity,
			"Provide exactly one of template_id or template_version_id")
	}
	for _, id := range []*string{r.TemplateID, r.TemplateVersionID} {
		if id != nil && *id != "" {
			if _, err := uuid.Parse(*id); err != nil {
				return coder.NewAPIError(http.StatusUnprocessableEntity,
					fmt.Sprintf("Invalid UUID: %s", *id))
			}
		}
	}
	for _, param := range r.RichParameterValues {
		if param.Name == "" || param.Value == "" {
			return coder.NewAPIError(http.StatusUnprocessableEntity,
				"Rich parameter values require non-empty name and value")
		}
	}
	return nil
}

func (r *CreateWorkspaceRequest) ToCoderBody() map[string]any {
	body := map[string]any{"name": r.Name}
	if r.TemplateID != nil && *r.TemplateID != "" {
		body["template_id"] = *r.TemplateID
	}
	if r.TemplateVersionID != nil && *r.TemplateVersionID != "" {
		body["template_version_id"] = *r.TemplateVersionID
	}
	if len(r.RichParameterValues) > 0 {
		params := make([]map[string]any, 0, len(r.RichParameterValues))
		for _, p := range r.RichParameterValues {
			params = append(params, map[string]any{"name": p.Name, "value": p.Value})
		}
		body["rich_parameter_values"] = params
	}
	return body
}

type BuildSummary struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	BuildNumber *int64  `json:"build_number"`
	Transition  *string `json:"transition"`
}

type WorkspaceResponse struct {
	ID                  string                   `json:"id"`
	Name                string                   `json:"name"`
	TemplateID          *string                  `json:"template_id"`
	LatestBuild         BuildSummary             `json:"latest_build"`
	AgentLifecycleState *string                  `json:"agent_lifecycle_state"`
	StartupReady        bool                     `json:"startup_ready"`
	Access              *WorkspaceAccessResponse `json:"access"`
}

type WorkspaceListResponse struct {
	Count      int64               `json:"count"`
	Workspaces []WorkspaceResponse `json:"workspaces"`
}

type WorkspaceBuildResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	BuildNumber int64   `json:"build_number"`
	Status      string  `json:"status"`
	Transition  *string `json:"transition"`
	JobError    *string `json:"job_error"`
	CreatedAt   *string `json:"created_at"`
}

type ProvisionerJobLog struct {
	ID        int64   `json:"id"`
	CreatedAt *string `json:"created_at"`
	LogLevel  *string `json:"log_level"`
	LogSource *string `json:"log_source"`
	Output    *string `json:"output"`
	Stage     *string `json:"stage"`
}

type WorkspaceAgentLog struct {
	ID        int64   `json:"id"`
	CreatedAt *string `json:"created_at"`
	Level     *string `json:"level"`
	Output    *string `json:"output"`
	SourceID  *string `json:"source_id"`
}

func optString(obj map[string]any, key string) *string {
	if obj == nil {
		return nil
	}
	if value, ok := obj[key].(string); ok && value != "" {
		return &value
	}
	return nil
}

func optInt(obj map[string]any, key string) *int64 {
	if obj == nil {
		return nil
	}
	if value, ok := obj[key].(float64); ok {
		i := int64(value)
		return &i
	}
	return nil
}

func intOr(obj map[string]any, key string, fallback int64) int64 {
	if value := optInt(obj, key); value != nil {
		return *value
	}
	return fallback
}

func str(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	value, _ := obj[key].(string)
	return value
}

func obj(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	value, _ := parent[key].(map[string]any)
	return value
}

// WorkspaceResponseFromCoder mirrors WorkspaceResponse.from_coder.
func WorkspaceResponseFromCoder(payload map[string]any) WorkspaceResponse {
	latest := obj(payload, "latest_build")
	job := obj(latest, "job")
	agent := coder.PickAgent(payload)
	status := str(job, "status")
	if status == "" {
		status = "unknown"
	}
	return WorkspaceResponse{
		ID:         str(payload, "id"),
		Name:       str(payload, "name"),
		TemplateID: optString(payload, "template_id"),
		LatestBuild: BuildSummary{
			ID:          str(latest, "id"),
			Status:      status,
			BuildNumber: optInt(latest, "build_number"),
			Transition:  optString(latest, "transition"),
		},
		AgentLifecycleState: optString(agent, "lifecycle_state"),
		StartupReady:        coder.IsAgentReady(agent),
	}
}

// WorkspaceBuildResponseFromCoder mirrors WorkspaceBuildResponse.from_coder.
func WorkspaceBuildResponseFromCoder(payload map[string]any) WorkspaceBuildResponse {
	job := obj(payload, "job")
	status := str(job, "status")
	if status == "" {
		status = "unknown"
	}
	return WorkspaceBuildResponse{
		ID:          str(payload, "id"),
		WorkspaceID: str(payload, "workspace_id"),
		BuildNumber: intOr(payload, "build_number", 0),
		Status:      status,
		Transition:  optString(payload, "transition"),
		JobError:    optString(obj(payload, "job"), "error"),
		CreatedAt:   optString(payload, "created_at"),
	}
}

func ProvisionerJobLogFromCoder(entry map[string]any) ProvisionerJobLog {
	return ProvisionerJobLog{
		ID:        intOr(entry, "id", 0),
		CreatedAt: optString(entry, "created_at"),
		LogLevel:  optString(entry, "log_level"),
		LogSource: optString(entry, "log_source"),
		Output:    optString(entry, "output"),
		Stage:     optString(entry, "stage"),
	}
}

func WorkspaceAgentLogFromCoder(entry map[string]any) WorkspaceAgentLog {
	return WorkspaceAgentLog{
		ID:        intOr(entry, "id", 0),
		CreatedAt: optString(entry, "created_at"),
		Level:     optString(entry, "level"),
		Output:    optString(entry, "output"),
		SourceID:  optString(entry, "source_id"),
	}
}

// WorkspaceAccess mirrors main.py _workspace_access.
func WorkspaceAccess(
	workspace, user map[string]any, dashboard string, settings *config.Settings,
) WorkspaceAccessResponse {
	name := str(workspace, "name")
	appBase := dashboard
	if !settings.UsesSSOAccess() {
		appBase = coder.PreferCoderAppBase(settings.CoderAPIBase(), dashboard)
	}
	username := str(user, "username")
	started := coder.IsStarted(workspace)
	agent := coder.PickAgent(workspace)
	ready := started && coder.IsAgentReady(agent)

	access := WorkspaceAccessResponse{
		WorkspaceID:         str(workspace, "id"),
		WorkspaceName:       name,
		Username:            username,
		Started:             started,
		StartupReady:        ready,
		AgentLifecycleState: optString(agent, "lifecycle_state"),
		AgentID:             optString(agent, "id"),
		AgentName:           optString(agent, "name"),
	}
	if agent != nil {
		if directory := str(agent, "expanded_directory"); directory != "" {
			access.AgentDirectory = &directory
		} else if directory := str(agent, "directory"); directory != "" {
			access.AgentDirectory = &directory
		}
	}
	codeApp := coder.FindApp(agent, "code-server")
	apps := coder.DisplayApps(agent)
	hasVSCodeDesktop := false
	for _, app := range apps {
		if app == "vscode" || app == "vscode_insiders" {
			hasVSCodeDesktop = true
			break
		}
	}
	if ready && access.AgentName != nil && codeApp != nil {
		slug := str(codeApp, "slug")
		if slug == "" {
			slug = "code-server"
		}
		url := coder.BuildVSCodeBrowserURL(appBase, username, name, *access.AgentName, slug)
		access.VSCodeBrowserURL = &url
	}
	if ready && access.AgentID != nil && settings.UsesSSOAccess() {
		agentName := ""
		if access.AgentName != nil {
			agentName = *access.AgentName
		}
		url := coder.BuildWebTerminalURL(dashboard, username, name, agentName)
		access.TerminalURL = &url
	}
	access.HasTerminal = ready && access.AgentID != nil
	access.HasVSCodeBrowser = ready && codeApp != nil
	access.HasVSCodeDesktop = ready && hasVSCodeDesktop
	if codeApp != nil {
		access.CodeServerSlug = optString(codeApp, "slug")
	}
	return access
}
