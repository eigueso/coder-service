# coder-service backend

FastAPI service that authenticates against a Coder deployment and proxies workspace lifecycle, logs, and access (terminal / VS Code).

```text
Client  →  coder-service (:8000)  →  Coder API / agent PTY
```

Interactive API docs: [http://localhost:8000/docs](http://localhost:8000/docs)

## Stack

- FastAPI + Uvicorn
- httpx (Coder HTTP API)
- websockets (agent PTY proxy)
- pydantic-settings (loads repo-root `.env`)
- PDM for dependency / venv management

## Setup

```bash
cd backend
pdm install    # creates ./backend/.venv only — do not use system pip
```

Requires Python **3.11+**.

## Run

```bash
pdm run dev      # reload on change → http://localhost:8000
pdm run start    # production-style (no reload)
```

## Configuration

Loaded from `../.env` (repo root) or the process environment:

| Variable | Default | Description |
|----------|---------|-------------|
| `CODER_URL` / `coder_url` | `http://localhost:3000` | Coder API base URL (no trailing slash) |
| `CODER_EMAIL` / `coder_email` | | Optional; local smoke tests only |
| `CODER_PASSWORD` / `coder_password` | | Optional; local smoke tests only |

Do not commit `.env`.

## Auth

`POST /auth` proxies to Coder `POST /api/v2/users/login` and returns a session token.

```bash
TOKEN=$(curl -s -X POST http://localhost:8000/auth \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"..."}' \
  | python -c 'import sys,json; print(json.load(sys.stdin)["session_token"])')
```

Send that value as `Coder-Session-Token` on subsequent requests. Browser redirects and the terminal WebSocket also accept `?token=` when a custom header is not possible.

## Modules

| File | Role |
|------|------|
| `app/main.py` | Routes |
| `app/coder_client.py` | Thin httpx wrapper for Coder |
| `app/workspace_access.py` | Agent pick, readiness (`lifecycle_state`), VS Code URLs |
| `app/terminal_proxy.py` | Browser WS ↔ Coder agent PTY |
| `app/schemas.py` | Request / response models |
| `app/config.py` | Settings |
| `app/deps.py` | `Coder-Session-Token` dependency |

## API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/health` | no | Liveness + configured Coder URL |
| `POST` | `/auth` | no | Login → `{ session_token }` |
| `GET` | `/me` | yes | Username, email, dashboard URL |
| `GET` | `/workspaces` | yes | List workspaces (`startup_ready`, `agent_lifecycle_state`) |
| `POST` | `/workspaces` | yes | Create workspace |
| `GET` | `/workspaces/{name}` | yes | Get by name |
| `DELETE` | `/workspaces/{name}` | yes | Start delete build (`202`); `?orphan=true` optional |
| `GET` | `/workspaces/{name}/access` | yes | Access flags + URLs (gated on agent ready) |
| `GET` | `/workspaces/{name}/open/code-server` | token header or `?token=` | Redirect into code-server |
| `POST` | `/workspaces/{name}/vscode-desktop` | yes | Mint `vscode://coder.coder-remote/open?...` URI |
| `WS` | `/workspaces/{name}/terminal` | `?token=` or header | PTY proxy |
| `GET` | `/workspaces/{name}/startup-logs` | yes | Agent startup script logs |
| `GET` | `/workspacebuilds/{id}` | yes | Build status |
| `GET` | `/workspacebuilds/{id}/logs` | yes | Provisioner / Terraform logs |

### Create workspace

Provide exactly one of `template_id` or `template_version_id`.

```bash
curl -s -X POST http://localhost:8000/workspaces \
  -H 'Content-Type: application/json' \
  -H "Coder-Session-Token: ${TOKEN}" \
  -d '{
    "name": "yellow-bird-23",
    "template_id": "93c85ebe-899b-4adb-8f68-d01ed67ca304",
    "rich_parameter_values": [
      {"name": "cpu", "value": "2"},
      {"name": "memory", "value": "2"},
      {"name": "home_disk_size", "value": "10"}
    ]
  }'
```

### Build vs startup logs

| Endpoint | Source | When |
|----------|--------|------|
| `/workspacebuilds/{id}/logs` | Provisioner job (Terraform) | During / after build |
| `/workspaces/{name}/startup-logs` | Agent (`GET /api/v2/workspaceagents/{id}/logs`) | After agent starts |

Build log polling is HTTP-only (`after`, `before`, `format=json|text`). Coder’s `?follow=true` upgrades to a WebSocket and is not wrapped for builds.

### Access gating

Terminal, VS Code Browser, and VS Code Desktop require:

1. Latest build `status === succeeded` and `transition === start`
2. Agent `lifecycle_state === ready` (startup script finished)

Until then, `/access` returns `startup_ready: false` and the open endpoints respond with `409`.

### Terminal WebSocket

```text
ws://localhost:8000/workspaces/{name}/terminal?token=SESSION&width=120&height=48
```

Resolves the workspace agent, then relays to Coder’s agent PTY. Prefer the dashboard host for upstream when the configured API URL is loopback.

## Smoke check

```bash
curl -s http://localhost:8000/health
curl -s http://localhost:3000/api/v2/buildinfo   # Coder reachable
```
