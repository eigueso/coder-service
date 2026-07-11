import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react'
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
import { useAuth } from '../auth/useAuth'
import { BuildLogsAccordion } from '../components/BuildLogsAccordion'
import { StartupScriptAccordion } from '../components/StartupScriptAccordion'
import { generateWorkspaceName } from '../lib/generateWorkspaceName'
import { Boxes, ChevronDown, Code2, Cpu, ExternalLink, HardDrive, LogOut, MonitorCog, Plus, Sparkles, Terminal, Trash2 } from 'lucide-react'
import { AnimatePresence, motion } from 'motion/react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

const DEFAULT_TEMPLATE_ID =
  import.meta.env.VITE_DEFAULT_TEMPLATE_ID ?? '93c85ebe-899b-4adb-8f68-d01ed67ca304'
const DEFAULT_TEMPLATE_NAME =
  import.meta.env.VITE_DEFAULT_TEMPLATE_NAME ?? 'Kubernetes workspace'

const CPU_OPTIONS = ['2', '4', '6', '8']
const MEMORY_OPTIONS = ['2', '4', '6', '8']
const EMPTY_WORKSPACES: Workspace[] = []

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

function isWorkspaceStarting(workspace: Workspace): boolean {
  const build = workspace.latest_build
  return (build.transition === 'start' && isActiveBuild(build.status)) ||
    (isWorkspaceStarted(workspace) && !workspace.startup_ready)
}

function needsWorkspacePolling(workspaces: Workspace[]): boolean {
  return workspaces.some((ws) => {
    if (isActiveBuild(ws.latest_build.status)) return true
    // Keep polling after Terraform until the agent startup script finishes.
    return isWorkspaceStarted(ws) && !ws.startup_ready
  })
}

type AccessButtonProps = {
  label: string
  displayLabel: string
  title: string
  disabled?: boolean
  onClick?: () => void
  href?: string
  compact?: boolean
  children: ReactNode
}

type ResourceSelectProps = {
  label: string
  value: string
  options: readonly string[]
  unit: string
  onChange: (value: string) => void
}

function ResourceSelect({ label, value, options, unit, onChange }: ResourceSelectProps) {
  return (
    <label className="text-sm font-medium sm:col-span-1">
      {label}
      <select
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="mt-2 h-10 w-full rounded-md border border-input bg-background px-3 text-sm outline-none transition focus:border-ring focus:ring-2 focus:ring-ring/20"
      >
        {options.map((option) => <option key={option} value={option}>{option} {unit}</option>)}
      </select>
    </label>
  )
}

function AccessButton({ label, displayLabel, title, disabled, onClick, href, compact, children }: AccessButtonProps) {
  const className =
    `inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-background text-sm font-medium text-foreground transition-colors hover:border-primary hover:bg-primary hover:text-primary-foreground disabled:cursor-not-allowed disabled:opacity-40 ${compact ? 'w-10 justify-center px-0' : 'justify-start px-3'}`

  if (href && !disabled) {
    return (
      <a href={href} target="_blank" rel="noreferrer" title={title} aria-label={label} className={className}>
        {children}<span className={compact ? 'sr-only' : undefined}>{displayLabel}</span>{!compact ? <ExternalLink className="h-3.5 w-3.5 opacity-60" /> : null}
      </a>
    )
  }

  return (
    <button type="button" title={title} aria-label={label} disabled={disabled} onClick={onClick} className={className}>
      {children}<span className={compact ? 'sr-only' : undefined}>{displayLabel}</span>
    </button>
  )
}

