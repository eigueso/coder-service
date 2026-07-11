from __future__ import annotations

from typing import Annotated
from uuid import UUID

import uvicorn
from fastapi import Depends, FastAPI, Header, HTTPException, Query, Response, WebSocket, status
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import RedirectResponse

from app.coder_client import CoderClient
from app.config import Settings, get_settings
from app.deps import require_session_token
from app.schemas import (
    AuthRequest,
    AuthResponse,
    CreateWorkspaceRequest,
    LogFormat,
    MeResponse,
    ProvisionerJobLog,
    VSCodeDesktopResponse,
    WorkspaceAccessResponse,
    WorkspaceAgentLog,
    WorkspaceBuildResponse,
    WorkspaceListResponse,
    WorkspaceResponse,
)
from app.terminal_proxy import proxy_workspace_terminal
from app.workspace_access import (
    agent_lifecycle_state,
    build_vscode_browser_url,
    build_vscode_desktop_uri,
    display_apps,
    find_app,
    is_agent_ready,
    is_started,
    pick_agent,
    prefer_coder_app_base,
)

app = FastAPI(
    title="coder-service",
    version="0.1.0",
    description="Backend that authenticates users against Coder and exposes workspace helpers.",
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=[
        "http://localhost:5173",
        "http://127.0.0.1:5173",
    ],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

SessionToken = Annotated[str, Depends(require_session_token)]
AppSettings = Annotated[Settings, Depends(get_settings)]


@app.get("/health")
async def health(settings: AppSettings) -> dict[str, str]:
    return {"status": "ok", "coder_url": settings.coder_api_base}


@app.post(
    "/auth",
    response_model=AuthResponse,
    status_code=status.HTTP_201_CREATED,
    summary="Authenticate against Coder",
)
async def auth(body: AuthRequest, settings: AppSettings) -> AuthResponse:
    async with CoderClient(settings) as coder:
        session_token = await coder.login(body.email, body.password)
    return AuthResponse(session_token=session_token)


@app.get("/me", response_model=MeResponse, summary="Current Coder user")
async def me(settings: AppSettings, session_token: SessionToken) -> MeResponse:
    async with CoderClient(settings, session_token=session_token) as coder:
        user = await coder.get_me()
        buildinfo = await coder.get_buildinfo()
    dashboard = (buildinfo.get("dashboard_url") or settings.coder_api_base).rstrip("/")
    return MeResponse(
        username=user["username"],
        email=user.get("email"),
        dashboard_url=dashboard,
    )


@app.post(
    "/workspaces",
    response_model=WorkspaceResponse,
    status_code=status.HTTP_201_CREATED,
    summary="Create a workspace",
)
async def create_workspace(
    body: CreateWorkspaceRequest,
    settings: AppSettings,
    session_token: SessionToken,
) -> WorkspaceResponse:
    async with CoderClient(settings, session_token=session_token) as coder:
        payload = await coder.create_workspace(body.to_coder_body())
    return WorkspaceResponse.from_coder(payload)


@app.get(
    "/workspaces",
    response_model=WorkspaceListResponse,
    summary="List workspaces",
)
async def list_workspaces(
    settings: AppSettings,
    session_token: SessionToken,
) -> WorkspaceListResponse:
    async with CoderClient(settings, session_token=session_token) as coder:
        payload = await coder.list_workspaces(owner="me")
    workspaces = [
        WorkspaceResponse.from_coder(item) for item in payload.get("workspaces") or []
    ]
    return WorkspaceListResponse(
        count=payload.get("count", len(workspaces)),
        workspaces=workspaces,
    )


@app.get(
    "/workspaces/{name}",
    response_model=WorkspaceResponse,
    summary="Get workspace by name",
)
async def get_workspace(
    name: str,
    settings: AppSettings,
    session_token: SessionToken,
) -> WorkspaceResponse:
    async with CoderClient(settings, session_token=session_token) as coder:
        payload = await coder.get_workspace(name)
    return WorkspaceResponse.from_coder(payload)


@app.get(
    "/workspaces/{name}/access",
    response_model=WorkspaceAccessResponse,
    summary="Workspace access links",
)
async def get_workspace_access(
    name: str,
    settings: AppSettings,
    session_token: SessionToken,
) -> WorkspaceAccessResponse:
    async with CoderClient(settings, session_token=session_token) as coder:
        workspace = await coder.get_workspace(name)
        user = await coder.get_me()
        buildinfo = await coder.get_buildinfo()

    dashboard = (buildinfo.get("dashboard_url") or settings.coder_api_base).rstrip("/")
    app_base = prefer_coder_app_base(
        api_base=settings.coder_api_base,
        dashboard_url=dashboard,
    )
    username = user["username"]
    started = is_started(workspace)
    agent = pick_agent(workspace)
    ready = started and is_agent_ready(agent)
    lifecycle = agent_lifecycle_state(agent)
    agent_name = agent.get("name") if agent else None
    agent_id = agent.get("id") if agent else None
    directory = None
    if agent:
        directory = agent.get("expanded_directory") or agent.get("directory")

    code_app = find_app(agent, "code-server") if agent else None
    apps = display_apps(agent) if agent else []
    has_vscode_desktop = "vscode" in apps or "vscode_insiders" in apps

    vscode_browser_url = None
    if ready and agent_name and code_app:
        vscode_browser_url = build_vscode_browser_url(
            base_url=app_base,
            username=username,
            workspace_name=name,
            agent_name=agent_name,
            app_slug=code_app.get("slug") or "code-server",
        )

    return WorkspaceAccessResponse(
        workspace_id=workspace["id"],
        workspace_name=name,
        username=username,
        started=started,
        startup_ready=ready,
        agent_lifecycle_state=lifecycle,
        agent_id=agent_id,
        agent_name=agent_name,
        agent_directory=directory,
        has_terminal=ready and agent_id is not None,
        has_vscode_browser=ready and code_app is not None,
        has_vscode_desktop=ready and has_vscode_desktop,
        vscode_browser_url=vscode_browser_url,
        code_server_slug=(code_app.get("slug") if code_app else None),
    )


@app.get(
    "/workspaces/{name}/open/code-server",
    summary="Open VS Code Browser",
    response_class=RedirectResponse,
)
async def open_code_server(
    name: str,
    settings: AppSettings,
    token: str | None = Query(
        default=None,
        description="Session token (required for plain browser navigation)",
    ),
    coder_session_token: Annotated[
        str | None,
        Header(alias="Coder-Session-Token"),
    ] = None,
) -> RedirectResponse:
    session_token = token or coder_session_token
    if not session_token:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Missing session token (query token= or Coder-Session-Token header)",
        )

    async with CoderClient(settings, session_token=session_token) as coder:
        workspace = await coder.get_workspace(name)
        user = await coder.get_me()
        buildinfo = await coder.get_buildinfo()

    if not is_started(workspace):
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="Workspace is not started")

    agent = pick_agent(workspace)
    if not is_agent_ready(agent):
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Startup script is still running; try again when the agent is ready",
        )
    code_app = find_app(agent, "code-server") if agent else None
    if not agent or not code_app:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="code-server app not found")

    dashboard = (buildinfo.get("dashboard_url") or settings.coder_api_base).rstrip("/")
    app_base = prefer_coder_app_base(
        api_base=settings.coder_api_base,
        dashboard_url=dashboard,
    )
    target = build_vscode_browser_url(
        base_url=app_base,
        username=user["username"],
        workspace_name=name,
        agent_name=agent["name"],
        app_slug=code_app.get("slug") or "code-server",
    )
    separator = "&" if "?" in target else "?"
    return RedirectResponse(
        url=f"{target}{separator}coder_session_token={session_token}",
        status_code=status.HTTP_307_TEMPORARY_REDIRECT,
    )


