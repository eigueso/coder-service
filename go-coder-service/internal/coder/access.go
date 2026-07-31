// Helpers for resolving workspace agents and access links,
// mirroring backend/app/workspace_access.py.
package coder

import (
	"net/url"
	"strings"
)

func asObjectSlice(value any) []map[string]any {
	items, _ := value.([]any)
	var result []map[string]any
	for _, item := range items {
		if obj, ok := item.(map[string]any); ok {
			result = append(result, obj)
		}
	}
	return result
}

func getString(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	value, _ := obj[key].(string)
	return value
}

func getObject(obj map[string]any, key string) map[string]any {
	if obj == nil {
		return nil
	}
	value, _ := obj[key].(map[string]any)
	return value
}

func IterAgents(workspace map[string]any) []map[string]any {
	var agents []map[string]any
	latest := getObject(workspace, "latest_build")
	for _, resource := range asObjectSlice(latest["resources"]) {
		agents = append(agents, asObjectSlice(resource["agents"])...)
	}
	return agents
}

// PickAgent prefers the agent named "main", falling back to the first agent.
func PickAgent(workspace map[string]any) map[string]any {
	agents := IterAgents(workspace)
	if len(agents) == 0 {
		return nil
	}
	for _, agent := range agents {
		if getString(agent, "name") == "main" {
			return agent
		}
	}
	return agents[0]
}

func FindApp(agent map[string]any, slug string) map[string]any {
	if agent == nil {
		return nil
	}
	for _, app := range asObjectSlice(agent["apps"]) {
		if getString(app, "slug") == slug {
			return app
		}
	}
	return nil
}

func DisplayApps(agent map[string]any) []string {
	if agent == nil {
		return nil
	}
	items, _ := agent["display_apps"].([]any)
	var apps []string
	for _, item := range items {
		if s, ok := item.(string); ok {
			apps = append(apps, s)
		}
	}
	return apps
}

func IsStarted(workspace map[string]any) bool {
	latest := getObject(workspace, "latest_build")
	job := getObject(latest, "job")
	return getString(job, "status") == "succeeded" && getString(latest, "transition") == "start"
}

func AgentLifecycleState(agent map[string]any) string {
	return getString(agent, "lifecycle_state")
}

// IsAgentReady is true once the agent startup script has finished.
func IsAgentReady(agent map[string]any) bool {
	return AgentLifecycleState(agent) == "ready"
}

func BuildVSCodeBrowserURL(baseURL, username, workspaceName, agentName, appSlug string) string {
	if appSlug == "" {
		appSlug = "code-server"
	}
	base := strings.TrimRight(baseURL, "/")
	return base + "/@" + url.PathEscape(username) + "/" +
		url.PathEscape(workspaceName) + "." + url.PathEscape(agentName) +
		"/apps/" + url.QueryEscape(appSlug) + "/"
}

func BuildWebTerminalURL(baseURL, username, workspaceName, agentName string) string {
	segment := url.PathEscape(workspaceName)
	if agentName != "" {
		segment += "." + url.PathEscape(agentName)
	}
	return strings.TrimRight(baseURL, "/") + "/@" + url.PathEscape(username) + "/" + segment + "/terminal"
}

// PreferCoderAppBase picks the host used for path-based workspace apps
// (code-server). Prefer the configured API/access URL over the buildinfo
// dashboard, which is often a try.coder.app tunnel with an extra hop.
func PreferCoderAppBase(apiBase, dashboardURL string) string {
	if api := strings.TrimRight(apiBase, "/"); api != "" {
		return api
	}
	return strings.TrimRight(dashboardURL, "/")
}

func BuildVSCodeDesktopURI(coderURL, owner, workspace, token, agent, folder, app string) string {
	if app == "" {
		app = "vscode"
	}
	query := url.Values{
		"owner":      {owner},
		"workspace":  {workspace},
		"url":        {strings.TrimRight(coderURL, "/")},
		"token":      {token},
		"openRecent": {"true"},
	}
	if agent != "" {
		query.Set("agent", agent)
	}
	if folder != "" {
		query.Set("folder", folder)
	}
	return app + "://coder.coder-remote/open?" + query.Encode()
}
