// Package web serves the htmx/Tailwind UI, porting the React SPA
// (frontend/src) onto server-rendered templates.
package web

import (
	"embed"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/malmonte/go-coder-service/internal/api"
	"github.com/malmonte/go-coder-service/internal/coder"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Renderer struct {
	templates *template.Template
}

func (r *Renderer) Render(w io.Writer, name string, data any, _ echo.Context) error {
	return r.templates.ExecuteTemplate(w, name, data)
}

type Handlers struct {
	app *api.App
}

func Register(e *echo.Echo, app *api.App) error {
	templates, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return err
	}
	e.Renderer = &Renderer{templates: templates}

	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	e.StaticFS("/static", static)

	h := &Handlers{app: app}
	e.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusFound, "/workspaces")
	})
	e.GET("/login", h.loginPage)
	e.POST("/login", h.loginSubmit)
	e.POST("/logout", h.logout)
	e.GET("/workspaces", h.workspacesPage)
	e.POST("/workspaces/create", h.createWorkspace)
	e.POST("/workspaces/:name/delete", h.deleteWorkspace)
	e.POST("/workspaces/:name/vscode-desktop-open", h.openVSCodeDesktop)
	e.GET("/workspaces/:name/open/code-server", h.openCodeServer)
	e.GET("/workspaces/:name/terminal", h.terminalPage)
	e.GET("/fragments/workspaces", h.workspaceListFragment)
	e.GET("/fragments/name-suggestion", h.nameSuggestionFragment)
	e.GET("/fragments/workspaces/:name/build-logs/:build_id", h.buildLogsFragment)
	e.GET("/fragments/workspaces/:name/startup-logs", h.startupLogsFragment)
	return nil
}

func (h *Handlers) sessionToken(c echo.Context) string {
	cookie, err := c.Cookie(api.SessionCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func setSessionCookie(c echo.Context, token string) {
	cookie := &http.Cookie{
		Name:     api.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.Scheme() == "https",
	}
	if token == "" {
		cookie.MaxAge = -1
	}
	c.SetCookie(cookie)
}

func errorDetail(err error, fallback string) string {
	var apiErr *coder.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Detail
	}
	return fallback
}

func isUnauthorized(err error) bool {
	var apiErr *coder.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized
}

// --- pages ---

func (h *Handlers) loginPage(c echo.Context) error {
	if h.sessionToken(c) != "" {
		return c.Redirect(http.StatusFound, "/workspaces")
	}
	return c.Render(http.StatusOK, "login.html", nil)
}

func (h *Handlers) loginSubmit(c echo.Context) error {
	email := strings.TrimSpace(c.FormValue("email"))
	token, err := h.app.MintTokenForEmail(c.Request().Context(), email)
	if err != nil {
		return c.Render(http.StatusOK, "alert_error.html",
			errorDetail(err, "Unable to sign in. Try again."))
	}
	setSessionCookie(c, token)
	c.Response().Header().Set("HX-Redirect", "/workspaces")
	return c.NoContent(http.StatusOK)
}

func (h *Handlers) logout(c echo.Context) error {
	setSessionCookie(c, "")
	c.Response().Header().Set("HX-Redirect", "/login")
	return c.NoContent(http.StatusOK)
}

type nameSuggestionData struct {
	Suggestion string
	Applied    string
}

type workspacesPageData struct {
	List           *listData
	NameSuggestion nameSuggestionData
	TemplateName   string
	CreateOpen     bool
}

func (h *Handlers) workspacesPage(c echo.Context) error {
	token := h.sessionToken(c)
	if token == "" {
		return c.Redirect(http.StatusFound, "/login")
	}
	list, err := h.listData(c, token, c.QueryParam("expanded"), c.QueryParam("logs"))
	if err != nil {
		if isUnauthorized(err) {
			setSessionCookie(c, "")
			return c.Redirect(http.StatusFound, "/login")
		}
		list = &listData{Error: errorDetail(err, "Failed to load workspaces")}
	}
	return c.Render(http.StatusOK, "workspaces.html", workspacesPageData{
		List:           list,
		NameSuggestion: nameSuggestionData{Suggestion: GenerateWorkspaceName()},
		TemplateName:   h.app.Settings.DefaultTemplateName,
		CreateOpen:     c.QueryParam("create") == "open",
	})
}

// --- workspace list fragment ---

type cardData struct {
	api.WorkspaceResponse
	Expanded          bool
	BuildActive       bool
	Started           bool
	Starting          bool
	Ready             bool
	WaitingForStartup bool
	ConnectionStatus  string
	StatusTone        string
	BuildNumberLabel  string
	Transition        string
	TerminalHref      string
	VSCodeWebHref     string
	HasTerminal       bool
	HasVSCodeBrowser  bool
	HasVSCodeDesktop  bool
	LogsActive        bool
	BuildLogsAutoOpen bool

	// Server-driven accordion state: open flags resolved from the logs query
	// param, plus the param value each toggle link should submit.
	BuildKey          string
	StartupKey        string
	BuildLogsOpen     bool
	StartupLogsOpen   bool
	BuildToggleLogs   string
	StartupToggleLogs string
}

type listData struct {
	Workspaces  []cardData
	Count       int
	Expanded    string
	LogsParam   string
	PollNeeded  bool
	Error       string
	DeleteError string
}

// parseLogOverrides reads the logs query param ("b-<id>:1,s-<name>:0") into
// explicit user open/close choices for the log accordions.
func parseLogOverrides(raw string) map[string]bool {
	overrides := map[string]bool{}
	for _, entry := range strings.Split(raw, ",") {
		if key, value, found := strings.Cut(entry, ":"); found && key != "" {
			overrides[key] = value == "1"
		}
	}
	return overrides
}

func serializeLogOverrides(overrides map[string]bool) string {
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		value := "0"
		if overrides[key] {
			value = "1"
		}
		parts = append(parts, key+":"+value)
	}
	return strings.Join(parts, ",")
}

