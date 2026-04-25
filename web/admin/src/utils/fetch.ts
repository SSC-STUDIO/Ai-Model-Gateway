/**
 * Shared JSON fetch utilities for admin API calls.
 */

export interface FetchJSONOptions extends RequestInit {
  onUnauthorized?: () => void
}

export async function fetchJSON<T>(
  input: RequestInfo | URL,
  options: FetchJSONOptions = {}
): Promise<T> {
  const { onUnauthorized, ...init } = options
  const headers = new Headers(init.headers)

  if (!headers.has('Accept')) {
    headers.set('Accept', 'application/json')
  }
  if (init.body !== undefined && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const response = await fetch(input, {
    credentials: 'same-origin',
    ...init,
    headers,
  })

  const text = await response.text()
  let payload: unknown = text
  if (text) {
    try {
      payload = JSON.parse(text) as unknown
    } catch {
      payload = text
    }
  }

  if (!response.ok) {
    if (response.status === 401) {
      onUnauthorized?.()
    }
    const message = extractErrorMessage(payload)
    throw new Error(message || `${response.status} ${response.statusText}`)
  }

  return payload as T
}

function extractErrorMessage(payload: unknown): string {
  if (!payload || typeof payload !== 'object') {
    return typeof payload === 'string' && payload.trim() ? payload.trim() : ''
  }
  const record = payload as Record<string, unknown>
  const message =
    typeof record.error === 'string' ? record.error
    : typeof record.message === 'string' ? record.message
    : typeof record.Error === 'string' ? record.Error
    : typeof record.Message === 'string' ? record.Message
    : ''
  if (message.trim()) {
    return message.trim()
  }
  return ''
}
