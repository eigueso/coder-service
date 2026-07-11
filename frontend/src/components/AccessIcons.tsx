type IconProps = {
  className?: string
}

export function TerminalIcon({ className }: IconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
    >
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M7 9l3 3-3 3" />
      <path d="M12 15h5" />
    </svg>
  )
}

export function VSCodeBrowserIcon({ className }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden="true">
      <path
        fill="currentColor"
        d="M17.6 3.2 9.3 8.1 4.8 5.5l.8 3.1-2.6 1.6v3.6l2.6 1.6-.8 3.1 4.5-2.6 8.3 4.9c.5.3 1.2 0 1.2-.6V3.8c0-.6-.7-1-.1.2-1.2zM10 14.1l-2.4 1.4.4-1.7L6 12.7v-.9l2-.9-.4-1.7L10 10.6v3.5zm1.2-5.2 5.6-3.3v11.5l-5.6-3.3V8.9z"
      />
    </svg>
  )
}

export function VSCodeDesktopIcon({ className }: IconProps) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden="true">
      <path
        fill="currentColor"
        d="M3 5.5A2.5 2.5 0 0 1 5.5 3h13A2.5 2.5 0 0 1 21 5.5v9a2.5 2.5 0 0 1-2.5 2.5H13v2h3v1.5H8V19h3v-2H5.5A2.5 2.5 0 0 1 3 14.5v-9zm2.5-.5a.5.5 0 0 0-.5.5v9c0 .28.22.5.5.5h13a.5.5 0 0 0 .5-.5v-9a.5.5 0 0 0-.5-.5h-13z"
      />
      <path
        fill="currentColor"
        d="M14.8 8.2 11 10.4v3.2l3.8 2.2 3.4-2V10.2l-3.4-2zm.2 1.7 1.6.95v1.9L15 13.7l-1.6-.95v-1.9L15 9.9z"
      />
    </svg>
  )
}
