# coder-service backend

FastAPI service that authenticates users against a Coder deployment and proxies workspace create, build status, and build logs.

## Setup

All dependencies install into a project-local `.venv` via PDM (not your system Python):

```bash
cd backend
pdm install
```

## Run

```bash
pdm run dev
```

Server listens on `http://localhost:8000`. Docs: `http://localhost:8000/docs`.

## Configuration

Loaded from `../.env` (or process environment):

| Variable | Default | Description |
|----------|---------|-------------|
| `CODER_URL` | `http://localhost:3000` | Base URL of the Coder deployment |
| `CODER_EMAIL` | | Optional. Used only for local smoke tests |
| `CODER_PASSWORD` | | Optional. Used only for local smoke tests |

## Auth

`POST /auth` with JSON `{"email": "...", "password": "..."}` calls Coder's `POST /api/v2/users/login` and returns `{"session_token": "..."}`.

Example:

```bash
curl -s -X POST http://localhost:8000/auth \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@nodomain.com","password":"..."}'
```

Use the returned token as `Coder-Session-Token` on subsequent coder-service endpoints (and on Coder API calls).

## Workspace APIs

All of these require the `Coder-Session-Token` header from `/auth`.

### Create workspace

`POST /workspaces`

```bash
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
```

Provide exactly one of `template_id` or `template_version_id`. Response includes `latest_build.id` for status/logs.

### List workspaces

`GET /workspaces`

Returns `{ "count": N, "workspaces": [...] }` for the authenticated user.

### Get workspace by name

`GET /workspaces/{name}`

### Delete workspace

`DELETE /workspaces/{name}`

Starts a Coder delete build (`transition=delete`) and returns `202` with the build payload so you can poll status/logs. Optional `?orphan=true` skips destroying provisioned resources.

```bash
curl -s -X DELETE "http://localhost:8000/workspaces/${WS_NAME}" \
  -H "Coder-Session-Token: ${TOKEN}"
```

### Get build status

`GET /workspacebuilds/{build_id}`

Returns job status (`pending` | `running` | `succeeded` | `failed` | `canceled`) plus optional `job_error`.

### Get / poll build logs

`GET /workspacebuilds/{build_id}/logs`

Query params: `after`, `before`, `format` (`json` default, or `text`). Does not support live `follow` (WebSocket); poll with `after` instead.
