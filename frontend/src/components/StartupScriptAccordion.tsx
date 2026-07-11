import { useQuery } from '@tanstack/react-query'
import { getStartupLogs } from '../api/client'
import { ApiError, type WorkspaceAgentLog } from '../api/types'
import { LogAccordion } from './LogAccordion'
import { useLogAccordionOpen } from './useLogAccordionOpen'

const EMPTY_LOGS: WorkspaceAgentLog[] = []

type StartupScriptAccordionProps = {
  workspaceName: string
  active: boolean
}

export function StartupScriptAccordion({ workspaceName, active }: StartupScriptAccordionProps) {
  const [open, setOpen] = useLogAccordionOpen(workspaceName, active)
  const logsQuery = useQuery({
    queryKey: ['startup-logs', workspaceName],
    queryFn: () => getStartupLogs(workspaceName),
    enabled: Boolean(workspaceName) && (active || open),
    refetchInterval: active || open ? 2500 : false,
    retry: (failureCount, error) => error instanceof ApiError && error.status === 404 ? failureCount < 8 : failureCount < 2,
  })
  const logs = logsQuery.data ?? EMPTY_LOGS
  const waitingForAgent = logsQuery.error instanceof ApiError && logsQuery.error.status === 404
  const errorMessage = logsQuery.isError && !waitingForAgent
    ? logsQuery.error instanceof ApiError ? logsQuery.error.message : 'Failed to load startup script logs'
    : undefined

  return (
    <LogAccordion
      active={active}
      open={open}
      onOpenChange={setOpen}
      title="Startup script"
      logs={logs}
      isLoading={logsQuery.data === undefined && logsQuery.isFetching}
      errorMessage={errorMessage}
      statusMessage={waitingForAgent ? 'waiting for agent…' : undefined}
      emptyMessage={waitingForAgent || active ? 'Waiting for agent startup script output…' : 'No startup script logs yet.'}
      className="mt-3 border-t border-line pt-3"
      getLevel={(entry) => entry.level}
      renderPrefix={(entry) => entry.level ? `[${entry.level}]` : ''}
    />
  )
}
