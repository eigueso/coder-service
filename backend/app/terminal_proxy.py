"""WebSocket proxy from the browser to Coder's agent PTY endpoint."""

from __future__ import annotations

import asyncio
import logging
from typing import Any
from urllib.parse import urlencode
from uuid import UUID, uuid4

import websockets
from fastapi import WebSocket, WebSocketDisconnect
from starlette.websockets import WebSocketState
from websockets.exceptions import ConnectionClosed

from app.coder_client import CoderClient
from app.config import Settings
from app.workspace_access import is_agent_ready, is_started, pick_agent

logger = logging.getLogger(__name__)


def _to_ws_base(http_base: str) -> str:
    base = http_base.rstrip("/")
    if base.startswith("https://"):
        return "wss://" + base.removeprefix("https://")
    if base.startswith("http://"):
        return "ws://" + base.removeprefix("http://")
    return base


def _upstream_urls(*, api_base: str, dashboard: str, agent_id: str, query: str) -> list[tuple[str, str]]:
    """Build upstream PTY candidates.

    When the API base is loopback but the dashboard is a public access URL
    (typical try.coder.app tunnel), prefer the dashboard. Localhost often accepts
    the WebSocket upgrade but cannot reliably stream agent PTY data.
    """
    from urllib.parse import urlparse

    def host(url: str) -> str | None:
        return urlparse(url).hostname

    api_host = host(api_base)
    dash_host = host(dashboard)
    loopback = {"localhost", "127.0.0.1", "::1"}

    if api_host in loopback and dash_host and dash_host not in loopback:
        ordered_bases = [("dashboard", dashboard), ("api", api_base)]
    else:
        ordered_bases = [("api", api_base), ("dashboard", dashboard)]

    ordered: list[tuple[str, str]] = []
    seen: set[str] = set()
    for label, base in ordered_bases:
        base = (base or "").rstrip("/")
        if not base or base in seen:
            continue
        seen.add(base)
        ordered.append((label, f"{_to_ws_base(base)}/api/v2/workspaceagents/{agent_id}/pty?{query}"))
    return ordered


async def _connect_upstream(
    urls: list[tuple[str, str]],
    *,
    session_token: str,
    origin: str,
) -> Any:
    last_error: Exception | None = None
    for label, url in urls:
        try:
            conn = await websockets.connect(
                url,
                additional_headers={"Coder-Session-Token": session_token},
                max_size=8 * 1024 * 1024,
                open_timeout=10,
                close_timeout=1,
                ping_interval=20,
                ping_timeout=20,
                compression=None,
                origin=origin,
            )
            logger.info("Terminal upstream connected via %s", label)
            return conn
        except Exception as exc:  # noqa: BLE001
            last_error = exc
            logger.warning("Terminal upstream %s failed: %s", label, exc)
    assert last_error is not None
    raise last_error


async def proxy_workspace_terminal(
    websocket: WebSocket,
    *,
    settings: Settings,
    session_token: str,
    workspace_name: str,
    height: int = 48,
    width: int = 120,
    agent_id: str | None = None,
) -> None:
    await websocket.accept()

    dashboard = settings.coder_api_base
    resolved_agent_id = agent_id

    try:
        async with CoderClient(settings, session_token=session_token) as coder:
            if settings.configured_dashboard_url:
                dashboard = settings.resolve_dashboard_url()
            else:
                buildinfo = await coder.get_buildinfo()
                dashboard = settings.resolve_dashboard_url(buildinfo.get("dashboard_url"))
            if not resolved_agent_id:
                workspace = await coder.get_workspace(workspace_name)
                if not is_started(workspace):
                    await websocket.close(code=4000, reason="Workspace is not started")
                    return
                agent = pick_agent(workspace)
                if agent is None or not agent.get("id"):
                    await websocket.close(code=4001, reason="No agent found on workspace")
                    return
                if not is_agent_ready(agent):
                    await websocket.close(code=4003, reason="Startup script still running")
                    return
                if agent.get("status") and agent.get("status") != "connected":
                    await websocket.close(
                        code=4002,
                        reason=f"Agent is {agent.get('status')}, not connected",
                    )
                    return
                resolved_agent_id = str(agent["id"])
            else:
                # Validate UUID shape early.
                UUID(resolved_agent_id)
    except Exception as exc:  # noqa: BLE001
        logger.exception("Failed resolving workspace for terminal proxy")
        detail = getattr(exc, "detail", None)
        reason = str(detail or exc)[:120]
        if websocket.client_state == WebSocketState.CONNECTED:
            await websocket.close(code=1011, reason=reason)
        return

    query = urlencode(
        {
            "reconnect": str(uuid4()),
            "height": str(height),
            "width": str(width),
        }
    )
    urls = _upstream_urls(
        api_base=settings.coder_api_base,
        dashboard=dashboard,
        agent_id=resolved_agent_id,
        query=query,
    )

    upstream = None
    try:
        upstream = await _connect_upstream(
            urls,
            session_token=session_token,
            origin=dashboard,
        )
        await _relay(websocket, upstream)
    except WebSocketDisconnect:
        return
    except Exception as exc:  # noqa: BLE001
        logger.exception("Terminal proxy failed for workspace %s", workspace_name)
        reason = str(exc)[:120]
        if websocket.client_state == WebSocketState.CONNECTED:
            try:
                await websocket.send_text(f"\r\n[coder-service] terminal proxy error: {reason}\r\n")
            except Exception:  # noqa: BLE001
                pass
            await websocket.close(code=1011, reason=reason)
    finally:
        if upstream is not None:
            try:
                await upstream.close()
            except Exception:  # noqa: BLE001
                pass


async def _relay(client: WebSocket, upstream: Any) -> None:
    """Bidirectional byte pump with decoupled send/receive loops."""

    to_upstream: asyncio.Queue[bytes | None] = asyncio.Queue(maxsize=256)
    to_client: asyncio.Queue[bytes | None] = asyncio.Queue(maxsize=256)

    async def read_client() -> None:
        try:
            while True:
                message = await client.receive()
                if message.get("type") == "websocket.disconnect":
                    break
                data = message.get("bytes")
                if data is None and message.get("text") is not None:
                    data = message["text"].encode("utf-8")
                if data is None:
                    continue
                await to_upstream.put(data)
        finally:
            await to_upstream.put(None)

    async def write_upstream() -> None:
        try:
            while True:
                data = await to_upstream.get()
                if data is None:
                    break
                await upstream.send(data)
        except ConnectionClosed:
            pass

    async def read_upstream() -> None:
        try:
            async for message in upstream:
                data = message if isinstance(message, bytes) else message.encode("utf-8")
                await to_client.put(data)
        except ConnectionClosed:
            pass
        finally:
            await to_client.put(None)

    async def write_client() -> None:
        try:
            while True:
                data = await to_client.get()
                if data is None:
                    break
                if client.client_state != WebSocketState.CONNECTED:
                    break
                await client.send_bytes(data)
        except (WebSocketDisconnect, RuntimeError):
            pass

    tasks = [
        asyncio.create_task(read_client()),
        asyncio.create_task(write_upstream()),
        asyncio.create_task(read_upstream()),
        asyncio.create_task(write_client()),
    ]
    try:
        await asyncio.wait(tasks, return_when=asyncio.FIRST_COMPLETED)
    finally:
        for task in tasks:
            task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
