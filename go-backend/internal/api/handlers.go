package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/malmonte/go-backend/internal/coder"
)

// Handler assembles the full HTTP handler: routes plus the security-header
// and CORS middleware, mirroring backend/app/main.py.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /ready", a.handleReady)
	mux.HandleFunc("POST /auth", a.handleAuth)
	mux.HandleFunc("GET /me", a.handleMe)
	mux.HandleFunc("POST /workspaces", a.handleCreateWorkspace)
	mux.HandleFunc("GET /workspaces", a.handleListWorkspaces)
	mux.HandleFunc("GET /workspaces/{name}", a.handleGetWorkspace)
	mux.HandleFunc("DELETE /workspaces/{name}", a.handleDeleteWorkspace)
	mux.HandleFunc("GET /workspaces/{name}/access", a.handleWorkspaceAccess)
	mux.HandleFunc("GET /workspaces/{name}/open/code-server", a.handleOpenCodeServer)
	mux.HandleFunc("POST /workspaces/{name}/vscode-desktop", a.handleVSCodeDesktop)
	mux.HandleFunc("GET /workspaces/{name}/terminal", a.handleTerminal)
	mux.HandleFunc("GET /workspaces/{name}/startup-logs", a.handleStartupLogs)
	mux.HandleFunc("GET /workspacebuilds/{build_id}", a.handleGetWorkspaceBuild)
	mux.HandleFunc("GET /workspacebuilds/{build_id}/logs", a.handleWorkspaceBuildLogs)

	// FastAPI renders unmatched routes as JSON; match that shape.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "Not Found"})
	})

	return SecurityHeaders(CORS(a.Settings, mux))
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "ok",
		"coder_url": a.Settings.CoderAPIBase(),
	})
}

func (a *App) handleReady(w http.ResponseWriter, r *http.Request) {
	if !a.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "Not ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleAuth mints a Coder token for a user email (SSO simulation).
func (a *App) handleAuth(w http.ResponseWriter, r *http.Request) {
	var body AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, coder.NewAPIError(http.StatusUnprocessableEntity, "Invalid request body"))
		return
	}
	email := strings.TrimSpace(body.Email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		writeError(w, coder.NewAPIError(http.StatusUnprocessableEntity,
			"value is not a valid email address"))
		return
	}
	sessionToken, err := a.mintTokenForEmail(r.Context(), address.Address)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, AuthResponse{SessionToken: sessionToken})
}

// mintTokenForEmail authenticates as the configured owner and mints an API
// token for the Coder user with the given email (SSO simulation).
func (a *App) mintTokenForEmail(ctx context.Context, email string) (string, error) {
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
	return client.CreateTokenForUser(ctx, str(user, "id"), "coder-service-"+randomSuffix(), 0)
}

// randomSuffix mirrors Python's uuid4().hex[:12].
func randomSuffix() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	token, err := requireSessionToken(r)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx := r.Context()
	client := a.Coder(token)
	user, err := client.Me(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	dashboard, err := a.DashboardURL(ctx, client)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, MeResponse{
		Username:     str(user, "username"),
		Email:        optString(user, "email"),
		DashboardURL: dashboard,
	})
}

func (a *App) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	token, err := requireSessionToken(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body CreateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, coder.NewAPIError(http.StatusUnprocessableEntity, "Invalid request body"))
		return
	}
	if err := body.Validate(); err != nil {
		writeError(w, err)
		return
	}
	payload, err := a.Coder(token).CreateWorkspace(r.Context(), body.ToCoderBody())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WorkspaceResponseFromCoder(payload))
}

func (a *App) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	token, err := requireSessionToken(r)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx := r.Context()
	client := a.Coder(token)
	payload, err := client.ListWorkspaces(ctx, "me")
	if err != nil {
		writeError(w, err)
		return
	}
	user, err := client.Me(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	dashboard, err := a.DashboardURL(ctx, client)
	if err != nil {
		writeError(w, err)
		return
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
	writeJSON(w, http.StatusOK, WorkspaceListResponse{
		Count:      intOr(payload, "count", int64(len(workspaces))),
		Workspaces: workspaces,
	})
}

func (a *App) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	token, err := requireSessionToken(r)
	if err != nil {
		writeError(w, err)
		return
	}
	payload, err := a.Coder(token).GetWorkspace(r.Context(), r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WorkspaceResponseFromCoder(payload))
}

