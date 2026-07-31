package api

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/malmonte/go-coder-service/internal/coder"
)

// Register mounts the JSON API routes (mirroring backend/app/main.py) on g.
func Register(g *echo.Group, app *App) {
	g.GET("/health", app.handleHealth)
	g.GET("/ready", app.handleReady)
	g.POST("/auth", app.handleAuth)
	g.GET("/me", app.handleMe)
	g.POST("/workspaces", app.handleCreateWorkspace)
	g.GET("/workspaces", app.handleListWorkspaces)
	g.GET("/workspaces/:name", app.handleGetWorkspace)
	g.GET("/workspaces/:name/access", app.handleWorkspaceAccess)
	g.GET("/workspaces/:name/open/code-server", app.handleOpenCodeServer)
	g.POST("/workspaces/:name/vscode-desktop", app.handleVSCodeDesktop)
	g.GET("/workspaces/:name/terminal", app.handleTerminal)
	g.DELETE("/workspaces/:name", app.handleDeleteWorkspace)
	g.GET("/workspacebuilds/:build_id", app.handleGetWorkspaceBuild)
	g.GET("/workspacebuilds/:build_id/logs", app.handleWorkspaceBuildLogs)
	g.GET("/workspaces/:name/startup-logs", app.handleStartupLogs)
}

func (a *App) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status":    "ok",
		"coder_url": a.Settings.CoderAPIBase(),
	})
}

func (a *App) handleReady(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
}

// handleAuth mints a Coder token for a user email (SSO simulation).
func (a *App) handleAuth(c echo.Context) error {
	var body AuthRequest
	if err := c.Bind(&body); err != nil {
		return WriteError(c, coder.NewAPIError(http.StatusUnprocessableEntity, "Invalid request body"))
	}
	address, err := mail.ParseAddress(strings.TrimSpace(body.Email))
	if err != nil || address.Address != strings.TrimSpace(body.Email) {
		return WriteError(c, coder.NewAPIError(http.StatusUnprocessableEntity,
			"value is not a valid email address"))
	}
	sessionToken, err := a.MintTokenForEmail(c.Request().Context(), address.Address)
	if err != nil {
		return WriteError(c, err)
	}
	return c.JSON(http.StatusCreated, AuthResponse{SessionToken: sessionToken})
}

// MintTokenForEmail authenticates as the configured owner and mints an API
// token for the Coder user with the given email (SSO simulation).
func (a *App) MintTokenForEmail(ctx context.Context, email string) (string, error) {
	ownerToken := a.Settings.OwnerSessionToken()
	if ownerToken == "" {
		return "", coder.NewAPIError(http.StatusInternalServerError,
			"Owner token missing: set coder_session_token in .env")
	}
	client := a.Coder(ownerToken)
	user, err := client.FindUserByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	tokenName := "coder-service-" + uuid.NewString()[:12]
	return client.CreateTokenForUser(ctx, str(user, "id"), tokenName, 0)
}

func (a *App) handleMe(c echo.Context) error {
	token, err := RequireSessionToken(c)
	if err != nil {
		return WriteError(c, err)
	}
	ctx := c.Request().Context()
	client := a.Coder(token)
	user, err := client.Me(ctx)
	if err != nil {
		return WriteError(c, err)
	}
	dashboard, err := a.DashboardURL(ctx, client)
	if err != nil {
		return WriteError(c, err)
	}
	return c.JSON(http.StatusOK, MeResponse{
		Username:     str(user, "username"),
		Email:        optString(user, "email"),
		DashboardURL: dashboard,
	})
}

func (a *App) handleCreateWorkspace(c echo.Context) error {
	token, err := RequireSessionToken(c)
	if err != nil {
		return WriteError(c, err)
	}
	var body CreateWorkspaceRequest
	if err := c.Bind(&body); err != nil {
		return WriteError(c, coder.NewAPIError(http.StatusUnprocessableEntity, "Invalid request body"))
	}
	if err := body.Validate(); err != nil {
		return WriteError(c, err)
	}
	payload, err := a.Coder(token).CreateWorkspace(c.Request().Context(), body.ToCoderBody())
	if err != nil {
		return WriteError(c, err)
	}
	return c.JSON(http.StatusCreated, WorkspaceResponseFromCoder(payload))
}

