import type { ComponentChildren } from 'preact'
import { Icon, type IconName } from './Icon'

export interface WorkspaceBandProps {
  id: string
  icon: IconName
  kicker: string
  title: string
  detail: string
  children: ComponentChildren
}

export function WorkspaceBand({ id, icon, kicker, title, detail, children }: WorkspaceBandProps) {
  return (
    <section id={id} class="workspace-band">
      <div class="workspace-band-header">
        <div class="workspace-band-copy">
          <span class="workspace-kicker">
            <Icon name={icon} class="workspace-kicker-icon" />
            {kicker}
          </span>
          <h3 class="workspace-band-title">{title}</h3>
          <p class="workspace-band-detail">{detail}</p>
        </div>
      </div>
      {children}
    </section>
  )
}