func serializeWithOverride(overrides map[string]bool, key string, value bool) string {
	next := make(map[string]bool, len(overrides)+1)
	for k, v := range overrides {
		next[k] = v
	}
	next[key] = value
	return serializeLogOverrides(next)
}

func isActiveBuildStatus(status string) bool {
	return status == "pending" || status == "running"
}

func statusTone(status string) string {
	switch status {
	case "succeeded":
		return "bg-accent-soft text-ok"
	case "running", "pending":
		return "bg-amber-100 text-warn"
	case "failed", "canceled":
		return "bg-danger-soft text-danger"
	default:
		return "bg-surface text-ink-muted"
	}
}

func (h *Handlers) listData(c echo.Context, token, expanded, logsRaw string) (*listData, error) {
	list, err := h.app.ListWorkspacesWithAccess(c, token)
	if err != nil {
		return nil, err
	}
	overrides := parseLogOverrides(logsRaw)
	sorted := make([]api.WorkspaceResponse, len(list.Workspaces))
	copy(sorted, list.Workspaces)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
	})

	data := &listData{Count: len(sorted), Expanded: expanded}
	for _, ws := range sorted {
		build := ws.LatestBuild
		card := cardData{
			WorkspaceResponse: ws,
			Expanded:          expanded == ws.ID,
			BuildActive:       isActiveBuildStatus(build.Status),
			StatusTone:        statusTone(build.Status),
			BuildNumberLabel:  "—",
		}
		if build.BuildNumber != nil {
			card.BuildNumberLabel = itoa64(*build.BuildNumber)
		}
		if build.Transition != nil {
			card.Transition = *build.Transition
		}
		card.Started = build.Status == "succeeded" && card.Transition == "start"
		card.Starting = (card.Transition == "start" && card.BuildActive) ||
			(card.Started && !ws.StartupReady)
		if access := ws.Access; access != nil {
			card.Ready = access.StartupReady
			card.WaitingForStartup = card.Started && !access.StartupReady
			card.HasTerminal = access.HasTerminal
			card.HasVSCodeBrowser = access.HasVSCodeBrowser
			card.HasVSCodeDesktop = access.HasVSCodeDesktop
			if access.TerminalURL != nil {
				// SSO mode: Coder-hosted terminal and code-server URLs.
				card.TerminalHref = *access.TerminalURL
				if access.VSCodeBrowserURL != nil {
					card.VSCodeWebHref = *access.VSCodeBrowserURL
				}
			} else {
				card.TerminalHref = "/workspaces/" + ws.Name + "/terminal"
				card.VSCodeWebHref = "/workspaces/" + ws.Name + "/open/code-server"
			}
		}
		switch {
		case card.WaitingForStartup:
			card.ConnectionStatus = "Preparing tools…"
		case card.Ready:
			card.ConnectionStatus = "Ready to connect"
		case card.BuildActive:
			card.ConnectionStatus = "Build in progress"
		default:
			card.ConnectionStatus = "Not available"
		}
		card.LogsActive = card.BuildActive
		card.BuildLogsAutoOpen = card.Transition != "delete"
		card.BuildKey = "b-" + build.ID
		card.StartupKey = "s-" + ws.Name
		if card.BuildActive || (card.Started && !ws.StartupReady) {
			data.PollNeeded = true
		}
		data.Workspaces = append(data.Workspaces, card)
	}

	// Keep only overrides for accordions that still exist, then resolve each
	// accordion's open state (explicit choice wins over the active default).
	valid := map[string]bool{}
	for _, card := range data.Workspaces {
		for _, key := range []string{card.BuildKey, card.StartupKey} {
			if value, ok := overrides[key]; ok {
				valid[key] = value
			}
		}
	}
	data.LogsParam = serializeLogOverrides(valid)
	for i := range data.Workspaces {
		card := &data.Workspaces[i]
		card.BuildLogsOpen = card.LogsActive && card.BuildLogsAutoOpen
		if value, ok := valid[card.BuildKey]; ok {
			card.BuildLogsOpen = value
		}
		card.StartupLogsOpen = card.Starting
		if value, ok := valid[card.StartupKey]; ok {
			card.StartupLogsOpen = value
		}
		card.BuildToggleLogs = serializeWithOverride(valid, card.BuildKey, !card.BuildLogsOpen)
		card.StartupToggleLogs = serializeWithOverride(valid, card.StartupKey, !card.StartupLogsOpen)
	}
	return data, nil
}