@app.post(
    "/workspaces/{name}/vscode-desktop",
    response_model=VSCodeDesktopResponse,
    summary="VS Code Desktop deep link",
)
async def vscode_desktop(
    name: str,
    settings: AppSettings,
    session_token: SessionToken,
) -> VSCodeDesktopResponse:
    async with CoderClient(settings, session_token=session_token) as coder:
        workspace = await coder.get_workspace(name)
        user = await coder.get_me()
        buildinfo = await coder.get_buildinfo()
        if not is_started(workspace):
            raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="Workspace is not started")
        agent = pick_agent(workspace)
        if not agent:
            raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="No agent found")
        if not is_agent_ready(agent):
            raise HTTPException(
                status_code=status.HTTP_409_CONFLICT,
                detail="Startup script is still running; try again when the agent is ready",
            )
        apps = display_apps(agent)
        if "vscode" not in apps and "vscode_insiders" not in apps:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail="VS Code Desktop is not enabled for this workspace agent",
            )
        api_key = await coder.create_api_key()

    dashboard = (buildinfo.get("dashboard_url") or settings.coder_api_base).rstrip("/")
    folder = agent.get("expanded_directory") or agent.get("directory")
    uri = build_vscode_desktop_uri(
        coder_url=dashboard,
        owner=user["username"],
        workspace=name,
        token=api_key,
        agent=agent.get("name"),
        folder=folder,
        app="vscode",
    )
    return VSCodeDesktopResponse(uri=uri)


