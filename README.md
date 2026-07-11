# coder-service

A thin proxy and UI in front of a [Coder](https://coder.com) deployment. Authenticate with your Coder account, then create, monitor, and open workspaces without using the Coder dashboard.

```text
Browser (React)  →  coder-service (FastAPI)  →  Coder API / agents
```

## Features

**Web UI** (`frontend/`)

- Sign in with Coder email/password
- List, launch, and delete workspaces
- Random workspace name suggestions (`color-animal-NN`, same pattern as Coder)
- Live **build logs** (Terraform / provisioner) and **startup script** logs (agent)
- Access icons enabled only after the agent startup script finishes (`lifecycle_state === ready`)
  - In-browser terminal (xterm.js over a WebSocket PTY proxy)
  - VS Code Browser (code-server redirect)
  - VS Code Desktop deep link

**API** (`backend/`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Liveness + configured Coder URL |
| `POST` | `/auth` | Login → `session_token` |
| `GET` | `/me` | Current user + dashboard URL |
| `GET` | `/workspaces` | List workspaces (includes `startup_ready`) |
| `POST` | `/workspaces` | Create workspace |
| `GET` | `/workspaces/{name}` | Get workspace by name |
| `DELETE` | `/workspaces/{name}` | Start delete build (`202`) |
| `GET` | `/workspaces/{name}/access` | Terminal / VS Code availability |
| `GET` | `/workspaces/{name}/open/code-server` | Redirect into code-server |
| `POST` | `/workspaces/{name}/vscode-desktop` | Mint VS Code Desktop URI |
| `WS` | `/workspaces/{name}/terminal` | PTY proxy to the workspace agent |
| `GET` | `/workspaces/{name}/startup-logs` | Agent startup script output |
| `GET` | `/workspacebuilds/{id}` | Build status |
| `GET` | `/workspacebuilds/{id}/logs` | Provisioner / Terraform logs |

Protected routes expect the `Coder-Session-Token` header returned by `/auth`. OpenAPI docs: `http://localhost:8000/docs`.

## Layout

```text
coder-service/
├── .env                 # secrets (gitignored) — see Configuration
├── .gitignore
├── README.md
├── backend/             # FastAPI + PDM
│   ├── app/
│   │   ├── main.py
│   │   ├── coder_client.py
│   │   ├── terminal_proxy.py
│   │   ├── workspace_access.py
│   │   ├── config.py
│   │   ├── deps.py
│   │   └── schemas.py
│   ├── pyproject.toml
│   └── pdm.lock
└── frontend/            # React + Vite + TanStack Query + Tailwind
    ├── src/
    │   ├── api/
    │   ├── auth/
    │   ├── components/  # build logs, startup script, access icons
    │   ├── pages/       # login, workspaces, terminal
    │   └── lib/         # workspace name generator
    └── package.json
```

## Prerequisites

- Python **3.11+** and [PDM](https://pdm-project.org/)
- Node.js **20+** and npm
- A running Coder deployment (local or remote) that your credentials can reach

## Configuration

Create `.env` at the **repo root** (never commit this file):

```bash
coder_url="http://localhost:3000"
coder_dashboard_url="https://kvjg49neg3a86.pit-1.try.coder.app"
coder_session_token="..."
```

| Variable | Required | Description |
|----------|----------|-------------|
| `coder_url` | yes | Coder API base URL (no trailing slash) |
| `coder_dashboard_url` | no | Public dashboard URL; overrides Coder `buildinfo.dashboard_url` when set |
| `coder_session_token` | yes (for SSO-style auth) | Owner API token used to mint per-user tokens |
| `coder_email` / `coder_password` | no | Legacy; unused by email-only auth |

Optional frontend env (`frontend/.env`, also gitignored):

| Variable | Default | Description |
|----------|---------|-------------|
| `VITE_DEFAULT_TEMPLATE_ID` | `93c85ebe-899b-4adb-8f68-d01ed67ca304` | Template used when launching from the UI |
| `VITE_DEFAULT_TEMPLATE_NAME` | `Kubernetes workspace` | Template name displayed in the UI |
| `VITE_BACKEND_WS` | (derived) | Override WebSocket base for the terminal (e.g. `ws://127.0.0.1:8000`) |

## Run locally

**1. Backend** — `http://localhost:8000`

```bash
cd backend
pdm install
pdm run dev
```

**2. Frontend** — `http://127.0.0.1:5173` (proxies `/api` → `:8000`)

```bash
cd frontend
npm install
npm run dev -- --host 127.0.0.1 --port 5173
```

Open the UI, sign in with your Coder credentials, then launch a workspace.

## Workspace lifecycle (UI)

1. **Launch** — create build starts; **Build logs** open and poll Terraform/provisioner output.
2. **Apply complete** — build accordion collapses; **Startup script** stays live while the agent runs install scripts (e.g. code-server).
3. **Agent ready** — startup accordion collapses; Terminal / VS Code Browser / VS Code Desktop become available.
4. **Delete** — access icons and Delete share the same disabled state while a build is pending/running or a delete is in flight.

Build logs = provisioner job. Startup script logs = agent logs. They are separate Coder APIs.

## Auth model

```bash
TOKEN=$(curl -s -X POST http://localhost:8000/auth \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"..."}' \
  | python -c 'import sys,json; print(json.load(sys.stdin)["session_token"])')

curl -s http://localhost:8000/workspaces \
  -H "Coder-Session-Token: ${TOKEN}"
```

The session token is a Coder login token. Access endpoints and the terminal also accept `?token=` where a browser cannot set custom headers (redirects / WebSocket).

## Create workspace (API)

Provide exactly one of `template_id` or `template_version_id`. Rich parameters depend on your template; the default Kubernetes template uses:

| Parameter | Allowed | Default |
|-----------|---------|---------|
| `cpu` | `2`, `4`, `6`, `8` | `2` |
| `memory` | `2`, `4`, `6`, `8` | `2` |
| `home_disk_size` | `1`–`99999` | `10` |

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

Confirm template IDs against your deployment with Coder’s `GET /api/v2/templates`.

## Notes

- Build log polling uses HTTP snapshots (`after=`). Coder’s `?follow=true` is a WebSocket and is not wrapped for builds; agent terminal PTY **is** proxied over WebSocket.
- Access is gated on agent `lifecycle_state == "ready"`, not merely “build succeeded”, so code-server install can finish before VS Code / terminal open.
- Prefer pointing the UI terminal at `ws://127.0.0.1:8000` (or set `VITE_BACKEND_WS`) rather than relying on the Vite WS proxy for PTY traffic.

## License

MIT (see `backend/pyproject.toml`).
