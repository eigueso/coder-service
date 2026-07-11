import { useQuery } from '@tanstack/react-query'
import { getBuildLogs } from '../api/client'
import { ApiError, type ProvisionerJobLog } from '../api/types'
import { LogAccordion } from './LogAccordion'
import { useLogAccordionOpen } from './useLogAccordionOpen'

const EMPTY_LOGS: ProvisionerJobLog[] = []

type BuildLogsAccordionProps = {
  buildId: string
  active: boolean
  autoOpen?: boolean
}

export function BuildLogsAccordion({ buildId, active, autoOpen = true }: BuildLogsAccordionProps) {
  const [open, setOpen] = useLogAccordionOpen(buildId, active, autoOpen)
  const logsQuery = useQuery({
    queryKey: ['build-logs', buildId],
    queryFn: () => getBuildLogs(buildId),
    enabled: Boolean(buildId) && (active || open),
    refetchInterval: active ? 2000 : false,
  })
  const logs = logsQuery.data ?? EMPTY_LOGS
  const errorMessage = logsQuery.isError
    ? logsQuery.error instanceof ApiError ? logsQuery.error.message : 'Failed to load build logs'
    : undefined

  return (
    <LogAccordion
      active={active}
      open={open}
      onOpenChange={setOpen}
      title="Build logs"
      logs={logs}
      isLoading={logsQuery.data === undefined && logsQuery.isFetching}
      errorMessage={errorMessage}
      emptyMessage={active ? 'Waiting for provisioner output…' : 'No logs for this build.'}
      className="mt-4 border-t border-line pt-3"
      getLevel={(entry) => entry.log_level}
      renderPrefix={(entry) => <>{`[${entry.stage ?? 'provision'}]${entry.log_level ? ` ${entry.log_level}` : ''}`}</>}
    />
  )
}
