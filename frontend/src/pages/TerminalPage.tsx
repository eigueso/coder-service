import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { useEffect, useRef, useState } from 'react'
import { Link, Navigate, useParams, useSearchParams } from 'react-router-dom'
import { getStoredToken, getWorkspaceAccess } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import '@xterm/xterm/css/xterm.css'

function terminalWsBase(): string {
  const configured = import.meta.env.VITE_BACKEND_WS as string | undefined
  if (configured) {
    return configured.replace(/\/$/, '')
  }
  // In Vite dev, proxying WebSockets through :5173 adds major latency. Talk to
  // the FastAPI server directly instead.
  if (import.meta.env.DEV) {
    return 'ws://127.0.0.1:8000'
  }
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${window.location.host}/api`
}

function TerminalSession({
  workspaceName,
  token,
}: {
  workspaceName: string
  token: string
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [status, setStatus] = useState<'connecting' | 'connected' | 'error'>('connecting')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const host = containerRef.current
    if (!host) {
      return
    }

    let disposed = false
    let socket: WebSocket | null = null
    let term: Terminal | null = null
    let dataDisposable: { dispose: () => void } | null = null
    const encoder = new TextEncoder()
    const decoder = new TextDecoder()

    const onResize = () => {
      // no-op until fitAddon exists; replaced below
    }
    let resizeHandler: (() => void) | null = null

    const start = async () => {
      let agentId: string | undefined
      try {
        const access = await getWorkspaceAccess(workspaceName)
        agentId = access.agent_id ?? undefined
      } catch {
        // Proxy can resolve the agent itself.
      }
      if (disposed) return

      const localTerm = new Terminal({
        cursorBlink: true,
        fontFamily: '"IBM Plex Mono", ui-monospace, monospace',
        fontSize: 13,
        scrollback: 5000,
        theme: {
          background: '#0f1c1a',
          foreground: '#e7efec',
          cursor: '#5eead4',
        },
      })
      const fitAddon = new FitAddon()
      localTerm.loadAddon(fitAddon)
      localTerm.open(host)
      fitAddon.fit()
      term = localTerm

      const params = new URLSearchParams({
        token,
        height: String(localTerm.rows),
        width: String(localTerm.cols),
      })
      if (agentId) params.set('agent_id', agentId)

      const localSocket = new WebSocket(
        `${terminalWsBase()}/workspaces/${encodeURIComponent(workspaceName)}/terminal?${params}`,
      )
      localSocket.binaryType = 'arraybuffer'
      socket = localSocket

      localSocket.onopen = () => {
        if (disposed) return
        setStatus('connected')
        setError(null)
        localSocket.send(
          encoder.encode(
            JSON.stringify({
              height: localTerm.rows,
              width: localTerm.cols,
            }),
          ),
        )
      }

      localSocket.onmessage = (event) => {
        if (disposed) return
        if (typeof event.data === 'string') {
          localTerm.write(event.data)
          return
        }
        localTerm.write(decoder.decode(event.data as ArrayBuffer))
      }

      localSocket.onerror = () => {
        if (disposed) return
        setStatus('error')
        setError('WebSocket connection failed')
      }

      localSocket.onclose = (event) => {
        if (disposed) return
        if (event.code !== 1000) {
          setStatus('error')
          setError(event.reason || `Disconnected (${event.code})`)
        }
      }

      dataDisposable = localTerm.onData((data) => {
        if (localSocket.readyState === WebSocket.OPEN) {
          localSocket.send(encoder.encode(JSON.stringify({ data })))
        }
      })

      resizeHandler = () => {
        fitAddon.fit()
        if (localSocket.readyState === WebSocket.OPEN) {
          localSocket.send(
            encoder.encode(
              JSON.stringify({
                height: localTerm.rows,
                width: localTerm.cols,
              }),
            ),
          )
        }
      }
      window.addEventListener('resize', resizeHandler)
      void onResize
    }

    void start()

    return () => {
      disposed = true
      if (resizeHandler) {
        window.removeEventListener('resize', resizeHandler)
      }
      dataDisposable?.dispose()
      if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
        socket.close(1000, 'client dispose')
      }
      term?.dispose()
    }
  }, [workspaceName, token])

  return (
    <div className="flex min-h-screen flex-col bg-ink text-white">
      <header className="flex items-center justify-between border-b border-white/10 px-4 py-3">
        <div>
          <p className="font-mono text-[10px] tracking-[0.2em] text-teal-300 uppercase">
            coder-service terminal
          </p>
          <h1 className="text-sm font-medium">{workspaceName}</h1>
        </div>
        <div className="flex items-center gap-3">
          <span className="font-mono text-[11px] text-white/50">
            {status === 'connecting' ? 'connecting…' : status === 'connected' ? 'connected' : 'error'}
          </span>
          <Link
            to="/workspaces"
            className="rounded-lg border border-white/15 px-3 py-1.5 text-xs text-white/80 transition hover:border-white/40 hover:text-white"
          >
            Back
          </Link>
        </div>
      </header>
      {error ? (
        <p className="border-b border-red-500/30 bg-red-950/50 px-4 py-2 font-mono text-xs text-red-200">
          {error}
        </p>
      ) : null}
      <div ref={containerRef} className="min-h-0 flex-1 p-2" />
    </div>
  )
}

export function TerminalPage() {
  const { name } = useParams<{ name: string }>()
  const [searchParams] = useSearchParams()
  const { setToken } = useAuth()
  const queryToken = searchParams.get('token')
  const token = queryToken || getStoredToken()

  useEffect(() => {
    if (queryToken) {
      setToken(queryToken)
    }
  }, [queryToken, setToken])

  if (!name) {
    return <p className="p-6 text-sm text-ink-muted">Missing workspace name</p>
  }

  if (!token) {
    return <Navigate to="/login" replace state={{ from: `/workspaces/${name}/terminal` }} />
  }

  return <TerminalSession workspaceName={name} token={token} />
}
