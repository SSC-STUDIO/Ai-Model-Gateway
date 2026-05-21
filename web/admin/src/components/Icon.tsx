import type { ComponentChildren } from 'preact'

export type IconName =
  | 'overview'
  | 'telemetry'
  | 'pricing'
  | 'benchmark'
  | 'logs'
  | 'config'
  | 'file'
  | 'edit'
  | 'palette'
  | 'history'
  | 'chart'
  | 'search'
  | 'refresh'
  | 'download'
  | 'copy'
  | 'upload'
  | 'activity'
  | 'server'
  | 'database'
  | 'check'
  | 'clock'
  | 'close'
  | 'key'
  | 'shield'
  | 'eye'
  | 'eyeOff'
  | 'arrowRight'
  | 'sun'
  | 'moon'

interface IconProps {
  name: IconName
  size?: number
  class?: string
  title?: string
}

const icons: Record<IconName, ComponentChildren> = {
  overview: (
    <>
      <path d="M4 19V9" />
      <path d="M10 19V5" />
      <path d="M16 19v-7" />
      <path d="M22 19H2" />
    </>
  ),
  telemetry: (
    <>
      <path d="M3 17l5-5 4 4 8-9" />
      <path d="M15 7h5v5" />
    </>
  ),
  pricing: (
    <>
      <path d="M12 3v18" />
      <path d="M17 7.5c-.8-1.4-2.4-2.2-4.5-2.2-2.8 0-4.5 1.2-4.5 3.1 0 4.2 9 2 9 6.7 0 2-1.8 3.5-4.7 3.5-2.3 0-4-.9-5-2.5" />
    </>
  ),
  benchmark: <path d="M13 2L4 14h7l-1 8 9-12h-7l1-8z" />,
  logs: (
    <>
      <path d="M8 6h13" />
      <path d="M8 12h13" />
      <path d="M8 18h13" />
      <path d="M3 6h.01" />
      <path d="M3 12h.01" />
      <path d="M3 18h.01" />
    </>
  ),
  config: (
    <>
      <path d="M4 21v-7" />
      <path d="M4 10V3" />
      <path d="M12 21v-9" />
      <path d="M12 8V3" />
      <path d="M20 21v-5" />
      <path d="M20 12V3" />
      <path d="M1 14h6" />
      <path d="M9 8h6" />
      <path d="M17 16h6" />
    </>
  ),
  file: (
    <>
      <path d="M6 3h8l4 4v14H6z" />
      <path d="M14 3v5h5" />
    </>
  ),
  edit: (
    <>
      <path d="M4 20h4l11-11a2.1 2.1 0 0 0-3-3L5 17z" />
      <path d="M14 6l4 4" />
    </>
  ),
  palette: (
    <>
      <path d="M12 3a9 9 0 0 0 0 18h1.2a1.8 1.8 0 0 0 1.3-3.05 1.5 1.5 0 0 1 1.05-2.55H17a4 4 0 0 0 0-8h-.8A8.8 8.8 0 0 0 12 3z" />
      <path d="M7.5 10h.01" />
      <path d="M10 7.5h.01" />
      <path d="M14 7.5h.01" />
    </>
  ),
  history: (
    <>
      <path d="M4 5v5h5" />
      <path d="M5.5 14a7 7 0 1 0 1.7-7.2L4 10" />
      <path d="M12 8v5l3 2" />
    </>
  ),
  chart: (
    <>
      <path d="M4 19V5" />
      <path d="M4 19h16" />
      <path d="M7 15l4-4 3 3 5-7" />
    </>
  ),
  search: (
    <>
      <circle cx="11" cy="11" r="7" />
      <path d="M20 20l-4-4" />
    </>
  ),
  refresh: (
    <>
      <path d="M21 12a9 9 0 0 1-15.4 6.4L3 16" />
      <path d="M3 21v-5h5" />
      <path d="M3 12A9 9 0 0 1 18.4 5.6L21 8" />
      <path d="M21 3v5h-5" />
    </>
  ),
  download: (
    <>
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
      <path d="M7 10l5 5 5-5" />
      <path d="M12 15V3" />
    </>
  ),
  copy: (
    <>
      <rect x="9" y="9" width="13" height="13" rx="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </>
  ),
  upload: (
    <>
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
      <path d="M17 8l-5-5-5 5" />
      <path d="M12 3v12" />
    </>
  ),
  activity: (
    <>
      <path d="M22 12h-4l-3 8L9 4l-3 8H2" />
    </>
  ),
  server: (
    <>
      <rect x="3" y="4" width="18" height="6" rx="2" />
      <rect x="3" y="14" width="18" height="6" rx="2" />
      <path d="M7 7h.01" />
      <path d="M7 17h.01" />
    </>
  ),
  database: (
    <>
      <ellipse cx="12" cy="5" rx="8" ry="3" />
      <path d="M4 5v6c0 1.7 3.6 3 8 3s8-1.3 8-3V5" />
      <path d="M4 11v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6" />
    </>
  ),
  check: (
    <>
      <path d="M20 6L9 17l-5-5" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 3" />
    </>
  ),
  close: (
    <>
      <path d="M18 6L6 18" />
      <path d="M6 6l12 12" />
    </>
  ),
  key: (
    <>
      <circle cx="7.5" cy="14.5" r="3.5" />
      <path d="M10 12l8-8" />
      <path d="M15 7l2 2" />
      <path d="M13 9l2 2" />
    </>
  ),
  shield: (
    <>
      <path d="M12 3l7 3v5c0 5-3 8.5-7 10-4-1.5-7-5-7-10V6l7-3z" />
      <path d="M9 12l2 2 4-5" />
    </>
  ),
  eye: (
    <>
      <path d="M2.5 12s3.5-6 9.5-6 9.5 6 9.5 6-3.5 6-9.5 6-9.5-6-9.5-6z" />
      <circle cx="12" cy="12" r="3" />
    </>
  ),
  eyeOff: (
    <>
      <path d="M3 3l18 18" />
      <path d="M10.6 10.6A3 3 0 0 0 13.4 13.4" />
      <path d="M7.6 7.6C4.5 9.4 2.5 12 2.5 12s3.5 6 9.5 6c1.7 0 3.2-.5 4.5-1.2" />
      <path d="M14.1 6.3A9.8 9.8 0 0 0 12 6c-6 0-9.5 6-9.5 6a17 17 0 0 0 3 3.4" />
      <path d="M18.4 15.4A17 17 0 0 0 21.5 12s-2-3.4-5.7-5.1" />
    </>
  ),
  arrowRight: (
    <>
      <path d="M5 12h14" />
      <path d="M13 6l6 6-6 6" />
    </>
  ),
  sun: (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2" />
      <path d="M12 20v2" />
      <path d="M4.93 4.93l1.41 1.41" />
      <path d="M17.66 17.66l1.41 1.41" />
      <path d="M2 12h2" />
      <path d="M20 12h2" />
      <path d="M4.93 19.07l1.41-1.41" />
      <path d="M17.66 6.34l1.41-1.41" />
    </>
  ),
  moon: <path d="M20 14.3A8 8 0 0 1 9.7 4 7 7 0 1 0 20 14.3z" />,
}

export function Icon({ name, size = 18, class: className, title }: IconProps) {
  return (
    <svg
      class={['ui-icon', className].filter(Boolean).join(' ')}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      role={title ? 'img' : undefined}
      aria-hidden={title ? undefined : 'true'}
      focusable="false"
    >
      {title && <title>{title}</title>}
      {icons[name]}
    </svg>
  )
}
