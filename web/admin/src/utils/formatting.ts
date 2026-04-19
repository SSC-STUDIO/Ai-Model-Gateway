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
