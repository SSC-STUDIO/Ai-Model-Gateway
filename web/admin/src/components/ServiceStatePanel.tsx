import type { ComponentChildren } from 'preact'
import type { IconName } from './Icon'
import { Icon } from './Icon'

type ServiceTone = 'success' | 'warning' | 'error' | 'neutral'

interface ServiceStateItem {
  label: string
  value: string
  tone?: ServiceTone
}

interface ServiceStatePanelProps {
  icon: IconName
  title: string
  message: string
  hint?: string
  detail?: string
  items?: ServiceStateItem[]
  actionLabel?: string
  onAction?: () => void
  children?: ComponentChildren
}

export function ServiceStatePanel({
  icon,
  title,
  message,
  hint,
  detail,
  items = [],
  actionLabel,
  onAction,
  children,
}: ServiceStatePanelProps) {
  return (
    <div class="empty-state-box service-state-box">
      <div class="empty-state-icon service-state-icon"><Icon name={icon} size={30} /></div>
      <div class="service-state-copy">
        <p class="empty-state-title">{title}</p>
        <p class="empty-state-hint service-state-message">{message}</p>
        {hint ? <p class="empty-state-hint">{hint}</p> : null}
      </div>

      {items.length > 0 ? (
        <div class="service-state-badges">
          {items.map((item) => (
            <span key={`${item.label}-${item.value}`} class={`status-badge ${item.tone ?? 'neutral'}`}>
              {item.label}: {item.value}
            </span>
          ))}
        </div>
      ) : null}

      {detail ? <code class="service-state-detail">{detail}</code> : null}

      {actionLabel && onAction ? (
        <div class="service-state-actions">
          <button type="button" class="secondary" onClick={onAction}>
            {actionLabel}
          </button>
        </div>
      ) : null}

      {children}
    </div>
  )
}
