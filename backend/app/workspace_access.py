"""Helpers for resolving workspace agents and access links."""

from __future__ import annotations

from typing import Any
from urllib.parse import quote, urlencode


def iter_agents(workspace: dict[str, Any]) -> list[dict[str, Any]]:
    agents: list[dict[str, Any]] = []
    latest = workspace.get("latest_build") or {}
    for resource in latest.get("resources") or []:
        for agent in resource.get("agents") or []:
            agents.append(agent)
    return agents


def pick_agent(workspace: dict[str, Any], preferred_name: str | None = "main") -> dict[str, Any] | None:
    agents = iter_agents(workspace)
    if not agents:
        return None
    if preferred_name:
        for agent in agents:
            if agent.get("name") == preferred_name:
                return agent
    return agents[0]


def find_app(agent: dict[str, Any], slug: str = "code-server") -> dict[str, Any] | None:
    for app in agent.get("apps") or []:
        if app.get("slug") == slug:
            return app
    return None


def display_apps(agent: dict[str, Any]) -> list[str]:
    raw = agent.get("display_apps") or []
    return [str(item) for item in raw]


def is_started(workspace: dict[str, Any]) -> bool:
    latest = workspace.get("latest_build") or {}
    job = latest.get("job") or {}
    return job.get("status") == "succeeded" and latest.get("transition") == "start"


def agent_lifecycle_state(agent: dict[str, Any] | None) -> str | None:
    if not agent:
        return None
    state = agent.get("lifecycle_state")
    return str(state) if state else None


def is_agent_ready(agent: dict[str, Any] | None) -> bool:
    """True once the agent startup script has finished and the agent is ready."""
    return agent_lifecycle_state(agent) == "ready"


def build_vscode_browser_url(
    *,
    base_url: str,
    username: str,
    workspace_name: str,
    agent_name: str,
    app_slug: str = "code-server",
) -> str:
    base = base_url.rstrip("/")
    return (
        f"{base}/@{quote(username)}/{quote(workspace_name)}.{quote(agent_name)}"
        f"/apps/{quote(app_slug, safe='')}/"
    )


def prefer_coder_app_base(*, api_base: str, dashboard_url: str) -> str:
    """Host used for path-based workspace apps (code-server).

    Prefer the configured API/access URL (e.g. http://localhost:3000). The
    buildinfo dashboard_url is often a try.coder.app tunnel that adds an extra
    hop even when the local Coder process can serve the same app paths.
    """
    api = (api_base or "").rstrip("/")
    dashboard = (dashboard_url or "").rstrip("/")
    return api or dashboard


def build_vscode_desktop_uri(
    *,
    coder_url: str,
    owner: str,
    workspace: str,
    token: str,
    agent: str | None = None,
    folder: str | None = None,
    app: str = "vscode",
) -> str:
    query = {
        "owner": owner,
        "workspace": workspace,
        "url": coder_url.rstrip("/"),
        "token": token,
        "openRecent": "true",
    }
    if agent:
        query["agent"] = agent
    if folder:
        query["folder"] = folder
    return f"{app}://coder.coder-remote/open?{urlencode(query)}"
