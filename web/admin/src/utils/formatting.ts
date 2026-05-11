import type { AnyRecord } from '../types'

export function pretty(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

export function formatUsd(value: number): string {
  if (value < 0.01 && value > 0) return '<$0.01'
  if (value >= 1000) return `$${(value / 1000).toFixed(2)}K`
  return `$${value.toFixed(2)}`
}

export function versionIdOf(item: unknown): string {
  if (!item || typeof item !== 'object') return ''
  const record = item as AnyRecord
  const raw = record.version_id ?? record.versionId ?? record.id
  return typeof raw === 'string' ? raw : ''
}

export function getInputValue(e: Event): string {
  return (e.currentTarget as HTMLInputElement | HTMLTextAreaElement).value
}

export function getSelectValue(e: Event): string {
  return (e.currentTarget as HTMLSelectElement).value
}

export function getChecked(e: Event): boolean {
  return (e.currentTarget as HTMLInputElement).checked
}

export function formatInteger(value: number | null | undefined): string {
  return typeof value === 'number' && Number.isFinite(value) ? value.toLocaleString() : '-'
}

export function formatAbsoluteTime(value: string | null | undefined): string {
  if (!value) return '-'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString()
}

export function formatRelativeTime(value: string | null | undefined): string {
  if (!value) return '-'
  const parsed = new Date(value)
  const ts = parsed.getTime()
  if (Number.isNaN(ts)) return value

  const now = Date.now()
  const diffMs = now - ts
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)

  if (diffSec < 10) return 'just now'
  if (diffSec < 60) return `${diffSec}s ago`
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHour < 24) return `${diffHour}h ago`
  if (diffDay < 7) return `${diffDay}d ago`
  return parsed.toLocaleDateString()
}

export function formatStatusTime(value: string | null | undefined): string {
  if (!value) return '-'
  const parsed = Date.parse(value)
  if (!Number.isFinite(parsed)) return value
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(parsed)
}
