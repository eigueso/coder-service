# go-coder-service

Go port of the Python `backend/` (FastAPI) and React `frontend/`, consolidated
into a single binary. Built with [Echo](https://echo.labstack.com/), server-rendered
templates + [htmx](https://htmx.org/), and Tailwind CSS v4.

- **JSON API** under `/api/*` — 1:1 routes and response shapes with the FastAPI
  backend (`Coder-Session-Token` header auth, `{"detail": "..."}` errors).
- **Web UI** at `/login`, `/workspaces`, `/workspaces/{name}/terminal` — ports the
  React SPA (login, workspace list with 5s polling during builds, create/delete,
  build-log and startup-script accordions, access buttons, xterm.js terminal).
- **Session handling**: the UI stores the Coder token in an `HttpOnly` cookie
  (`coder_session_token`) set by `POST /login`; the JSON API keeps the header contract.

## Configuration

Reads the shared `coder-service/.env` (same keys as the Python backend):

| Key | Default | Notes |
|---|---|---|
| `coder_url` | `http://localhost:3000` | Coder API base |
| `coder_dashboard_url` | – | overrides buildinfo dashboard discovery |
| `coder_session_token` | – | Owner token used by `POST /auth` (SSO simulation) |
| `coder_access_auth_mode` | `legacy_token` | `sso` disables the terminal proxy and uses Coder-hosted URLs |
| `terminal_allowed_origins` | localhost 5173 + 8080 | origins allowed on the terminal WebSocket |
| `cors_allowed_origins` | localhost 5173 + 8080 | CORS for the JSON API |
| `terminal_max_connections_per_worker` | `1000` | terminal proxy capacity |
| `vscode_desktop_token_lifetime_hours` | `8` | lifetime of minted VS Code Desktop tokens |
| `listen_addr` | `:8080` | Go port only |
| `default_template_id` | `93c85ebe-…` | was `VITE_DEFAULT_TEMPLATE_ID` |
| `default_template_name` | `Kubernetes workspace` | was `VITE_DEFAULT_TEMPLATE_NAME` |

## Build & run

```bash
# CSS (build-time only; Node is not needed at runtime)
npm install
npm run build:css

# Server (assets are embedded into the binary)
go build -o bin/go-coder-service ./cmd/server
./bin/go-coder-service
```

Then open http://localhost:8080 and sign in with a Coder user email.

## JSON API surface (mirrors backend/app/main.py)

```
GET    /api/health                                GET  /health (alias)
GET    /api/ready                                 GET  /ready (alias)
POST   /api/auth                                  mint token for email (201)
GET    /api/me
GET    /api/workspaces                            list + access links
POST   /api/workspaces                            create (201)
GET    /api/workspaces/{name}
DELETE /api/workspaces/{name}?orphan=             delete build (202)
GET    /api/workspaces/{name}/access
GET    /api/workspaces/{name}/open/code-server    307 redirect (?token=)
POST   /api/workspaces/{name}/vscode-desktop      vscode:// deep link
WS     /api/workspaces/{name}/terminal            PTY proxy (?token=&height=&width=&agent_id=)
GET    /api/workspacebuilds/{id}
GET    /api/workspacebuilds/{id}/logs?format=json|text&after=&before=
GET    /api/workspaces/{name}/startup-logs?after=
```

The terminal proxy keeps the Python behavior: origin allow-list, capacity
semaphore, close codes (4000–4003, 4401, 4403, 4429, 4404 in SSO mode), and
upstream fallback that prefers the public dashboard URL when the API base is
loopback. It additionally accepts the UI session cookie.

## Known deviations from the Python/React stack

- Validation errors return `{"detail": "<message>"}` with status 422 as a plain
  string instead of FastAPI's list-of-objects detail format.
- Pre-handshake WebSocket rejections (bad origin, missing token, capacity) accept
  then close with the documented code, instead of failing the HTTP upgrade.
- The delete confirmation uses the browser's native confirm dialog (`hx-confirm`)
  rather than a styled modal; the create form closes via full-page redirect.

## JavaScript policy

The UI is htmx + CSS only; the sole custom script is `terminal.js` (xterm.js
needs a JS host). The patterns replacing what would otherwise be scripted:

- **Create panel toggle** — hidden checkbox + Tailwind `peer-checked` /
  `group-has-checked` variants; the button label swaps via CSS.
- **Log accordion open state** — server-driven: each toggle is an htmx GET that
  re-renders the list with the choice encoded in the `logs=` query param
  (`b-<buildID>:1,s-<name>:0`), so state survives the 5s polling swaps with no
  client bookkeeping. Explicit user choices override the "auto-open while live"
  defaults, and stale keys are pruned server-side.
- **Stick-to-bottom logs** — fragments render newest-first inside a
  `flex-col-reverse` scroller, so the browser natively pins to the latest line
  and preserves the reader's position when scrolled up.
- **Name suggestion** — clicking the suggestion issues an htmx GET whose
  response fills the name input via an out-of-band swap and offers a fresh
  suggestion.