function WorkspaceAccessActions({
  workspace,
  disabled = false,
  compact = false,
}: {
  workspace: Workspace
  disabled?: boolean
  compact?: boolean
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
  const connectionStatus = waitingForStartup
    ? 'Preparing tools…'
    : ready
      ? 'Ready to connect'
      : disabled
        ? 'Build in progress'
        : 'Not available'

  const actions = (
    <>
      <AccessButton
        label={`Open terminal for ${workspace.name}`}
        displayLabel="Terminal"
        title={waitingForStartup ? 'Waiting for startup script…' : 'Terminal'}
        disabled={!ready || !access?.has_terminal}
        href={ready && access?.has_terminal ? openTerminalUrl(workspace.name) : undefined}
        compact={compact}
      >
        <Terminal className="h-4 w-4" />
      </AccessButton>

      <AccessButton
        label={`Open VS Code Browser for ${workspace.name}`}
        displayLabel="VS Code web"
        title={waitingForStartup ? 'Waiting for startup script…' : 'VS Code Browser'}
        disabled={!ready || !access?.has_vscode_browser}
        href={ready && access?.has_vscode_browser ? openCodeServerUrl(workspace.name) : undefined}
        compact={compact}
      >
        <Code2 className="h-4 w-4" />
      </AccessButton>

      <AccessButton
        label={`Open VS Code Desktop for ${workspace.name}`}
        displayLabel={desktopMutation.isPending ? 'Opening…' : 'VS Code desktop'}
        title={waitingForStartup ? 'Waiting for startup script…' : 'VS Code Desktop'}
        disabled={!ready || !access?.has_vscode_desktop || desktopMutation.isPending}
        onClick={() => desktopMutation.mutate()}
        compact={compact}
      >
        <MonitorCog className="h-4 w-4" />
      </AccessButton>
    </>
  )

  if (compact) {
    return <div className="flex items-center gap-1" title={waitingForStartup ? 'Waiting for startup script…' : undefined}>{actions}</div>
  }

  return (
    <div className="rounded-xl border border-border bg-muted/40 p-4" title={waitingForStartup ? 'Waiting for startup script…' : undefined}>
      <div className="mb-3 flex items-center justify-between gap-3"><p className="text-sm font-medium">Open with</p><p className="text-xs text-muted-foreground">{connectionStatus}</p></div>
      <div className="flex flex-wrap justify-start gap-2">{actions}</div>
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
  const [isCreateOpen, setCreateOpen] = useState(false)
  const [workspacePendingDeletion, setWorkspacePendingDeletion] = useState<Workspace | null>(null)
  const [expandedWorkspaceId, setExpandedWorkspaceId] = useState<string | null>(null)
  const [autoExpandedWorkspaceId, setAutoExpandedWorkspaceId] = useState<string | null>(null)
  const [workspacePendingFocusId, setWorkspacePendingFocusId] = useState<string | null>(null)
  const workspaceRefs = useRef(new Map<string, HTMLLIElement>())

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
    onSuccess: async (workspace) => {
      setName('')
      setNameSuggestion(generateWorkspaceName())
      setFormError(null)
      setCreateOpen(false)
      setExpandedWorkspaceId(workspace.id)
      setAutoExpandedWorkspaceId(workspace.id)
      setWorkspacePendingFocusId(workspace.id)
      await queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    },
    onError: (error) => {
      setFormError(error instanceof ApiError ? error.message : 'Failed to create workspace')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (workspaceName: string) => deleteWorkspace(workspaceName),
    onSuccess: async () => {
      setWorkspacePendingDeletion(null)
      await queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    },
  })

  const workspaces = workspacesQuery.data?.workspaces ?? EMPTY_WORKSPACES
  const sorted = useMemo(
    () => [...workspaces].sort((a, b) => a.name.localeCompare(b.name)),
    [workspaces],
  )

  useEffect(() => {
    if (!autoExpandedWorkspaceId) return
    const workspace = workspaces.find(({ id }) => id === autoExpandedWorkspaceId)
    if (!workspace || isActiveBuild(workspace.latest_build.status) || !workspace.startup_ready) return

    setExpandedWorkspaceId((expanded) => expanded === workspace.id ? null : expanded)
    setAutoExpandedWorkspaceId(null)
  }, [autoExpandedWorkspaceId, workspaces])

  useEffect(() => {
    if (!workspacePendingFocusId || !workspaces.some(({ id }) => id === workspacePendingFocusId)) return
    requestAnimationFrame(() => workspaceRefs.current.get(workspacePendingFocusId)?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
    setWorkspacePendingFocusId(null)
  }, [workspacePendingFocusId, workspaces])

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

  function onDelete() {
    if (!workspacePendingDeletion) return
    deleteMutation.mutate(workspacePendingDeletion.name)
  }

  function toggleWorkspace(workspaceId: string) {
    const willExpand = expandedWorkspaceId !== workspaceId
    setExpandedWorkspaceId(willExpand ? workspaceId : null)
    setAutoExpandedWorkspaceId(null)
    if (willExpand) {
      requestAnimationFrame(() => workspaceRefs.current.get(workspaceId)?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
    }
  }

  return (
    <main className="min-h-screen bg-background">
      <header className="border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80">
        <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-5 sm:px-8">
          <Link to="/workspaces" className="flex items-center gap-3">
            <span className="grid h-9 w-9 place-items-center rounded-lg bg-primary text-primary-foreground shadow-sm"><Boxes className="h-5 w-5" /></span>
            <span className="text-sm font-semibold tracking-tight">Coder Service</span>
          </Link>
          <Button type="button" variant="ghost" onClick={logout} className="text-muted-foreground">
            <LogOut className="h-4 w-4" />
            <span className="hidden sm:inline">Sign out</span>
          </Button>
        </div>
      </header>

      <div className="mx-auto max-w-6xl px-5 py-9 sm:px-8 sm:py-12">
        <div className="mb-9 flex flex-wrap items-end justify-between gap-5">
          <div className="max-w-2xl">
            <p className="mb-3 flex items-center gap-2 text-sm font-medium text-primary"><Sparkles className="h-4 w-4" /> Development environments</p>
            <h1 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">Your workspaces</h1>
            <p className="mt-3 text-base leading-7 text-muted-foreground">Open what you need now, or create a fresh environment when you’re ready to start something new.</p>
          </div>
          <Button type="button" size="lg" onClick={() => setCreateOpen((open) => !open)} aria-expanded={isCreateOpen}>
            <Plus className="h-4 w-4" /> {isCreateOpen ? 'Close setup' : 'New workspace'}
          </Button>
        </div>

      <AnimatePresence initial={false}>
      {isCreateOpen ? <motion.section
        key="workspace-setup"
        initial={{ opacity: 0, height: 0, y: -8 }}
        animate={{ opacity: 1, height: 'auto', y: 0 }}
        exit={{ opacity: 0, height: 0, y: -8 }}
        transition={{ duration: 0.2, ease: 'easeOut' }}
        className="mb-10 overflow-hidden rounded-xl border border-border bg-card shadow-sm"
      >
        <div className="border-b border-border px-5 py-5 sm:px-6">
          <div className="flex items-start gap-3">
            <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary"><Plus className="h-5 w-5" /></span>
            <div><h2 className="font-semibold text-card-foreground">New workspace</h2><p className="mt-0.5 text-sm text-muted-foreground">Choose resources for your Kubernetes workspace.</p></div>
          </div>
        </div>

        <form onSubmit={onCreate} className="grid gap-5 p-5 sm:grid-cols-2 sm:p-6 lg:grid-cols-6">
          <label className="text-sm font-medium sm:col-span-2 lg:col-span-3">
            Name
            <Input
              required
              pattern="^[a-zA-Z0-9]([a-zA-Z0-9-]{0,31})$"
              title="Letters, numbers, hyphens. Max 32 chars."
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-workspace"
              className="mt-2 h-10 w-full"
            />
            {!name.trim() ? (
              <p className="mt-2 text-xs font-normal text-muted-foreground">
                Need a suggestion?{' '}
                <button
                  type="button"
                  onClick={() => {
                    setName(nameSuggestion)
                    setNameSuggestion(generateWorkspaceName())
                  }}
                  className="font-medium text-primary underline-offset-2 hover:underline"
                >
                  {nameSuggestion}
                </button>
              </p>
            ) : null}
          </label>

          <ResourceSelect label="CPU" value={cpu} options={CPU_OPTIONS} unit="cores" onChange={setCpu} />
          <ResourceSelect label="Memory" value={memory} options={MEMORY_OPTIONS} unit="GB" onChange={setMemory} />

          <label className="text-sm font-medium sm:col-span-1">
            Disk (GB)
            <Input
              type="number"
              min={1}
              max={99999}
              required
              value={disk}
              onChange={(e) => setDisk(e.target.value)}
              className="mt-2 h-10 w-full"
            />
          </label>

          <div className="flex items-end sm:col-span-2 lg:col-span-4">
            {formError ? (
              <Alert variant="destructive"><AlertDescription>{formError}</AlertDescription></Alert>
            ) : (
              <p className="flex items-center gap-2 text-xs text-muted-foreground"><Cpu className="h-3.5 w-3.5" /> Template: <span className="font-medium text-foreground">{DEFAULT_TEMPLATE_NAME}</span></p>
            )}
          </div>

          <div className="flex items-end justify-end sm:col-span-2 lg:col-span-2">
            <Button
              type="submit"
              disabled={createMutation.isPending}
              className="h-10 w-full sm:w-auto"
            >
              <Plus className="h-4 w-4" /> {createMutation.isPending ? 'Launching…' : 'Create workspace'}
            </Button>
          </div>
        </form>
      </motion.section> : null}
      </AnimatePresence>

      <section className="mt-10">
        <div className="mb-4 flex items-baseline justify-between gap-3">
          <div><h2 className="text-lg font-semibold">Active workspaces</h2><p className="mt-1 text-sm text-muted-foreground">Your running environments stay front and center.</p></div>
          <p className="rounded-full bg-muted px-2.5 py-1 text-xs font-medium text-muted-foreground">
            {workspacesQuery.isLoading ? 'loading…' : `${sorted.length} total`}
          </p>
        </div>

        {workspacesQuery.isLoading ? (
          <p className="rounded-xl border border-dashed border-border bg-card px-5 py-10 text-sm text-muted-foreground">
            Loading workspaces…
          </p>
        ) : null}

        {workspacesQuery.isError ? (
          <p className="rounded-lg bg-destructive/10 px-5 py-4 text-sm text-destructive" role="alert">
            {workspacesQuery.error instanceof ApiError
              ? workspacesQuery.error.message
              : 'Failed to load workspaces'}
          </p>
        ) : null}

        {!workspacesQuery.isLoading && !workspacesQuery.isError && sorted.length === 0 ? (
          <div className="rounded-xl border border-dashed border-border bg-card px-5 py-12 text-center"><Boxes className="mx-auto h-8 w-8 text-muted-foreground/60" /><p className="mt-3 text-sm font-medium">No workspaces yet</p><p className="mt-1 text-sm text-muted-foreground">Create an environment and it will appear here when it’s ready.</p><Button type="button" variant="outline" className="mt-5" onClick={() => setCreateOpen(true)}><Plus className="h-4 w-4" /> Create workspace</Button></div>
        ) : null}

        <motion.ul layout className="mt-4 space-y-3">
          <AnimatePresence initial={false}>
          {sorted.map((workspace) => {
            const deleting =
              deleteMutation.isPending && deleteMutation.variables === workspace.name
            const build = workspace.latest_build
            const buildActive = isActiveBuild(build.status)
            const expanded = expandedWorkspaceId === workspace.id

            return (
              <motion.li
                key={workspace.id}
                ref={(element) => {
                  if (element) workspaceRefs.current.set(workspace.id, element)
                  else workspaceRefs.current.delete(workspace.id)
                }}
                layout="position"
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -12 }}
                transition={{ duration: 0.2, ease: 'easeOut' }}
                className="overflow-hidden rounded-xl border border-border bg-card shadow-sm transition-shadow hover:shadow-md"
              >
                <div className="flex flex-wrap items-center justify-between gap-3 p-4 sm:px-5">
                  <button
                    type="button"
                    className="flex min-w-0 flex-1 items-center gap-3 text-left"
                    aria-expanded={expanded}
                    onClick={() => toggleWorkspace(workspace.id)}
                  >
                    <span className="grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-primary text-primary-foreground"><HardDrive className="h-4 w-4" /></span>
                    <span className="min-w-0">
                      <span className="flex flex-wrap items-center gap-2">
                        <span className="truncate text-base font-semibold">{workspace.name}</span>
                        <Badge className={`tracking-wide uppercase ${statusTone(build.status)}`}>{build.status}</Badge>
                        {build.transition ? <span className="font-mono text-[11px] text-ink-muted uppercase">{build.transition}</span> : null}
                      </span>
                      <span className="mt-1 block font-mono text-xs text-muted-foreground">Build #{build.build_number ?? '—'}</span>
                    </span>
                    <ChevronDown className={`ml-auto h-4 w-4 shrink-0 text-muted-foreground transition-transform ${expanded ? 'rotate-180' : ''}`} />
                  </button>

                  <div className="flex items-center gap-2">
                    {!expanded && isWorkspaceStarted(workspace) ? <WorkspaceAccessActions workspace={workspace} disabled={deleting || buildActive} compact /> : null}
                    <Button
                      type="button"
                      variant="destructive"
                      size="sm"
                      onClick={() => setWorkspacePendingDeletion(workspace)}
                      disabled={deleting || buildActive}
                      className="bg-transparent shadow-none"
                    >
                      <Trash2 className="h-3.5 w-3.5" /> {deleting ? 'Deleting…' : 'Delete'}
                    </Button>
                  </div>
                </div>

                <AnimatePresence initial={false}>
                {expanded ? (
                  <motion.div
                    initial={{ opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: 'auto' }}
                    exit={{ opacity: 0, height: 0 }}
                    transition={{ duration: 0.2, ease: 'easeOut' }}
                    className="overflow-hidden border-t border-border"
                  >
                    <div className="px-5 py-5">
                    <WorkspaceAccessActions workspace={workspace} disabled={deleting || buildActive} />
                    <BuildLogsAccordion buildId={build.id} active={buildActive} autoOpen={build.transition !== 'delete'} />
                    <StartupScriptAccordion workspaceName={workspace.name} active={isWorkspaceStarting(workspace)} />
                    </div>
                  </motion.div>
                ) : null}
                </AnimatePresence>
              </motion.li>
            )
          })}
          </AnimatePresence>
        </motion.ul>

        {deleteMutation.isError ? (
          <Alert variant="destructive" className="mt-4"><AlertDescription>{deleteMutation.error instanceof ApiError
              ? deleteMutation.error.message
              : 'Failed to delete workspace'}</AlertDescription></Alert>
        ) : null}
      </section>
      </div>
      <Dialog
        open={workspacePendingDeletion !== null}
        onOpenChange={(open) => {
          if (!open && !deleteMutation.isPending) setWorkspacePendingDeletion(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete workspace?</DialogTitle>
            <DialogDescription>
              This starts a delete build for <span className="font-medium text-foreground">{workspacePendingDeletion?.name}</span>. Its files and environment will be removed.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={deleteMutation.isPending} onClick={() => setWorkspacePendingDeletion(null)}>Cancel</Button>
            <Button type="button" variant="destructive" disabled={deleteMutation.isPending} onClick={onDelete}>{deleteMutation.isPending ? 'Deleting…' : 'Delete workspace'}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </main>
  )
}
