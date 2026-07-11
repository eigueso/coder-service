import {
  ApiError,
  type AuthResponse,
  type CreateWorkspaceInput,
  type MeResponse,
  type ProvisionerJobLog,
  type VSCodeDesktopResponse,
  type Workspace,
  type WorkspaceAccess,
  type WorkspaceAgentLog,
  type WorkspaceBuild,
  type WorkspaceListResponse,
} from './types'

const TOKEN_KEY = 'coder_session_token'

function workspacePath(name: string): string {
  return `/workspaces/${encodeURIComponent(name)}`
}

function withQuery(path: string, params: URLSearchParams): string {
  const query = params.toString()
  return query ? `${path}?${query}` : path
}

export function getStoredToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setStoredToken(token: string | null): void {
  if (token) {
    localStorage.setItem(TOKEN_KEY, token)
  } else {
    localStorage.removeItem(TOKEN_KEY)
  }
}

async function parseError(response: Response): Promise<ApiError> {
  let message = response.statusText || 'Request failed'
  try {
    const body = (await response.json()) as { detail?: unknown }
    if (typeof body.detail === 'string') {
      message = body.detail
    } else if (Array.isArray(body.detail)) {
      message = body.detail
        .map((item) => {
          if (typeof item === 'string') return item
          if (item && typeof item === 'object' && 'msg' in item) {
            return String((item as { msg: unknown }).msg)
          }
          return JSON.stringify(item)
        })
        .join('; ')
    }
  } catch {
    // keep statusText
  }
  return new ApiError(response.status, message)
}

async function apiFetch<T>(
  path: string,
  options: RequestInit = {},
  token?: string | null,
): Promise<T> {
  const headers = new Headers(options.headers)
  if (!headers.has('Content-Type') && options.body) {
    headers.set('Content-Type', 'application/json')
  }
  const authToken = token === undefined ? getStoredToken() : token
  if (authToken) {
    headers.set('Coder-Session-Token', authToken)
  }

  const response = await fetch(`/api${path}`, {
    ...options,
    headers,
  })

  if (!response.ok) {
    throw await parseError(response)
  }

  if (response.status === 204) {
    return undefined as T
  }

  return (await response.json()) as T
}

export function login(email: string): Promise<AuthResponse> {
  return apiFetch<AuthResponse>(
    '/auth',
    {
      method: 'POST',
      body: JSON.stringify({ email }),
    },
    null,
  )
}

export function getMe(): Promise<MeResponse> {
  return apiFetch<MeResponse>('/me')
}

export function getWorkspaceAccess(name: string): Promise<WorkspaceAccess> {
  return apiFetch<WorkspaceAccess>(`${workspacePath(name)}/access`)
}

export function openCodeServerUrl(name: string): string {
  const token = getStoredToken()
  const params = new URLSearchParams()
  if (token) params.set('token', token)
  return `/api${withQuery(`${workspacePath(name)}/open/code-server`, params)}`
}

export function openTerminalUrl(name: string): string {
  const token = getStoredToken()
  const params = new URLSearchParams()
  if (token) params.set('token', token)
  return withQuery(`${workspacePath(name)}/terminal`, params)
}

export function createVSCodeDesktopLink(name: string): Promise<VSCodeDesktopResponse> {
  return apiFetch<VSCodeDesktopResponse>(`${workspacePath(name)}/vscode-desktop`, {
    method: 'POST',
  })
}

export function listWorkspaces(): Promise<WorkspaceListResponse> {
  return apiFetch<WorkspaceListResponse>('/workspaces')
}

export function createWorkspace(input: CreateWorkspaceInput): Promise<Workspace> {
  return apiFetch<Workspace>('/workspaces', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function deleteWorkspace(name: string, orphan = false): Promise<WorkspaceBuild> {
  const params = new URLSearchParams()
  if (orphan) params.set('orphan', 'true')
  return apiFetch<WorkspaceBuild>(withQuery(workspacePath(name), params), {
    method: 'DELETE',
  })
}

export function getBuildLogs(
  buildId: string,
  options: { after?: number } = {},
): Promise<ProvisionerJobLog[]> {
  const params = new URLSearchParams()
  if (options.after !== undefined) {
    params.set('after', String(options.after))
  }
  return apiFetch<ProvisionerJobLog[]>(withQuery(`/workspacebuilds/${encodeURIComponent(buildId)}/logs`, params))
}

export function getStartupLogs(
  workspaceName: string,
  options: { after?: number } = {},
): Promise<WorkspaceAgentLog[]> {
  const params = new URLSearchParams()
  if (options.after !== undefined) {
    params.set('after', String(options.after))
  }
  return apiFetch<WorkspaceAgentLog[]>(withQuery(`${workspacePath(workspaceName)}/startup-logs`, params))
}