// ListWorkspacesWithAccess is shared by the JSON API and the HTML UI.
func (a *App) ListWorkspacesWithAccess(c echo.Context, token string) (*WorkspaceListResponse, error) {
	ctx := c.Request().Context()
	client := a.Coder(token)
	payload, err := client.ListWorkspaces(ctx, "me")
	if err != nil {
		return nil, err
	}
	user, err := client.Me(ctx)
	if err != nil {
		return nil, err
	}
	dashboard, err := a.DashboardURL(ctx, client)
	if err != nil {
		return nil, err
	}
	items, _ := payload["workspaces"].([]any)
	workspaces := make([]WorkspaceResponse, 0, len(items))
	for _, item := range items {
		workspace, ok := item.(map[string]any)
		if !ok {
			continue
		}
		response := WorkspaceResponseFromCoder(workspace)
		access := WorkspaceAccess(workspace, user, dashboard, a.Settings)
		response.Access = &access
		workspaces = append(workspaces, response)
	}
	return &WorkspaceListResponse{
		Count:      intOr(payload, "count", int64(len(workspaces))),
		Workspaces: workspaces,
	}, nil
}

func (a *App) handleListWorkspaces(c echo.Context) error {
	token, err := RequireSessionToken(c)
	if err != nil {
		return WriteError(c, err)
	}
	list, err := a.ListWorkspacesWithAccess(c, token)
	if err != nil {
		return WriteError(c, err)
	}
	return c.JSON(http.StatusOK, list)
}

func (a *App) handleGetWorkspace(c echo.Context) error {
	token, err := RequireSessionToken(c)
	if err != nil {
		return WriteError(c, err)
	}
	payload, err := a.Coder(token).GetWorkspace(c.Request().Context(), c.Param("name"))
	if err != nil {
		return WriteError(c, err)
	}
	return c.JSON(http.StatusOK, WorkspaceResponseFromCoder(payload))
}

func (a *App) handleWorkspaceAccess(c echo.Context) error {
	token, err := RequireSessionToken(c)
	if err != nil {
		return WriteError(c, err)
	}
	access, err := a.WorkspaceAccessFor(c, token, c.Param("name"))
	if err != nil {
		return WriteError(c, err)
	}
	return c.JSON(http.StatusOK, access)
}

// WorkspaceAccessFor is shared by the JSON API and the HTML UI.
func (a *App) WorkspaceAccessFor(c echo.Context, token, name string) (*WorkspaceAccessResponse, error) {
	ctx := c.Request().Context()
	client := a.Coder(token)
	workspace, err := client.GetWorkspace(ctx, name)
	if err != nil {
		return nil, err
	}
	user, err := client.Me(ctx)
	if err != nil {
		return nil, err
	}
	dashboard, err := a.DashboardURL(ctx, client)
	if err != nil {
		return nil, err
	}
	access := WorkspaceAccess(workspace, user, dashboard, a.Settings)
	return &access, nil
}

// CodeServerRedirectTarget resolves the code-server URL for a workspace,
// mirroring the checks in main.py open_code_server.
func (a *App) CodeServerRedirectTarget(c echo.Context, token, name string) (string, error) {
	ctx := c.Request().Context()
	client := a.Coder(token)
	workspace, err := client.GetWorkspace(ctx, name)
	if err != nil {
		return "", err
	}
	user, err := client.Me(ctx)
	if err != nil {
		return "", err
	}
	dashboard, err := a.DashboardURL(ctx, client)
	if err != nil {
		return "", err
	}
	if !coder.IsStarted(workspace) {
		return "", coder.NewAPIError(http.StatusConflict, "Workspace is not started")
	}
	agent := coder.PickAgent(workspace)
	if !coder.IsAgentReady(agent) {
		return "", coder.NewAPIError(http.StatusConflict,
			"Startup script is still running; try again when the agent is ready")
	}
	codeApp := coder.FindApp(agent, "code-server")
	if agent == nil || codeApp == nil {
		return "", coder.NewAPIError(http.StatusNotFound, "code-server app not found")
	}
	appBase := coder.PreferCoderAppBase(a.Settings.CoderAPIBase(), dashboard)
	slug := str(codeApp, "slug")
	if slug == "" {
		slug = "code-server"
	}
	return coder.BuildVSCodeBrowserURL(appBase, str(user, "username"), name, str(agent, "name"), slug), nil
}