@app.websocket("/workspaces/{name}/terminal")
async def workspace_terminal(
    websocket: WebSocket,
    name: str,
    settings: AppSettings,
    token: str | None = Query(default=None, description="Coder session token"),
    height: int = Query(default=48, ge=1, le=500),
    width: int = Query(default=120, ge=1, le=500),
    agent_id: str | None = Query(default=None, description="Optional agent UUID to skip lookup"),
) -> None:
    """Proxy a browser WebSocket to Coder's agent PTY endpoint."""
    session_token = token or websocket.headers.get("coder-session-token")
    if not session_token:
        await websocket.close(code=4401, reason="Missing session token")
        return
    await proxy_workspace_terminal(
        websocket,
        settings=settings,
        session_token=session_token,
        workspace_name=name,
        height=height,
        width=width,
        agent_id=agent_id,
    )


@app.delete(
    "/workspaces/{name}",
    response_model=WorkspaceBuildResponse,
    status_code=status.HTTP_202_ACCEPTED,
    summary="Delete a workspace",
)
async def delete_workspace(
    name: str,
    settings: AppSettings,
    session_token: SessionToken,
    orphan: bool = Query(default=False),
) -> WorkspaceBuildResponse:
    async with CoderClient(settings, session_token=session_token) as coder:
        payload = await coder.delete_workspace(name, orphan=orphan)
    return WorkspaceBuildResponse.from_coder(payload)


@app.get(
    "/workspacebuilds/{build_id}",
    response_model=WorkspaceBuildResponse,
    summary="Get workspace build status",
)
async def get_workspace_build(
    build_id: UUID,
    settings: AppSettings,
    session_token: SessionToken,
) -> WorkspaceBuildResponse:
    async with CoderClient(settings, session_token=session_token) as coder:
        payload = await coder.get_workspace_build(build_id)
    return WorkspaceBuildResponse.from_coder(payload)


@app.get(
    "/workspacebuilds/{build_id}/logs",
    response_model=None,
    summary="Get workspace build logs",
)
async def get_workspace_build_logs(
    build_id: UUID,
    settings: AppSettings,
    session_token: SessionToken,
    after: int | None = Query(default=None),
    before: int | None = Query(default=None),
    log_format: LogFormat = Query(default="json", alias="format"),
) -> list[ProvisionerJobLog] | Response:
    async with CoderClient(settings, session_token=session_token) as coder:
        result = await coder.get_workspace_build_logs(
            build_id,
            after=after,
            before=before,
            log_format=log_format,
        )

    if log_format == "text":
        return Response(content=result if isinstance(result, str) else str(result), media_type="text/plain")

    assert isinstance(result, list)
    return [ProvisionerJobLog.model_validate(entry) for entry in result]


@app.get(
    "/workspaces/{name}/startup-logs",
    response_model=list[WorkspaceAgentLog],
    summary="Get startup script / agent logs",
    description=(
        "Returns logs from the workspace agent (startup scripts, code-server install, etc.) "
        "via Coder GET /api/v2/workspaceagents/{id}/logs. Distinct from Terraform build logs."
    ),
)
async def get_startup_logs(
    name: str,
    settings: AppSettings,
    session_token: SessionToken,
    after: int | None = Query(default=None, description="Return logs with id greater than this"),
) -> list[WorkspaceAgentLog]:
    async with CoderClient(settings, session_token=session_token) as coder:
        workspace = await coder.get_workspace(name)
        agent = pick_agent(workspace)
        if not agent or not agent.get("id"):
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail="No agent found on workspace (startup logs appear after the agent starts)",
            )
        result = await coder.get_agent_logs(agent["id"], after=after)

    assert isinstance(result, list)
    return [WorkspaceAgentLog.model_validate(entry) for entry in result]


def run() -> None:
    uvicorn.run("app.main:app", host="0.0.0.0", port=8000, reload=False)


if __name__ == "__main__":
    run()
