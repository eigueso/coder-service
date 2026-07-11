# coder-service

Local service that sits in front of a Coder deployment. Goal: expose our own APIs (auth, workspace create, build status/logs, etc.) that proxy to Coder.

This README is a session handoff so work can continue in this workspace.

## Layout

```text
coder-service/
├── .env                 # CODER_URL, CODER_EMAIL, CODER_PASSWORD (gitignored locally)
├── backend/             # FastAPI + PDM (Python 3.14 .venv)
│   ├── app/
│   │   ├── main.py          # FastAPI app: /health, /auth, workspaces, builds
│   │   ├── config.py        # pydantic-settings, loads ../.env
│   │   ├── coder_client.py  # httpx client for Coder API
│   │   ├── deps.py          # Coder-Session-Token dependency
│   │   └── schemas.py       # Request/response models
│   ├── pyproject.toml
│   ├── pdm.lock
│   └── .venv/               # created by `pdm install` (do not use system pip)
└── frontend/            # React + Vite + TanStack Query + Tailwind
    ├── src/
    │   ├── api/             # HTTP client + types
    │   ├── auth/            # session token + route guard
    │   └── pages/           # /login, /workspaces
    ├── package.json
    └── README.md
```

Related paths outside this repo (used during the first session):

| Path | Why it matters |
|------|----------------|
| `/Users/malmonte/workspace/coder/coder` | Coder source + API docs under `docs/reference/api/` |
| `/Users/malmonte/workspace/new-coder-server/templates/kubernetes-template` | Template used for workspace create (rich params) |

## Environment

`.env` at the repo root (already present):

```bash
coder_url="http://localhost:3000"
coder_email="..."
coder_password="..."
```

Local Coder is reachable at `http://localhost:3000` (dashboard URL may show a `*.try.coder.app` host; API calls in this session used localhost successfully).

## Run the backend

```bash
cd backend
pdm install          # installs into ./backend/.venv only
pdm run dev          # http://localhost:8000  (OpenAPI: /docs)
```

## What exists today

Protected routes expect the `Coder-Session-Token` header returned by `/auth`.

### `GET /health`

Returns `{"status":"ok","coder_url":"http://localhost:3000"}`.

### `POST /auth`

Accepts Coder login credentials and returns a Coder session token.

```bash
curl -s -X POST http://localhost:8000/auth \
  -H 'Content-Type: application/json' \
  -d '{"email":"'"$CODER_EMAIL"'","password":"'"$CODER_PASSWORD"'"}'
# -> {"session_token":"..."}   HTTP 201
# bad password -> HTTP 401
```

Implementation: proxies to Coder `POST /api/v2/users/login`, which returns `{"session_token":"..."}`. That value is the `Coder-Session-Token` header for later API calls.

### `POST /workspaces`

Creates a workspace for the authenticated user (Coder `POST /api/v2/users/me/workspaces`).

```bash
TOKEN=$(curl -s -X POST http://localhost:8000/auth \
  -H 'Content-Type: application/json' \
  -d '{"email":"'"$CODER_EMAIL"'","password":"'"$CODER_PASSWORD"'"}' \
  | python -c 'import sys,json; print(json.load(sys.stdin)["session_token"])')

curl -s -X POST http://localhost:8000/workspaces \
  -H 'Content-Type: application/json' \
  -H "Coder-Session-Token: ${TOKEN}" \
  -d '{
    "name": "my-k8s-workspace",
    "template_id": "93c85ebe-899b-4adb-8f68-d01ed67ca304",
    "rich_parameter_values": [
      {"name": "cpu", "value": "2"},
      {"name": "memory", "value": "2"},
      {"name": "home_disk_size", "value": "10"}
    ]
  }'
# -> { id, name, template_id, latest_build: { id, status, ... } }
```

Provide exactly one of `template_id` or `template_version_id`.

### `GET /workspaces/{name}`

Looks up a workspace by name for the authenticated user.

### `DELETE /workspaces/{name}`

Deletes a workspace by starting a Coder build with `transition=delete`. Returns HTTP 202 and the delete build (`id`, `status`, …) so you can poll `/workspacebuilds/{id}` and `/logs`. Optional `?orphan=true` marks the workspace deleted without destroying cloud resources.