func (a *App) handleOpenCodeServer(c echo.Context) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	if token == "" {
		token = SessionToken(c)
	}
	if token == "" {
		return WriteError(c, coder.NewAPIError(http.StatusUnauthorized,
			"Missing session token (query token= or Coder-Session-Token header)"))
	}
	target, err := a.CodeServerRedirectTarget(c, token, c.Param("name"))
	if err != nil {
		return WriteError(c, err)
	}
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	return c.Redirect(http.StatusTemporaryRedirect, target+separator+"coder_session_token="+token)
}

// VSCodeDesktopURI mints a scoped token and builds the vscode:// deep link.
func (a *App) VSCodeDesktopURI(c echo.Context, token, name string) (string, error) {
	ctx := c.Request().Context()
	client := a.Coder(token)
	workspace, err := client.GetWorkspace(ctx, name)
	if err != nil {
		return "", err
	}
	user, err := client.Me(ctx)
	if err != nil {
		return "", err
	}
	dashboard, err := a.DashboardURL(ctx, client)
	if err != nil {
		return "", err
	}
	if !coder.IsStarted(workspace) {
		return "", coder.NewAPIError(http.StatusConflict, "Workspace is not started")
	}
	agent := coder.PickAgent(workspace)
	if agent == nil {
		return "", coder.NewAPIError(http.StatusNotFound, "No agent found")
	}
	if !coder.IsAgentReady(agent) {
		return "", coder.NewAPIError(http.StatusConflict,
			"Startup script is still running; try again when the agent is ready")
	}
	hasDesktop := false
	for _, app := range coder.DisplayApps(agent) {
		if app == "vscode" || app == "vscode_insiders" {
			hasDesktop = true
			break
		}
	}
	if !hasDesktop {
		return "", coder.NewAPIError(http.StatusNotFound,
			"VS Code Desktop is not enabled for this workspace agent")
	}
	lifetimeNs := int64(a.Settings.VSCodeDesktopTokenLifetimeHours) * 3_600_000_000_000
	apiKey, err := client.CreateTokenForUser(ctx, str(user, "id"),
		"coder-service-vscode-"+uuid.NewString()[:12], lifetimeNs)
	if err != nil {
		return "", err
	}
	folder := str(agent, "expanded_directory")
	if folder == "" {
		folder = str(agent, "directory")
	}
	return coder.BuildVSCodeDesktopURI(
		dashboard, str(user, "username"), name, apiKey, str(agent, "name"), folder, "vscode"), nil
}

func (a *App) handleVSCodeDesktop(c echo.Context) error {
	token, err := RequireSessionToken(c)
	if err != nil {
		return WriteError(c, err)
	}
	uri, err := a.VSCodeDesktopURI(c, token, c.Param("name"))
	if err != nil {
		return WriteError(c, err)
	}
	return c.JSON(http.StatusOK, VSCodeDesktopResponse{URI: uri})
}

func (a *App) handleDeleteWorkspace(c echo.Context) error {
	token, err := RequireSessionToken(c)
	if err != nil {
		return WriteError(c, err)
	}
	orphan := c.QueryParam("orphan") == "true"
	payload, err := a.Coder(token).DeleteWorkspace(c.Request().Context(), c.Param("name"), orphan)
	if err != nil {
		return WriteError(c, err)
	}
	return c.JSON(http.StatusAccepted, WorkspaceBuildResponseFromCoder(payload))
}

