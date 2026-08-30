# go-backend

Go port of the Python backend (`backend/app`): a JSON API that authenticates
users against [Coder](https://coder.com) and exposes workspace helpers. It is
backend-only — the frontends (`frontend/`, `solid-frontend/`) run separately
and proxy `/api/*` to this service.

The API contract is 1:1 with the Python backend: same routes, request/response
shapes, status codes, error format (`{"detail": "..."}`), and WebSocket close
codes.

## Layout

```
cmd/server/          entry point (HTTP server, graceful shutdown)
internal/config/     settings from env vars + shared coder-service/.env
internal/coder/      thin Coder API client + workspace/agent helpers
internal/api/        handlers, schemas, middleware, terminal WebSocket proxy
```

Dependencies are minimal by design: the standard library `net/http` router,
`github.com/coder/websocket` for the terminal proxy, `google/uuid`, and
`godotenv`.

## Run

```bash
make run          # builds and starts on :8000 (listen_addr to override)
make test         # full test suite
make cover        # coverage summary
make vet          # go vet + gofmt check
```

## Configuration

Loaded from the process environment plus the first `.env` found in the working
directory, its parent, or next to the binary (matching the Python backend's
lookup). Variable names are case-insensitive.

| Variable | Default | Purpose |
|---|---|---|
| `coder_url` | `http://localhost:3000` | Coder API base URL |
| `coder_dashboard_url` | _(discovered)_ | Override for the dashboard URL from `/api/v2/buildinfo` |
| `coder_session_token` | _(empty)_ | Owner token used by `POST /auth` to mint user tokens |
| `coder_access_auth_mode` | `legacy_token` | `legacy_token` or `sso` |
| `coder_http_max_connections` | `200` | Coder HTTP pool: max connections |
| `coder_http_max_keepalive_connections` | `50` | Coder HTTP pool: idle connections |
| `coder_http_keepalive_expiry_seconds` | `30` | Coder HTTP pool: idle timeout |
| `terminal_max_connections_per_worker` | `1000` | Concurrent terminal proxies |
| `terminal_allowed_origins` | localhost:5173 origins | Origins allowed on the terminal WebSocket |
| `vscode_desktop_token_lifetime_hours` | `8` | Lifetime of tokens minted for VS Code Desktop |
| `cors_allowed_origins` | localhost:5173 origins | CORS allowlist |
| `listen_addr` | `:8000` | Bind address |

## Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/health` | Liveness probe |
| GET | `/ready` | Readiness probe (503 while shutting down) |
| POST | `/auth` | Mint a Coder token for a user email (SSO simulation) |
| GET | `/me` | Current Coder user + dashboard URL |
| POST | `/workspaces` | Create a workspace |
| GET | `/workspaces` | List workspaces with access info |
| GET | `/workspaces/{name}` | Get workspace by name |
| DELETE | `/workspaces/{name}` | Delete a workspace (`?orphan=true` supported) |
| GET | `/workspaces/{name}/access` | Workspace access links |
| GET | `/workspaces/{name}/open/code-server` | 307 redirect to VS Code Browser |
| POST | `/workspaces/{name}/vscode-desktop` | VS Code Desktop deep link |
| GET | `/workspaces/{name}/terminal` | WebSocket proxy to the agent PTY |
| GET | `/workspaces/{name}/startup-logs` | Agent / startup-script logs |
| GET | `/workspacebuilds/{build_id}` | Workspace build status |
| GET | `/workspacebuilds/{build_id}/logs` | Provisioner logs (`?format=json\|text`) |

Authenticated endpoints require the `Coder-Session-Token` header; the terminal
WebSocket and the code-server redirect also accept `?token=` for plain browser
navigation.

### Terminal WebSocket close codes

`4404` SSO mode, `4403` disallowed origin / agent mismatch, `4401` missing
token, `4429` capacity reached, `4000` workspace not started, `4001` no agent,
`4002` agent not connected, `4003` startup script still running, `1011`
internal errors — identical to the Python backend.
