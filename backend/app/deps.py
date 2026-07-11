"""FastAPI dependencies."""

from __future__ import annotations

from typing import Annotated

from fastapi import Header, HTTPException, status


async def require_session_token(
    coder_session_token: Annotated[
        str | None,
        Header(
            alias="Coder-Session-Token",
            description="Session token from POST /auth (Coder-Session-Token).",
        ),
    ] = None,
) -> str:
    """Require a Coder session token on protected endpoints."""
    if not coder_session_token or not coder_session_token.strip():
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Missing Coder-Session-Token header. Authenticate via POST /auth first.",
        )
    return coder_session_token.strip()