func (h *Handlers) workspaceListFragment(c echo.Context) error {
	token := h.sessionToken(c)
	if token == "" {
		c.Response().Header().Set("HX-Redirect", "/login")
		return c.NoContent(http.StatusOK)
	}
	list, err := h.listData(c, token, c.QueryParam("expanded"), c.QueryParam("logs"))
	if err != nil {
		if isUnauthorized(err) {
			setSessionCookie(c, "")
			c.Response().Header().Set("HX-Redirect", "/login")
			return c.NoContent(http.StatusOK)
		}
		list = &listData{Error: errorDetail(err, "Failed to load workspaces")}
	}
	return c.Render(http.StatusOK, "workspace_list.html", list)
}

func (h *Handlers) nameSuggestionFragment(c echo.Context) error {
	// apply carries the suggestion the user clicked; the response swaps the
	// name input out-of-band with that value and offers a fresh suggestion.
	return c.Render(http.StatusOK, "name_suggestion.html", nameSuggestionData{
		Suggestion: GenerateWorkspaceName(),
		Applied:    strings.TrimSpace(c.QueryParam("apply")),
	})
}

// --- actions ---

func (h *Handlers) createWorkspace(c echo.Context) error {
	token := h.sessionToken(c)
	if token == "" {
		c.Response().Header().Set("HX-Redirect", "/login")
		return c.NoContent(http.StatusOK)
	}
	templateID := h.app.Settings.DefaultTemplateID
	body := api.CreateWorkspaceRequest{
		Name:       strings.TrimSpace(c.FormValue("name")),
		TemplateID: &templateID,
		RichParameterValues: []api.RichParameterValue{
			{Name: "cpu", Value: strings.TrimSpace(c.FormValue("cpu"))},
			{Name: "memory", Value: strings.TrimSpace(c.FormValue("memory"))},
			{Name: "home_disk_size", Value: strings.TrimSpace(c.FormValue("disk"))},
		},
	}
	if err := body.Validate(); err != nil {
		return c.Render(http.StatusOK, "alert_error.html",
			errorDetail(err, "Failed to create workspace"))
	}
	payload, err := h.app.Coder(token).CreateWorkspace(c.Request().Context(), body.ToCoderBody())
	if err != nil {
		return c.Render(http.StatusOK, "alert_error.html",
			errorDetail(err, "Failed to create workspace"))
	}
	workspace := api.WorkspaceResponseFromCoder(payload)
	c.Response().Header().Set("HX-Redirect", "/workspaces?expanded="+workspace.ID)
	return c.NoContent(http.StatusOK)
}

func (h *Handlers) deleteWorkspace(c echo.Context) error {
	token := h.sessionToken(c)
	if token == "" {
		c.Response().Header().Set("HX-Redirect", "/login")
		return c.NoContent(http.StatusOK)
	}
	_, err := h.app.Coder(token).DeleteWorkspace(c.Request().Context(), c.Param("name"), false)
	list, listErr := h.listData(c, token, c.QueryParam("expanded"), c.QueryParam("logs"))
	if listErr != nil {
		list = &listData{Error: errorDetail(listErr, "Failed to load workspaces")}
	}
	if err != nil {
		list.DeleteError = errorDetail(err, "Failed to delete workspace")
	}
	return c.Render(http.StatusOK, "workspace_list.html", list)
}

func (h *Handlers) openVSCodeDesktop(c echo.Context) error {
	token := h.sessionToken(c)
	if token == "" {
		c.Response().Header().Set("HX-Redirect", "/login")
		return c.NoContent(http.StatusOK)
	}
	uri, err := h.app.VSCodeDesktopURI(c, token, c.Param("name"))
	if err != nil {
		return c.Render(http.StatusOK, "alert_error.html",
			errorDetail(err, "Failed to open VS Code Desktop"))
	}
	c.Response().Header().Set("HX-Redirect", uri)
	return c.NoContent(http.StatusOK)
}

