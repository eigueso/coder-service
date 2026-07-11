import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import {
  createVSCodeDesktopLink,
  createWorkspace,
  deleteWorkspace,
  getWorkspaceAccess,
  listWorkspaces,
  openCodeServerUrl,
  openTerminalUrl,
} from '../api/client'
import { ApiError, type Workspace } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { TerminalIcon, VSCodeBrowserIcon, VSCodeDesktopIcon } from '../components/AccessIcons'
import { BuildLogsAccordion } from '../components/BuildLogsAccordion'
import { StartupScriptAccordion } from '../components/StartupScriptAccordion'
import { generateWorkspaceName } from '../lib/generateWorkspaceName'

const DEFAULT_TEMPLATE_ID =
  import.meta.env.VITE_DEFAULT_TEMPLATE_ID ?? '93c85ebe-899b-4adb-8f68-d01ed67ca304'

const CPU_OPTIONS = ['2', '4', '6', '8']
const MEMORY_OPTIONS = ['2', '4', '6', '8']

function statusTone(status: string): string {
  switch (status) {
    case 'succeeded':
      return 'bg-accent-soft text-ok'
    case 'running':
    case 'pending':
      return 'bg-amber-100 text-warn'
    case 'failed':
    case 'canceled':
      return 'bg-danger-soft text-danger'
    default:
      return 'bg-surface text-ink-muted'
  }
}

function isActiveBuild(status: string): boolean {
  return status === 'pending' || status === 'running'
}

function isWorkspaceStarted(workspace: Workspace): boolean {
  const { status, transition } = workspace.latest_build
  return status === 'succeeded' && transition === 'start'
}

function needsWorkspacePolling(workspaces: Workspace[]): boolean {
  return workspaces.some((ws) => {
    if (isActiveBuild(ws.latest_build.status)) return true
    // Keep polling after Terraform until the agent startup script finishes.
    return isWorkspaceStarted(ws) && !ws.startup_ready
  })
}

function AccessButton({
  label,
  title,
  disabled,
  onClick,
  href,
  to,
  children,
}: {
  label: string
  title: string
  disabled?: boolean
  onClick?: () => void
  href?: string
  to?: string
  children: ReactNode
}) {
  const className =
    'inline-flex h-10 w-10 items-center justify-center rounded-xl border border-accent/30 bg-accent-soft text-accent transition hover:border-accent hover:bg-accent hover:text-white disabled:cursor-not-allowed disabled:opacity-40'

  if (to && !disabled) {
    return (
      <Link to={to} title={title} aria-label={label} className={className}>
        {children}
      </Link>
    )
  }

  if (href && !disabled) {
    return (
      <a href={href} target="_blank" rel="noreferrer" title={title} aria-label={label} className={className}>
        {children}
      </a>
    )
  }

  return (
    <button type="button" title={title} aria-label={label} disabled={disabled} onClick={onClick} className={className}>
      {children}
    </button>
  )
}

function WorkspaceAccessActions({
  workspace,
  disabled = false,
}: {
  workspace: Workspace
  disabled?: boolean
}) {
  const started = isWorkspaceStarted(workspace)
  const accessQuery = useQuery({
    queryKey: ['workspace-access', workspace.name],
    queryFn: () => getWorkspaceAccess(workspace.name),
    enabled: started && !disabled,
    refetchInterval: (query) => {
      const access = query.state.data
      if (!started || disabled) return false
      // Poll until the startup script completes (lifecycle_state === ready).
      if (!access?.startup_ready) return 2000
      return false
    },
  })

  const desktopMutation = useMutation({
    mutationFn: () => createVSCodeDesktopLink(workspace.name),
    onSuccess: (data) => {
      window.location.href = data.uri
    },
  })

  const access = accessQuery.data
  const ready = !disabled && Boolean(access?.startup_ready)
  const waitingForStartup = !disabled && started && !access?.startup_ready

  return (
    <div className="flex items-center gap-2" title={waitingForStartup ? 'Waiting for startup script…' : undefined}>
      <AccessButton
        label={`Open terminal for ${workspace.name}`}
        title={waitingForStartup ? 'Waiting for startup script…' : 'Terminal'}
        disabled={!ready || !access?.has_terminal}
        href={ready && access?.has_terminal ? openTerminalUrl(workspace.name) : undefined}
      >
        <TerminalIcon className="h-5 w-5" />
      </AccessButton>

      <AccessButton
        label={`Open VS Code Browser for ${workspace.name}`}
        title={waitingForStartup ? 'Waiting for startup script…' : 'VS Code Browser'}
        disabled={!ready || !access?.has_vscode_browser}
        href={ready && access?.has_vscode_browser ? openCodeServerUrl(workspace.name) : undefined}
      >
        <VSCodeBrowserIcon className="h-5 w-5" />
      </AccessButton>

      <AccessButton
        label={`Open VS Code Desktop for ${workspace.name}`}
        title={waitingForStartup ? 'Waiting for startup script…' : 'VS Code Desktop'}
        disabled={!ready || !access?.has_vscode_desktop || desktopMutation.isPending}
        onClick={() => desktopMutation.mutate()}
      >
        <VSCodeDesktopIcon className="h-5 w-5" />
      </AccessButton>
    </div>
  )
}