func (a *App) handleGetWorkspaceBuild(c echo.Context) error {
	token, err := RequireSessionToken(c)
	if err != nil {
		return WriteError(c, err)
	}
	buildID, err := parseUUIDParam(c, "build_id")
	if err != nil {
		return WriteError(c, err)
	}
	payload, err := a.Coder(token).GetWorkspaceBuild(c.Request().Context(), buildID)
	if err != nil {
		return WriteError(c, err)
	}
	return c.JSON(http.StatusOK, WorkspaceBuildResponseFromCoder(payload))
}

func (a *App) handleWorkspaceBuildLogs(c echo.Context) error {
	token, err := RequireSessionToken(c)
	if err != nil {
		return WriteError(c, err)
	}
	buildID, err := parseUUIDParam(c, "build_id")
	if err != nil {
		return WriteError(c, err)
	}
	after, err := optIntQuery(c, "after")
	if err != nil {
		return WriteError(c, err)
	}
	before, err := optIntQuery(c, "before")
	if err != nil {
		return WriteError(c, err)
	}
	format := c.QueryParam("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "text" {
		return WriteError(c, coder.NewAPIError(http.StatusUnprocessableEntity,
			"format must be 'json' or 'text'"))
	}
	result, err := a.Coder(token).GetWorkspaceBuildLogs(
		c.Request().Context(), buildID, after, before, format)
	if err != nil {
		return WriteError(c, err)
	}
	if format == "text" {
		text, _ := result.(string)
		return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(text))
	}
	entries, _ := result.([]any)
	logs := make([]ProvisionerJobLog, 0, len(entries))
	for _, entry := range entries {
		if log, ok := entry.(map[string]any); ok {
			logs = append(logs, ProvisionerJobLogFromCoder(log))
		}
	}
	return c.JSON(http.StatusOK, logs)
}

// StartupLogsFor is shared by the JSON API and the HTML UI.
func (a *App) StartupLogsFor(c echo.Context, token, name string, after *int64) ([]WorkspaceAgentLog, error) {
	ctx := c.Request().Context()
	client := a.Coder(token)
	workspace, err := client.GetWorkspace(ctx, name)
	if err != nil {
		return nil, err
	}
	agent := coder.PickAgent(workspace)
	if agent == nil || str(agent, "id") == "" {
		return nil, coder.NewAPIError(http.StatusNotFound,
			"No agent found on workspace (startup logs appear after the agent starts)")
	}
	entries, err := client.GetAgentLogs(ctx, str(agent, "id"), after)
	if err != nil {
		return nil, err
	}
	logs := make([]WorkspaceAgentLog, 0, len(entries))
	for _, entry := range entries {
		if log, ok := entry.(map[string]any); ok {
			logs = append(logs, WorkspaceAgentLogFromCoder(log))
		}
	}
	return logs, nil
}

func (a *App) handleStartupLogs(c echo.Context) error {
	token, err := RequireSessionToken(c)
	if err != nil {
		return WriteError(c, err)
	}
	after, err := optIntQuery(c, "after")
	if err != nil {
		return WriteError(c, err)
	}
	logs, err := a.StartupLogsFor(c, token, c.Param("name"), after)
	if err != nil {
		return WriteError(c, err)
	}
	return c.JSON(http.StatusOK, logs)
}

func parseUUIDParam(c echo.Context, name string) (string, error) {
	raw := c.Param(name)
	if _, err := uuid.Parse(raw); err != nil {
		return "", coder.NewAPIError(http.StatusUnprocessableEntity,
			fmt.Sprintf("Invalid UUID: %s", raw))
	}
	return raw, nil
}

func optIntQuery(c echo.Context, name string) (*int64, error) {
	raw := strings.TrimSpace(c.QueryParam(name))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, coder.NewAPIError(http.StatusUnprocessableEntity,
			fmt.Sprintf("Invalid integer for %s: %s", name, raw))
	}
	return &value, nil
}
