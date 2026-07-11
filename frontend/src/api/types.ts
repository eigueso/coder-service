export type BuildSummary = {
  id: string
  status: string
  build_number: number | null
  transition: string | null
}

export type Workspace = {
  id: string
  name: string
  template_id: string | null
  latest_build: BuildSummary
  agent_lifecycle_state: string | null
  startup_ready: boolean
}

export type WorkspaceListResponse = {
  count: number
  workspaces: Workspace[]
}

export type WorkspaceBuild = {
  id: string
  workspace_id: string
  build_number: number
  status: string
  transition: string | null
  job_error: string | null
  created_at: string | null
}

export type ProvisionerJobLog = {
  id: number
  created_at: string | null
  log_level: string | null
  log_source: string | null
  output: string | null
  stage: string | null
}

export type WorkspaceAgentLog = {
  id: number
  created_at: string | null
  level: string | null
  output: string | null
  source_id: string | null
}

export type AuthResponse = {
  session_token: string
}

export type MeResponse = {
  username: string
  email: string | null
  dashboard_url: string
}

export type WorkspaceAccess = {
  workspace_id: string
  workspace_name: string
  username: string
  started: boolean
  startup_ready: boolean
  agent_lifecycle_state: string | null
  agent_id: string | null
  agent_name: string | null
  agent_directory: string | null
  has_terminal: boolean
  has_vscode_browser: boolean
  has_vscode_desktop: boolean
  vscode_browser_url: string | null
  code_server_slug: string | null
}

export type VSCodeDesktopResponse = {
  uri: string
}

export type CreateWorkspaceInput = {
  name: string
  template_id: string
  rich_parameter_values: Array<{ name: string; value: string }>
}

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}