export function WorkspacesPage() {
  const { logout } = useAuth()
  const queryClient = useQueryClient()

  const [name, setName] = useState('')
  const [nameSuggestion, setNameSuggestion] = useState(() => generateWorkspaceName())
  const [cpu, setCpu] = useState('2')
  const [memory, setMemory] = useState('2')
  const [disk, setDisk] = useState('10')
  const [formError, setFormError] = useState<string | null>(null)

  const workspacesQuery = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
    refetchInterval: (query) => {
      const data = query.state.data
      if (!data) return false
      return needsWorkspacePolling(data.workspaces) ? 2500 : false
    },
  })

  const createMutation = useMutation({
    mutationFn: createWorkspace,
    onSuccess: async () => {
      setName('')
      setNameSuggestion(generateWorkspaceName())
      setFormError(null)
      await queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    },
    onError: (error) => {
      setFormError(error instanceof ApiError ? error.message : 'Failed to create workspace')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (workspaceName: string) => deleteWorkspace(workspaceName),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    },
  })

  const workspaces = workspacesQuery.data?.workspaces ?? []
  const sorted = useMemo(
    () => [...workspaces].sort((a, b) => a.name.localeCompare(b.name)),
    [workspaces],
  )

  function onCreate(event: FormEvent) {
    event.preventDefault()
    setFormError(null)
    createMutation.mutate({
      name: name.trim(),
      template_id: DEFAULT_TEMPLATE_ID,
      rich_parameter_values: [
        { name: 'cpu', value: cpu },
        { name: 'memory', value: memory },
        { name: 'home_disk_size', value: disk },
      ],
    })
  }

  function onDelete(workspace: Workspace) {
    const confirmed = window.confirm(
      `Delete workspace "${workspace.name}"? This starts a delete build in Coder.`,
    )
    if (!confirmed) return
    deleteMutation.mutate(workspace.name)
  }

  return (
    <main className="mx-auto min-h-screen max-w-5xl px-6 py-10">
      <header className="flex flex-wrap items-end justify-between gap-4 border-b border-line pb-6">
        <div>
          <p className="font-mono text-xs tracking-[0.2em] text-accent uppercase">coder-service</p>
          <h1 className="mt-2 text-3xl font-semibold tracking-tight">Workspaces</h1>
          <p className="mt-1 text-sm text-ink-muted">
            Launch, monitor, and delete workspaces backed by your Coder deployment.
          </p>
        </div>
        <button
          type="button"
          onClick={logout}
          className="rounded-xl border border-line bg-panel px-4 py-2 text-sm font-medium text-ink-muted transition hover:border-ink/20 hover:text-ink"
        >
          Sign out
        </button>
      </header>

      <section className="mt-8 rounded-2xl border border-line bg-panel/90 p-6 shadow-[0_20px_60px_-45px_rgba(15,28,26,0.5)]">
        <h2 className="text-lg font-semibold">Launch workspace</h2>
        <p className="mt-1 text-sm text-ink-muted">
          Uses the Kubernetes template with cpu / memory / disk rich parameters.
        </p>

        <form onSubmit={onCreate} className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <label className="sm:col-span-2 lg:col-span-2 text-sm font-medium">
            Name
            <input
              required
              pattern="^[a-zA-Z0-9]([a-zA-Z0-9-]{0,31})$"
              title="Letters, numbers, hyphens. Max 32 chars."
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-workspace"
              className="mt-2 w-full rounded-xl border border-line bg-surface px-3 py-2.5 outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
            {!name.trim() ? (
              <p className="mt-2 text-xs font-normal text-ink-muted">
                Need a suggestion?{' '}
                <button
                  type="button"
                  onClick={() => {
                    setName(nameSuggestion)
                    setNameSuggestion(generateWorkspaceName())
                  }}
                  className="font-mono text-accent underline-offset-2 hover:underline"
                >
                  {nameSuggestion}
                </button>
              </p>
            ) : null}
          </label>

          <label className="text-sm font-medium">
            CPU
            <select
              value={cpu}
              onChange={(e) => setCpu(e.target.value)}
              className="mt-2 w-full rounded-xl border border-line bg-surface px-3 py-2.5 outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            >
              {CPU_OPTIONS.map((value) => (
                <option key={value} value={value}>
                  {value} cores
                </option>
              ))}
            </select>
          </label>

          <label className="text-sm font-medium">
            Memory
            <select
              value={memory}
              onChange={(e) => setMemory(e.target.value)}
              className="mt-2 w-full rounded-xl border border-line bg-surface px-3 py-2.5 outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            >
              {MEMORY_OPTIONS.map((value) => (
                <option key={value} value={value}>
                  {value} GB
                </option>
              ))}
            </select>
          </label>

          <label className="text-sm font-medium">
            Disk (GB)
            <input
              type="number"
              min={1}
              max={99999}
              required
              value={disk}
              onChange={(e) => setDisk(e.target.value)}
              className="mt-2 w-full rounded-xl border border-line bg-surface px-3 py-2.5 outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
          </label>

          <div className="flex items-end sm:col-span-2 lg:col-span-3">
            {formError ? (
              <p className="rounded-xl bg-danger-soft px-3 py-2 text-sm text-danger" role="alert">
                {formError}
              </p>
            ) : (
              <p className="font-mono text-xs text-ink-muted">template {DEFAULT_TEMPLATE_ID}</p>
            )}
          </div>

          <div className="flex items-end justify-end">
            <button
              type="submit"
              disabled={createMutation.isPending}
              className="w-full rounded-xl bg-accent px-4 py-2.5 text-sm font-semibold text-white transition hover:bg-accent-strong disabled:cursor-not-allowed disabled:opacity-60 sm:w-auto"
            >
              {createMutation.isPending ? 'Launching…' : 'Launch'}
            </button>
          </div>
        </form>
      </section>

      <section className="mt-8">
        <div className="mb-4 flex items-baseline justify-between gap-3">
          <h2 className="text-lg font-semibold">Your workspaces</h2>
          <p className="font-mono text-xs text-ink-muted">
            {workspacesQuery.isLoading ? 'loading…' : `${sorted.length} total`}
          </p>
        </div>

        {workspacesQuery.isLoading ? (
          <p className="rounded-2xl border border-dashed border-line bg-panel/60 px-5 py-10 text-sm text-ink-muted">
            Loading workspaces…
          </p>
        ) : null}

        {workspacesQuery.isError ? (
          <p className="rounded-2xl bg-danger-soft px-5 py-4 text-sm text-danger" role="alert">
            {workspacesQuery.error instanceof ApiError
              ? workspacesQuery.error.message
              : 'Failed to load workspaces'}
          </p>
        ) : null}

        {!workspacesQuery.isLoading && !workspacesQuery.isError && sorted.length === 0 ? (
          <p className="rounded-2xl border border-dashed border-line bg-panel/60 px-5 py-10 text-sm text-ink-muted">
            No workspaces yet. Launch one above to get started.
          </p>
        ) : null}

        <ul className="space-y-3">
          {sorted.map((workspace) => {
            const deleting =
              deleteMutation.isPending && deleteMutation.variables === workspace.name
            const build = workspace.latest_build
            const buildActive = isActiveBuild(build.status)

            return (
              <li
                key={workspace.id}
                className="rounded-2xl border border-line bg-panel px-5 py-4"
              >
                <div className="flex flex-wrap items-center justify-between gap-4">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="truncate text-base font-semibold">{workspace.name}</h3>
                      <span
                        className={`rounded-full px-2.5 py-0.5 font-mono text-[11px] tracking-wide uppercase ${statusTone(build.status)}`}
                      >
                        {build.status}
                      </span>
                      {build.transition ? (
                        <span className="font-mono text-[11px] text-ink-muted uppercase">
                          {build.transition}
                        </span>
                      ) : null}
                    </div>
                    <p className="mt-1 font-mono text-xs text-ink-muted">
                      build #{build.build_number ?? '—'} · {build.id}
                    </p>
                  </div>

                  <div className="flex flex-wrap items-center gap-2">
                    <WorkspaceAccessActions
                      workspace={workspace}
                      disabled={deleting || buildActive}
                    />
                    <button
                      type="button"
                      onClick={() => onDelete(workspace)}
                      disabled={deleting || buildActive}
                      className="rounded-xl border border-danger/30 bg-danger-soft px-3 py-2 text-sm font-medium text-danger transition hover:border-danger disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {deleting ? 'Deleting…' : 'Delete'}
                    </button>
                  </div>
                </div>

                <BuildLogsAccordion buildId={build.id} active={buildActive} />
                <StartupScriptAccordion
                  workspaceName={workspace.name}
                  active={
                    buildActive ||
                    (isWorkspaceStarted(workspace) && !workspace.startup_ready)
                  }
                />
              </li>
            )
          })}
        </ul>

        {deleteMutation.isError ? (
          <p className="mt-4 rounded-xl bg-danger-soft px-3 py-2 text-sm text-danger" role="alert">
            {deleteMutation.error instanceof ApiError
              ? deleteMutation.error.message
              : 'Failed to delete workspace'}
          </p>
        ) : null}
      </section>
    </main>
  )
}
