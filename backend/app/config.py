from functools import lru_cache
from pathlib import Path

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


def _resolve_env_file() -> Path | None:
    """Find coder-service/.env from the normal backend layout or the process cwd."""
    here = Path(__file__).resolve()
    candidates = [
        here.parents[2] / ".env",  # .../coder-service/.env (normal layout)
        here.parents[1] / ".env",  # .../backend/.env
        Path.cwd() / ".env",
        Path.cwd().parent / ".env",
    ]
    for path in candidates:
        if path.is_file():
            return path
    return None


class Settings(BaseSettings):
    """Runtime configuration for the backend."""

    model_config = SettingsConfigDict(
        env_file=_resolve_env_file(),
        env_file_encoding="utf-8",
        extra="ignore",
        case_sensitive=False,
    )

    coder_url: str = Field(
        default="http://localhost:3000",
        description="Base URL of the Coder API (no trailing slash).",
    )
    # Present in ../.env for local validation; not required at runtime.
    coder_email: str | None = None
    coder_password: str | None = None

    @property
    def coder_api_base(self) -> str:
        return self.coder_url.rstrip("/")


@lru_cache
def get_settings() -> Settings:
    return Settings()
