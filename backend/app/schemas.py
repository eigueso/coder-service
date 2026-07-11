from __future__ import annotations

from datetime import datetime
from typing import Any, Literal
from uuid import UUID

from pydantic import BaseModel, EmailStr, Field, model_validator


class AuthRequest(BaseModel):
    email: EmailStr = Field(..., description="Coder account email")
    password: str = Field(..., min_length=1, description="Coder account password")


class AuthResponse(BaseModel):
    session_token: str = Field(
        ...,
        description="Coder session token. Send as Coder-Session-Token on subsequent API calls.",
    )


class MeResponse(BaseModel):
    username: str = Field(..., description="Coder username")
    email: str | None = None
    dashboard_url: str = Field(
        ...,
        description="Coder dashboard base URL used for web app links.",
    )


class WorkspaceAccessResponse(BaseModel):
    workspace_id: UUID
    workspace_name: str
    username: str
    started: bool
    startup_ready: bool = False
    agent_lifecycle_state: str | None = None
    agent_id: UUID | None = None
    agent_name: str | None = None
    agent_directory: str | None = None
    has_terminal: bool = False
    has_vscode_browser: bool = False
    has_vscode_desktop: bool = False
    vscode_browser_url: str | None = None
    code_server_slug: str | None = None


class VSCodeDesktopResponse(BaseModel):
    uri: str = Field(..., description="vscode://coder.coder-remote/open?... deep link")


class RichParameterValue(BaseModel):
    name: str = Field(..., min_length=1, description="Parameter name (e.g. cpu, memory)")
    value: str = Field(..., min_length=1, description="Parameter value as a string")


class CreateWorkspaceRequest(BaseModel):
    name: str = Field(
        ...,
        min_length=1,
        max_length=32,
        pattern=r"^[a-zA-Z0-9]([a-zA-Z0-9-]{0,31})$",
        description="Workspace name (letters, numbers, hyphens; max 32 chars).",
    )
    template_id: UUID | None = Field(
        default=None,
        description="Template ID (uses active version). Mutually exclusive with template_version_id.",
    )
    template_version_id: UUID | None = Field(
        default=None,
        description="Specific template version. Mutually exclusive with template_id.",
    )
    rich_parameter_values: list[RichParameterValue] = Field(
        default_factory=list,
        description="Rich parameter values for the initial provision.",
    )

    @model_validator(mode="after")
    def validate_template_source(self) -> CreateWorkspaceRequest:
        if self.name.lower() in {"new", "create"}:
            raise ValueError("Workspace name cannot be 'new' or 'create'")
        has_template = self.template_id is not None
        has_version = self.template_version_id is not None
        if has_template == has_version:
            raise ValueError("Provide exactly one of template_id or template_version_id")
        return self

    def to_coder_body(self) -> dict[str, Any]:
        body: dict[str, Any] = {"name": self.name}
        if self.template_id is not None:
            body["template_id"] = str(self.template_id)
        if self.template_version_id is not None:
            body["template_version_id"] = str(self.template_version_id)
        if self.rich_parameter_values:
            body["rich_parameter_values"] = [
                {"name": p.name, "value": p.value} for p in self.rich_parameter_values
            ]
        return body


class BuildSummary(BaseModel):
    id: UUID
    status: str = Field(..., description="Job status: pending | running | succeeded | failed | canceled")
    build_number: int | None = None
    transition: str | None = None


class WorkspaceResponse(BaseModel):
    id: UUID
    name: str
    template_id: UUID | None = None
    latest_build: BuildSummary
    agent_lifecycle_state: str | None = None
    startup_ready: bool = False

    @classmethod
    def from_coder(cls, payload: dict[str, Any]) -> WorkspaceResponse:
        from app.workspace_access import agent_lifecycle_state, is_agent_ready, pick_agent

        latest = payload.get("latest_build") or {}
        job = latest.get("job") or {}
        agent = pick_agent(payload)
        return cls(
            id=payload["id"],
            name=payload["name"],
            template_id=payload.get("template_id"),
            latest_build=BuildSummary(
                id=latest["id"],
                status=job.get("status") or "unknown",
                build_number=latest.get("build_number"),
                transition=latest.get("transition"),
            ),
            agent_lifecycle_state=agent_lifecycle_state(agent),
            startup_ready=is_agent_ready(agent),
        )


class WorkspaceListResponse(BaseModel):
    count: int
    workspaces: list[WorkspaceResponse]


class WorkspaceBuildResponse(BaseModel):
    id: UUID
    workspace_id: UUID
    build_number: int
    status: str = Field(..., description="Job status: pending | running | succeeded | failed | canceled")
    transition: str | None = None
    job_error: str | None = None
    created_at: datetime | None = None

    @classmethod
    def from_coder(cls, payload: dict[str, Any]) -> WorkspaceBuildResponse:
        job = payload.get("job") or {}
        return cls(
            id=payload["id"],
            workspace_id=payload["workspace_id"],
            build_number=payload["build_number"],
            status=job.get("status") or "unknown",
            transition=payload.get("transition"),
            job_error=job.get("error") or None,
            created_at=payload.get("created_at"),
        )


class ProvisionerJobLog(BaseModel):
    id: int
    created_at: datetime | None = None
    log_level: str | None = None
    log_source: str | None = None
    output: str | None = None
    stage: str | None = None


class WorkspaceAgentLog(BaseModel):
    id: int
    created_at: datetime | None = None
    level: str | None = None
    output: str | None = None
    source_id: UUID | None = None


LogFormat = Literal["json", "text"]