func (h *Handlers) openCodeServer(c echo.Context) error {
	token := h.sessionToken(c)
	if token == "" {
		return c.Redirect(http.StatusFound, "/login")
	}
	target, err := h.app.CodeServerRedirectTarget(c, token, c.Param("name"))
	if err != nil {
		return c.String(http.StatusOK, errorDetail(err, "Failed to open VS Code"))
	}
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	return c.Redirect(http.StatusTemporaryRedirect, target+separator+"coder_session_token="+token)
}

// --- terminal ---

type terminalPageData struct {
	WorkspaceName string
	AgentID       string
}

func (h *Handlers) terminalPage(c echo.Context) error {
	name := c.Param("name")
	// Mirror the SPA: a token query param takes over the stored session.
	if token := strings.TrimSpace(c.QueryParam("token")); token != "" {
		setSessionCookie(c, token)
	} else if h.sessionToken(c) == "" {
		return c.Redirect(http.StatusFound, "/login")
	}
	token := strings.TrimSpace(c.QueryParam("token"))
	if token == "" {
		token = h.sessionToken(c)
	}
	// Best-effort agent lookup; the proxy can resolve the agent itself.
	agentID := ""
	if access, err := h.app.WorkspaceAccessFor(c, token, name); err == nil && access.AgentID != nil {
		agentID = *access.AgentID
	}
	return c.Render(http.StatusOK, "terminal.html", terminalPageData{
		WorkspaceName: name,
		AgentID:       agentID,
	})
}

// --- log fragments ---

type logLine struct {
	Prefix     string
	Output     string
	LevelClass string
}

type logsData struct {
	Lines        []logLine
	ErrorMessage string
	EmptyMessage string
}

func logLevelClass(level string) string {
	switch level {
	case "error":
		return "text-red-300"
	case "warn":
		return "text-amber-200"
	case "debug", "trace":
		return "text-slate-400"
	default:
		return "text-emerald-100/90"
	}
}

func (h *Handlers) buildLogsFragment(c echo.Context) error {
	token := h.sessionToken(c)
	if token == "" {
		return c.NoContent(http.StatusUnauthorized)
	}
	active := c.QueryParam("active") == "1"
	data := logsData{EmptyMessage: "No logs for this build."}
	if active {
		data.EmptyMessage = "Waiting for provisioner output…"
	}
	result, err := h.app.Coder(token).GetWorkspaceBuildLogs(
		c.Request().Context(), c.Param("build_id"), nil, nil, "json")
	if err != nil {
		data.ErrorMessage = errorDetail(err, "Failed to load build logs")
	} else {
		entries, _ := result.([]any)
		for _, entry := range entries {
			log, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			parsed := api.ProvisionerJobLogFromCoder(log)
			stage := "provision"
			if parsed.Stage != nil {
				stage = *parsed.Stage
			}
			level := ""
			if parsed.LogLevel != nil {
				level = *parsed.LogLevel
			}
			prefix := "[" + stage + "]"
			if level != "" {
				prefix += " " + level
			}
			output := ""
			if parsed.Output != nil {
				output = *parsed.Output
			}
			data.Lines = append(data.Lines, logLine{
				Prefix:     prefix,
				Output:     output,
				LevelClass: logLevelClass(level),
			})
		}
	}
	reverseLines(data.Lines)
	return c.Render(http.StatusOK, "log_lines.html", data)
}

// reverseLines orders newest-first: the scroller renders flex-col-reverse so
// the browser natively pins the view to the latest output.
func reverseLines(lines []logLine) {
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
}

func (h *Handlers) startupLogsFragment(c echo.Context) error {
	token := h.sessionToken(c)
	if token == "" {
		return c.NoContent(http.StatusUnauthorized)
	}
	active := c.QueryParam("active") == "1"
	data := logsData{EmptyMessage: "No startup script logs yet."}
	if active {
		data.EmptyMessage = "Waiting for agent startup script output…"
	}
	logs, err := h.app.StartupLogsFor(c, token, c.Param("name"), nil)
	if err != nil {
		var apiErr *coder.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			// Agent not up yet — mirror the SPA's "waiting for agent…" state.
			data.EmptyMessage = "Waiting for agent startup script output…"
		} else {
			data.ErrorMessage = errorDetail(err, "Failed to load startup script logs")
		}
	}
	for _, entry := range logs {
		level := ""
		if entry.Level != nil {
			level = *entry.Level
		}
		prefix := ""
		if level != "" {
			prefix = "[" + level + "]"
		}
		output := ""
		if entry.Output != nil {
			output = *entry.Output
		}
		data.Lines = append(data.Lines, logLine{
			Prefix:     prefix,
			Output:     output,
			LevelClass: logLevelClass(level),
		})
	}
	reverseLines(data.Lines)
	return c.Render(http.StatusOK, "log_lines.html", data)
}

func itoa64(n int64) string {
	return strconv.FormatInt(n, 10)
}
