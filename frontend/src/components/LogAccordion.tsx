import { useEffect, useRef, type ReactNode } from 'react'

type LogEntry = {
  id: number
  output: string | null
}

type LogAccordionProps<T extends LogEntry> = {
  active: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  logs: readonly T[]
  isLoading: boolean
  errorMessage?: string
  emptyMessage: string
  statusMessage?: string
  className: string
  getLevel: (entry: T) => string | null
  renderPrefix: (entry: T) => ReactNode
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

export function LogAccordion<T extends LogEntry>({
  active,
  open,
  onOpenChange,
  title,
  logs,
  isLoading,
  errorMessage,
  emptyMessage,
  statusMessage,
  className,
  getLevel,
  renderPrefix,
}: LogAccordionProps<T>) {
  const scrollerRef = useRef<HTMLPreElement>(null)
  const stickToBottomRef = useRef(true)

  useEffect(() => {
    if (stickToBottomRef.current && scrollerRef.current) {
      scrollerRef.current.scrollTop = scrollerRef.current.scrollHeight
    }
  }, [logs])

  function onScroll() {
    const element = scrollerRef.current
    if (!element) return
    stickToBottomRef.current = element.scrollHeight - element.scrollTop - element.clientHeight < 40
  }

  const countLabel = isLoading
    ? 'loading…'
    : statusMessage ?? `${logs.length} line${logs.length === 1 ? '' : 's'}`

  return (
    <details
      open={open}
      onToggle={(event) => onOpenChange((event.target as HTMLDetailsElement).open)}
      className={className}
    >
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-sm font-medium text-ink [&::-webkit-details-marker]:hidden">
        <span className="flex items-center gap-2">
          <span className="text-ink-muted">{open ? '▾' : '▸'}</span>
          {title}
          {active ? <span className="rounded-full bg-amber-100 px-2 py-0.5 font-mono text-[10px] tracking-wide text-warn uppercase">live</span> : null}
        </span>
        <span className="font-mono text-[11px] text-ink-muted">{countLabel}</span>
      </summary>

      <div className="mt-3 overflow-hidden rounded-xl border border-ink/10 bg-ink">
        {errorMessage ? <p className="px-3 py-2 font-mono text-xs text-red-300" role="alert">{errorMessage}</p> : null}
        {!errorMessage && logs.length === 0 ? <p className="px-3 py-3 font-mono text-xs text-slate-400">{emptyMessage}</p> : null}
        {logs.length > 0 ? (
          <pre ref={scrollerRef} onScroll={onScroll} className="max-h-64 overflow-auto px-3 py-3 font-mono text-[11px] leading-5 whitespace-pre-wrap">
            {logs.map((entry) => <div key={entry.id} className={logLevelClass(getLevel(entry))}><span className="text-slate-500">{renderPrefix(entry)}</span>{' '}{entry.output ?? ''}</div>)}
          </pre>
        ) : null}
      </div>
    </details>
  )
}