func (a *App) handleWorkspaceAccess(w http.ResponseWriter, r *http.Request) {
	token, err := requireSessionToken(r)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx := r.Context()
	client := a.Coder(token)
	workspace, err := client.GetWorkspace(ctx, r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	user, err := client.Me(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	dashboard, err := a.DashboardURL(ctx, client)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WorkspaceAccess(workspace, user, dashboard, a.Settings))
}

func (a *App) handleOpenCodeServer(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		token = sessionToken(r)
	}
	if token == "" {
		writeError(w, coder.NewAPIError(http.StatusUnauthorized,
			"Missing session token (query token= or Coder-Session-Token header)"))
		return
	}
	target, err := a.codeServerRedirectTarget(r.Context(), token, r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	http.Redirect(w, r, target+separator+"coder_session_token="+token, http.StatusTemporaryRedirect)
}

// codeServerRedirectTarget resolves the code-server URL for a workspace,
// mirroring the checks in main.py open_code_server.
func (a *App) codeServerRedirectTarget(ctx context.Context, token, name string) (string, error) {
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

func (a *App) handleVSCodeDesktop(w http.ResponseWriter, r *http.Request) {
	token, err := requireSessionToken(r)
	if err != nil {
		writeError(w, err)
		return
	}
	uri, err := a.vscodeDesktopURI(r.Context(), token, r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, VSCodeDesktopResponse{URI: uri})
}

// vscodeDesktopURI mints a scoped token and builds the vscode:// deep link.
func (a *App) vscodeDesktopURI(ctx context.Context, token, name string) (string, error) {
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
		"coder-service-vscode-"+randomSuffix(), lifetimeNs)
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

func (a *App) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	token, err := requireSessionToken(r)
	if err != nil {
		writeError(w, err)
		return
	}
	orphan := r.URL.Query().Get("orphan") == "true"
	payload, err := a.Coder(token).DeleteWorkspace(r.Context(), r.PathValue("name"), orphan)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, WorkspaceBuildResponseFromCoder(payload))
}

func (a *App) handleGetWorkspaceBuild(w http.ResponseWriter, r *http.Request) {
	token, err := requireSessionToken(r)
	if err != nil {
		writeError(w, err)
		return
	}
	buildID, err := parseUUID(r.PathValue("build_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	payload, err := a.Coder(token).GetWorkspaceBuild(r.Context(), buildID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WorkspaceBuildResponseFromCoder(payload))
}

func (a *App) handleWorkspaceBuildLogs(w http.ResponseWriter, r *http.Request) {
	token, err := requireSessionToken(r)
	if err != nil {
		writeError(w, err)
		return
	}
	buildID, err := parseUUID(r.PathValue("build_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	after, err := optIntQuery(r, "after")
	if err != nil {
		writeError(w, err)
		return
	}
	before, err := optIntQuery(r, "before")
	if err != nil {
		writeError(w, err)
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "text" {
		writeError(w, coder.NewAPIError(http.StatusUnprocessableEntity,
			"format must be 'json' or 'text'"))
		return
	}
	result, err := a.Coder(token).GetWorkspaceBuildLogs(r.Context(), buildID, after, before, format)
	if err != nil {
		writeError(w, err)
		return
	}
	if format == "text" {
		text, _ := result.(string)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(text))
		return
	}
	entries, _ := result.([]any)
	logs := make([]ProvisionerJobLog, 0, len(entries))
	for _, entry := range entries {
		if log, ok := entry.(map[string]any); ok {
			logs = append(logs, ProvisionerJobLogFromCoder(log))
		}
	}
	writeJSON(w, http.StatusOK, logs)
}

func (a *App) handleStartupLogs(w http.ResponseWriter, r *http.Request) {
	token, err := requireSessionToken(r)
	if err != nil {
		writeError(w, err)
		return
	}
	after, err := optIntQuery(r, "after")
	if err != nil {
		writeError(w, err)
		return
	}
	ctx := r.Context()
	client := a.Coder(token)
	workspace, err := client.GetWorkspace(ctx, r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}
	agent := coder.PickAgent(workspace)
	if agent == nil || str(agent, "id") == "" {
		writeError(w, coder.NewAPIError(http.StatusNotFound,
			"No agent found on workspace (startup logs appear after the agent starts)"))
		return
	}
	entries, err := client.GetAgentLogs(ctx, str(agent, "id"), after)
	if err != nil {
		writeError(w, err)
		return
	}
	logs := make([]WorkspaceAgentLog, 0, len(entries))
	for _, entry := range entries {
		if log, ok := entry.(map[string]any); ok {
			logs = append(logs, WorkspaceAgentLogFromCoder(log))
		}
	}
	writeJSON(w, http.StatusOK, logs)
}

func parseUUID(raw string) (string, error) {
	if _, err := uuid.Parse(raw); err != nil {
		return "", coder.NewAPIError(http.StatusUnprocessableEntity,
			fmt.Sprintf("Invalid UUID: %s", raw))
	}
	return raw, nil
}

func optIntQuery(r *http.Request, name string) (*int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
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