```bash
curl -s -X DELETE "http://localhost:8000/workspaces/${WS_NAME}" \
  -H "Coder-Session-Token: ${TOKEN}"
# -> { id, workspace_id, build_number, status, transition: "delete", ... }
```

### `GET /workspacebuilds/{build_id}`

Returns build status (`pending` | `running` | `succeeded` | `failed` | `canceled`) and optional `job_error`.

### `GET /workspacebuilds/{build_id}/logs`

HTTP log snapshot. Query params: `after`, `before`, `format` (`json` | `text`). Poll with `after=<last_log_id>`; live WebSocket `follow` is not wrapped yet.

## Coder API notes from this session

Docs live in the Coder repo: `docs/reference/api/workspaces.md`, `builds.md`, `authorization.md`, `schemas.md`.

### Kubernetes template rich parameters

From `new-coder-server/templates/kubernetes-template/main.tf` (`data "coder_parameter"`):

| Name | Allowed values | Default |
|------|----------------|---------|
| `cpu` | `2`, `4`, `6`, `8` | `2` |
| `memory` | `2`, `4`, `6`, `8` | `2` |
| `home_disk_size` | number `1`–`99999` | `10` |

Terraform variables like `use_kubeconfig`, `namespace`, `workspace_image` are template-push settings, not workspace create rich params.

Known working template id from this session: `93c85ebe-899b-4adb-8f68-d01ed67ca304` (confirm with `GET /api/v2/templates` if it changes).

### Direct Coder calls (reference)

If you need to hit Coder itself instead of coder-service:

```bash
# Job status
curl -s -H "Coder-Session-Token: ${TOKEN}" \
  "${CODER_URL}/api/v2/workspacebuilds/${BUILD_ID}"

# Logs snapshot / poll
curl -s -H "Coder-Session-Token: ${TOKEN}" \
  "${CODER_URL}/api/v2/workspacebuilds/${BUILD_ID}/logs?after=${LAST_LOG_ID}"
```

**Important:** Coder's `?follow=true` upgrades to a **WebSocket**. Plain curl/Yaak GET fails with:

```text
WebSocket protocol violation: Connection header "" does not contain Upgrade
```

coder-service intentionally does not pass `follow`; poll `/logs` instead.

## Suggested next work

1. Optional: WebSocket proxy for live build logs (`follow=true`).
2. Template picker (list Coder templates) instead of a fixed `VITE_DEFAULT_TEMPLATE_ID`.
3. Keep secrets in `.env`; do not commit tokens or passwords.

## Frontend

```bash
cd frontend
npm install
npm run dev   # http://localhost:5173  (proxies /api → :8000)
```

Routes: `/login` (auth), `/workspaces` (list / launch / delete). Requires backend on `:8000`.

## Quick smoke checklist

```bash
# 1. Coder up
curl -s http://localhost:3000/api/v2/buildinfo

# 2. Backend up
cd backend && pdm run dev

# 3. Auth
TOKEN=$(curl -s -X POST http://localhost:8000/auth \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$(grep coder_email ../.env | cut -d= -f2 | tr -d '\"')\",\"password\":\"$(grep coder_password ../.env | cut -d= -f2 | tr -d '\"')\"}" \
  | python -c 'import sys,json; print(json.load(sys.stdin)["session_token"])')

# 4. Create / status / logs (use a unique workspace name)
curl -s -X POST http://localhost:8000/workspaces \
  -H 'Content-Type: application/json' \
  -H "Coder-Session-Token: ${TOKEN}" \
  -d '{"name":"smoke-ws","template_id":"93c85ebe-899b-4adb-8f68-d01ed67ca304","rich_parameter_values":[{"name":"cpu","value":"2"},{"name":"memory","value":"2"},{"name":"home_disk_size","value":"10"}]}'
```

## Session origin

First session explored Coder's public API (create workspace + build logs), then scaffolded this FastAPI/PDM backend with `/auth` validated against local Coder using `.env` credentials. Later sessions added workspace create, lookup, build status, and log polling behind `Coder-Session-Token`.
