# coder-service frontend

React UI for signing into coder-service and managing Coder workspaces: launch, watch build / startup logs, and open terminal or VS Code once the agent is ready.

Requires the backend on `http://127.0.0.1:8000`.

## Stack

- React 19 + TypeScript (Vite 8)
- TanStack Query (+ DevTools)
- React Router 7
- Tailwind CSS v4
- xterm.js (in-browser terminal)
- `unique-names-generator` (workspace name suggestions)

## Setup

```bash
cd frontend
npm install
```

## Run

```bash
# from repo root, in another terminal:
#   cd backend && pdm run dev

npm run dev -- --host 127.0.0.1 --port 5173
```

App: [http://127.0.0.1:5173](http://127.0.0.1:5173)

Vite proxies `/api/*` → `http://127.0.0.1:8000/*` (including WebSockets). For the terminal PTY, the page prefers a direct `ws://127.0.0.1:8000` connection when possible — override with `VITE_BACKEND_WS` if needed.

## Routes

| Path | Auth | Description |
|------|------|-------------|
| `/login` | no | Email / password → `POST /auth`; stores session token |
| `/workspaces` | yes | List, launch, delete; build + startup logs; access icons |
| `/workspaces/:name/terminal` | token in query / storage | Full-page xterm PTY |
| `/` | — | Redirects to `/workspaces` |

## Features

**Launch**

- CPU / memory / disk rich parameters
- Clickable name suggestion when the field is empty (`Need a suggestion? color-animal-NN`)

**Monitoring**

- **Build logs** — Terraform / provisioner; opens while the build is pending/running, collapses when finished
- **Startup script** — agent logs (e.g. code-server install); stays live until `startup_ready`, then collapses

**Access** (enabled only when `startup_ready`)

- Terminal → `/workspaces/:name/terminal`
- VS Code Browser → backend redirect into code-server
- VS Code Desktop → backend mints `vscode://…` deep link

Delete and access icons share the same disabled state: delete in flight or any build pending/running.

## Layout

```text
src/
├── api/           # fetch client + types (session token header)
├── auth/          # AuthContext, RequireAuth
├── components/    # BuildLogsAccordion, StartupScriptAccordion, AccessIcons
├── lib/           # generateWorkspaceName
├── pages/         # LoginPage, WorkspacesPage, TerminalPage
├── App.tsx
└── main.tsx
```

## Config

Optional `frontend/.env` (gitignored):

| Variable | Default | Description |
|----------|---------|-------------|
| `VITE_DEFAULT_TEMPLATE_ID` | `93c85ebe-899b-4adb-8f68-d01ed67ca304` | Template ID for Launch |
| `VITE_DEFAULT_TEMPLATE_NAME` | `Kubernetes workspace` | Template name displayed in the launch form |
| `VITE_BACKEND_WS` | (auto) | WebSocket base for terminal, e.g. `ws://127.0.0.1:8000` |

Confirm template IDs against your Coder deployment (`GET /api/v2/templates`).

## Auth storage

The session token from `/auth` is kept in `localStorage` under `coder_session_token` and sent as `Coder-Session-Token` on API calls. Sign out clears it.

## Scripts

```bash
npm run dev       # Vite dev server
npm run build     # tsc + production bundle
npm run preview   # serve dist/
npm run lint      # oxlint
```

## Notes

- Workspace list polling runs while any build is active **or** a started workspace is not yet `startup_ready`.
- The total count stays stable during background refetches (no “refreshing…” flicker).
- Build logs and startup script logs are different backend endpoints; see the [backend README](../backend/README.md).
