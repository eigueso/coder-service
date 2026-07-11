import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { getBuildLogs } from '../api/client'
import { ApiError } from '../api/types'

type BuildLogsAccordionProps = {
  buildId: string
  active: boolean
}

function logLevelClass(level: string | null): string {
  switch (level) {
    case 'error':
      return 'text-red-300'
    case 'warn':
      return 'text-amber-200'
    case 'debug':
    case 'trace':
      return 'text-slate-400'
    default:
      return 'text-emerald-100/90'
  }
}

export function BuildLogsAccordion({ buildId, active }: BuildLogsAccordionProps) {
  const [open, setOpen] = useState(active)
  const scrollerRef = useRef<HTMLPreElement>(null)
  const stickToBottomRef = useRef(true)

  useEffect(() => {
    stickToBottomRef.current = true
    setOpen(active)
  }, [buildId, active])

  const logsQuery = useQuery({
    queryKey: ['build-logs', buildId],
    enabled: Boolean(buildId) && (active || open),
    refetchInterval: active ? 2000 : false,
    queryFn: () => getBuildLogs(buildId),
  })

  const logs = logsQuery.data ?? []

  useEffect(() => {
    if (!stickToBottomRef.current || !scrollerRef.current) return
    scrollerRef.current.scrollTop = scrollerRef.current.scrollHeight
  }, [logs])

  function onScroll() {
    const el = scrollerRef.current
    if (!el) return
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
    stickToBottomRef.current = distanceFromBottom < 40
  }

  return (
    <details
      open={open}
      onToggle={(event) => setOpen((event.target as HTMLDetailsElement).open)}
      className="mt-4 border-t border-line pt-3"
    >
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-sm font-medium text-ink [&::-webkit-details-marker]:hidden">
        <span className="flex items-center gap-2">
          <span className="text-ink-muted">{open ? '▾' : '▸'}</span>
          Build logs
          {active ? (
            <span className="rounded-full bg-amber-100 px-2 py-0.5 font-mono text-[10px] tracking-wide text-warn uppercase">
              live
            </span>
          ) : null}
        </span>
        <span className="font-mono text-[11px] text-ink-muted">
          {logsQuery.isFetching && logs.length === 0
            ? 'loading…'
            : `${logs.length} line${logs.length === 1 ? '' : 's'}`}
        </span>
      </summary>

      <div className="mt-3 overflow-hidden rounded-xl border border-ink/10 bg-ink">
        {logsQuery.isError ? (
          <p className="px-3 py-2 font-mono text-xs text-red-300" role="alert">
            {logsQuery.error instanceof ApiError
              ? logsQuery.error.message
              : 'Failed to load build logs'}
          </p>
        ) : null}

        {!logsQuery.isError && logs.length === 0 ? (
          <p className="px-3 py-3 font-mono text-xs text-slate-400">
            {active ? 'Waiting for provisioner output…' : 'No logs for this build.'}
          </p>
        ) : null}

        {logs.length > 0 ? (
          <pre
            ref={scrollerRef}
            onScroll={onScroll}
            className="max-h-64 overflow-auto px-3 py-3 font-mono text-[11px] leading-5 whitespace-pre-wrap"
          >
            {logs.map((entry) => (
              <div key={entry.id} className={logLevelClass(entry.log_level)}>
                <span className="text-slate-500">
                  [{entry.stage ?? 'provision'}]
                  {entry.log_level ? ` ${entry.log_level}` : ''}
                </span>{' '}
                {entry.output ?? ''}
              </div>
            ))}
          </pre>
        ) : null}
      </div>
    </details>
  )
}
