import { useEffect, useState } from 'react'

export function useLogAccordionOpen(resetKey: string, active: boolean, autoOpen = true) {
  const [open, setOpen] = useState(active && autoOpen)

  useEffect(() => {
    setOpen(active && autoOpen)
  }, [resetKey, active, autoOpen])

  return [open, setOpen] as const
}
