# coder-service frontend

React + Vite UI for authenticating against coder-service and managing Coder workspaces.

## Stack

- React + TypeScript (Vite)
- TanStack Query (+ DevTools)
- Tailwind CSS v4
- React Router

## Setup

```bash
cd frontend
npm install
```

## Run

Backend must be running on `http://localhost:8000` (`cd backend && pdm run dev`).

```bash
npm run dev
```

App: `http://localhost:5173`

Vite proxies `/api/*` → `http://localhost:8000/*`.

## Routes

| Path | Description |
|------|-------------|
| `/login` | Email/password → `POST /auth`, stores `Coder-Session-Token` |
| `/workspaces` | List / create / delete workspaces (auth required) |

## Config

| Variable | Default | Description |
|----------|---------|-------------|
| `VITE_DEFAULT_TEMPLATE_ID` | `93c85ebe-899b-4adb-8f68-d01ed67ca304` | Template used when launching workspaces |
