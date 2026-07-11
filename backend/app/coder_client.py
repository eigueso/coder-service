"""Thin HTTP client for Coder's public API."""

from __future__ import annotations

from typing import Any
from uuid import UUID

import httpx
from fastapi import HTTPException, status

from app.config import Settings


class CoderClient:
    def __init__(
        self,
        settings: Settings,
        *,
        session_token: str | None = None,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        self._settings = settings
        self._session_token = session_token
        self._client = client
        self._owns_client = client is None

    async def __aenter__(self) -> CoderClient:
        if self._client is None:
            self._client = httpx.AsyncClient(
                base_url=self._settings.coder_api_base,
                timeout=httpx.Timeout(30.0),
            )
        return self

    async def __aexit__(self, *args: object) -> None:
        if self._owns_client and self._client is not None:
            await self._client.aclose()
            self._client = None

    def _auth_headers(self) -> dict[str, str]:
        if not self._session_token:
            raise RuntimeError("CoderClient requires session_token for authenticated calls")
        return {
            "Accept": "application/json",
            "Coder-Session-Token": self._session_token,
        }

    async def _request(
        self,
        method: str,
        path: str,
        *,
        authenticated: bool = False,
        json: dict[str, Any] | None = None,
        params: dict[str, Any] | None = None,
        headers: dict[str, str] | None = None,
        expect_json: bool = True,
    ) -> Any:
        if self._client is None:
            raise RuntimeError("CoderClient must be used as an async context manager")

        request_headers = {"Accept": "application/json"}
        if authenticated:
            request_headers.update(self._auth_headers())
        if headers:
            request_headers.update(headers)

        try:
            response = await self._client.request(
                method,
                path,
                json=json,
                params=params,
                headers=request_headers,
            )
        except httpx.RequestError as exc:
            raise HTTPException(
                status_code=status.HTTP_502_BAD_GATEWAY,
                detail=f"Failed to reach Coder at {self._settings.coder_api_base}: {exc}",
            ) from exc

        if response.is_success:
            if not expect_json:
                return response.text
            if not response.content:
                return None
            return response.json()

        self._raise_for_status(response)
        return None  # pragma: no cover

    def _raise_for_status(self, response: httpx.Response) -> None:
        detail_text = response.text
        try:
            payload = response.json()
            if isinstance(payload, dict):
                detail_text = (
                    payload.get("detail")
                    or payload.get("message")
                    or payload.get("error")
                    or detail_text
                )
        except ValueError:
            pass

        if response.status_code in {
            status.HTTP_401_UNAUTHORIZED,
            status.HTTP_403_FORBIDDEN,
        }:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Invalid or expired session token",
            )

        if response.status_code == status.HTTP_404_NOT_FOUND:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=detail_text or "Resource not found in Coder",
            )

        if response.status_code == status.HTTP_400_BAD_REQUEST:
            raise HTTPException(
                status_code=status.HTTP_400_BAD_REQUEST,
                detail=detail_text or "Bad request to Coder",
            )

        if response.status_code == status.HTTP_409_CONFLICT:
            raise HTTPException(
                status_code=status.HTTP_409_CONFLICT,
                detail=detail_text or "Conflict in Coder",
            )

        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail=f"Coder request failed with status {response.status_code}: {detail_text}",
        )

    async def login(self, email: str, password: str) -> str:
        """Authenticate against Coder and return a session token.

        Calls ``POST /api/v2/users/login``. On success Coder returns
        ``{"session_token": "..."}`` which is usable as ``Coder-Session-Token``.
        """
        if self._client is None:
            raise RuntimeError("CoderClient must be used as an async context manager")

        try:
            response = await self._client.post(
                "/api/v2/users/login",
                json={"email": email, "password": password},
                headers={"Accept": "application/json"},
            )
        except httpx.RequestError as exc:
            raise HTTPException(
                status_code=status.HTTP_502_BAD_GATEWAY,
                detail=f"Failed to reach Coder at {self._settings.coder_api_base}: {exc}",
            ) from exc

        if response.status_code == status.HTTP_201_CREATED:
            payload = response.json()
            token = payload.get("session_token")
            if not token:
                raise HTTPException(
                    status_code=status.HTTP_502_BAD_GATEWAY,
                    detail="Coder login succeeded but no session_token was returned",
                )
            return token

        if response.status_code in {
            status.HTTP_401_UNAUTHORIZED,
            status.HTTP_403_FORBIDDEN,
            status.HTTP_400_BAD_REQUEST,
        }:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Invalid email or password",
            )

        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail=f"Coder login failed with status {response.status_code}: {response.text}",
        )

    async def list_workspaces(self, *, owner: str = "me") -> dict[str, Any]:
        """List workspaces, defaulting to the authenticated user (``owner:me``)."""
        return await self._request(
            "GET",
            "/api/v2/workspaces",
            authenticated=True,
            params={"q": f"owner:{owner}"},
        )

    async def get_me(self) -> dict[str, Any]:
        """Return the authenticated Coder user."""
        return await self._request(
            "GET",
            "/api/v2/users/me",
            authenticated=True,
        )

    async def get_buildinfo(self) -> dict[str, Any]:
        """Return Coder deployment build info (includes dashboard_url)."""
        return await self._request(
            "GET",
            "/api/v2/buildinfo",
            authenticated=True,
        )

    async def create_workspace(self, body: dict[str, Any]) -> dict[str, Any]:
        """Create a workspace for the authenticated user (``POST .../users/me/workspaces``)."""
        return await self._request(
            "POST",
            "/api/v2/users/me/workspaces",
            authenticated=True,
            json=body,
        )

    async def get_workspace(self, name: str) -> dict[str, Any]:
        """Look up a workspace by name for the authenticated user."""
        return await self._request(
            "GET",
            f"/api/v2/users/me/workspace/{name}",
            authenticated=True,
        )

    async def create_workspace_build(
        self,
        workspace_id: UUID | str,
        *,
        transition: str,
        orphan: bool = False,
    ) -> dict[str, Any]:
        """Start a workspace build (start / stop / delete)."""
        body: dict[str, Any] = {"transition": transition}
        if orphan:
            body["orphan"] = True
        return await self._request(
            "POST",
            f"/api/v2/workspaces/{workspace_id}/builds",
            authenticated=True,
            json=body,
        )

    async def delete_workspace(
        self,
        name: str,
        *,
        orphan: bool = False,
    ) -> dict[str, Any]:
        """Delete a workspace by name via a delete-transition build."""
        workspace = await self.get_workspace(name)
        return await self.create_workspace_build(
            workspace["id"],
            transition="delete",
            orphan=orphan,
        )

    async def get_workspace_build(self, build_id: UUID | str) -> dict[str, Any]:
        """Fetch a workspace build by ID."""
        return await self._request(
            "GET",
            f"/api/v2/workspacebuilds/{build_id}",
            authenticated=True,
        )

    async def get_workspace_build_logs(
        self,
        build_id: UUID | str,
        *,
        after: int | None = None,
        before: int | None = None,
        log_format: str = "json",
    ) -> list[dict[str, Any]] | str:
        """Fetch provisioner logs for a build (HTTP snapshot; no ``follow``)."""
        params: dict[str, Any] = {}
        if after is not None:
            params["after"] = after
        if before is not None:
            params["before"] = before
        if log_format and log_format != "json":
            params["format"] = log_format

        expect_json = log_format != "text"
        return await self._request(
            "GET",
            f"/api/v2/workspacebuilds/{build_id}/logs",
            authenticated=True,
            params=params or None,
            expect_json=expect_json,
        )

    async def get_agent_logs(
        self,
        agent_id: UUID | str,
        *,
        after: int | None = None,
    ) -> list[dict[str, Any]]:
        """Fetch workspace agent / startup-script logs."""
        params: dict[str, Any] = {}
        if after is not None:
            params["after"] = after
        return await self._request(
            "GET",
            f"/api/v2/workspaceagents/{agent_id}/logs",
            authenticated=True,
            params=params or None,
        )

    async def create_api_key(self) -> str:
        """Create an API key for the authenticated user (``POST /users/me/keys``)."""
        payload = await self._request(
            "POST",
            "/api/v2/users/me/keys",
            authenticated=True,
        )
        key = payload.get("key") if isinstance(payload, dict) else None
        if not key:
            raise HTTPException(
                status_code=status.HTTP_502_BAD_GATEWAY,
                detail="Coder create API key succeeded but no key was returned",
            )
        return key
