# OpenCode Agent Guide: coder-service

This guide helps agents understand the non-obvious architecture, development workflows, and quirks of this project.

## 1. Project Boundaries & Architecture

This repository is a monorepo consisting of:
- **`backend/`**: A FastAPI application managing communication with Coder's API. Package manager: **PDM**.
- **`frontend/`**: A React, Vite, TypeScript, and TailwindCSS UI. Package manager: **npm**.

### The Auth Model (Critical!)
- **SSO Simulation**: The backend mimics SSO. Users sign in on the frontend with **email only**.
- `POST /auth` expects `{ "email": "..." }` and **no password**.
- The backend uses the owner's Admin API token (`coder_session_token` in `.env`) to look up the user in Coder, mint a Coder API token for that user, and return it.
- Subsequent calls expect this token in the `Coder-Session-Token` header. For WebSockets or plain redirects (where headers can't be set), the backend accepts the token as a query parameter (e.g., `?token=...`).

### WebSocket Terminal Proxying
- `/workspaces/{name}/terminal` is a WebSocket route that proxies PTY traffic between xterm.js in the browser and the Coder agent.
- When `api_base` is loopback (localhost) but `coder_dashboard_url` is a public tunnel, the PTY proxy prefers the public tunnel since loopback often cannot reliably stream agent PTY data.

---

## 2. Developer Commands

### Environment Configuration
- **Backend/Root `.env`**: A single `.env` at the repository root holds all Coder settings:
  ```env
  coder_url="http://localhost:3000"
  coder_dashboard_url="https://..."
  coder_session_token="wqc..."
  ```
  *Note*: The backend resolves `.env` by climbing parent directories up to the repository root. Do not duplicate `.env` files unless necessary.
- **Frontend `.env`**: Set in `frontend/.env`:
  ```env
  VITE_DEFAULT_TEMPLATE_ID="93c85ebe-899b-4adb-8f68-d01ed67ca304"
  ```

### Backend Commands (run from `/backend`)
- **Install**: `pdm install`
- **Run dev server**: `pdm run dev` (starts on port `8000`)
- **Production start**: `pdm run start`

### Frontend Commands (run from `/frontend`)
- **Install**: `npm install`
- **Run dev server**: `npm run dev -- --host 127.0.0.1 --port 5173` (Vite proxy forwards `/api` -> `http://127.0.0.1:8000`)
- **Lint**: `npx oxlint`
- **Typecheck**: `npx tsc -b`
- **Build**: `npm run build`

---

## 3. Verification & Code Quality

### Backend (Python)
- **No Python Linters or Tests are configured**. No `pytest`, `ruff`, `black`, or `mypy`.
- Always manually verify any Python changes by starting the dev server (`pdm run dev`) and ensuring there are no runtime/syntax errors.

### Frontend (TypeScript/React)
- **Verification Order**: Always run linting and typechecking before attempting a build:
  ```bash
  npx oxlint && npx tsc -b && npm run build
  ```
- **Rules & Quirks**:
  - `oxlint` is extremely strict about unused dependencies or React hooks rules (e.g. `react-hooks/exhaustive-deps`).
  - Fast Refresh in React only works when files export components only. Exporting other values or functions may trigger linter warnings.
