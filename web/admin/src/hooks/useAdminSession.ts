import { useCallback, useEffect, useRef, useState } from 'preact/hooks'
import type { AdminSession, AnyRecord } from '../types'
import { invalidateCache } from './useCachedFetch'

function asRecord(value: unknown): AnyRecord | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as AnyRecord : null
}

function asBoolean(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}

function normalizeSession(payload: unknown, defaultEnabled = true): AdminSession {
  const record = asRecord(payload)
  return {
    enabled: asBoolean(record?.enabled) ?? defaultEnabled,
    authenticated: asBoolean(record?.authenticated) ?? false,
    name: asString(record?.name),
    role: asString(record?.role),
  }
}

function extractErrorMessage(payload: unknown): string {
  const record = asRecord(payload)
  const message = asString(record?.error) ?? asString(record?.message)
  if (message && message.trim()) {
    return message.trim()
  }
  if (typeof payload === 'string' && payload.trim()) {
    return payload.trim()
  }
  return 'Request failed'
}

async function readAdminJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  if (!headers.has('Accept')) {
    headers.set('Accept', 'application/json')
  }
  if (init?.body !== undefined && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const response = await fetch(url, {
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
    throw new Error(extractErrorMessage(payload) || `${response.status} ${response.statusText}`)
  }

  return payload as T
}

export interface AdminSessionResult {
  session: AdminSession | null
  loading: boolean
  loginBusy: boolean
  logoutBusy: boolean
  error: string
  clearError: () => void
  refreshSession: () => Promise<AdminSession | null>
  login: (token: string) => Promise<AdminSession | null>
  logout: () => Promise<boolean>
}

export function useAdminSession(): AdminSessionResult {
  const [session, setSession] = useState<AdminSession | null>(null)
  const [loading, setLoading] = useState(true)
  const [loginBusy, setLoginBusy] = useState(false)
  const [logoutBusy, setLogoutBusy] = useState(false)
  const [error, setError] = useState('')
  const refreshPromiseRef = useRef<Promise<AdminSession | null> | null>(null)

  const clearError = useCallback(() => {
    setError('')
  }, [])

  const refreshSession = useCallback(async () => {
    if (refreshPromiseRef.current) {
      return refreshPromiseRef.current
    }

    const pending = (async () => {
      setLoading(true)
      try {
        const next = normalizeSession(await readAdminJSON<unknown>('/api/admin/session'))
        setSession(next)
        setError('')
        if (!next.authenticated) {
          invalidateCache(/\/api\/admin\//)
        }
        return next
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        setError(message)
        return null
      } finally {
        setLoading(false)
        refreshPromiseRef.current = null
      }
    })()

    refreshPromiseRef.current = pending
    return pending
  }, [])

  const login = useCallback(async (token: string) => {
    const trimmed = token.trim()
    if (!trimmed) {
      return null
    }

    setLoginBusy(true)
    setError('')
    try {
      const next = normalizeSession(
        await readAdminJSON<unknown>('/api/admin/login', {
          method: 'POST',
          body: JSON.stringify({ token: trimmed }),
        })
      )
      setSession(next)
      invalidateCache(/\/api\/admin\//)
      return next
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setError(message)
      return null
    } finally {
      setLoginBusy(false)
    }
  }, [])

  const logout = useCallback(async () => {
    setLogoutBusy(true)
    setError('')
    try {
      await readAdminJSON('/api/admin/logout', { method: 'POST' })
      invalidateCache(/\/api\/admin\//)
      setSession((current) => ({
        enabled: current?.enabled ?? true,
        authenticated: false,
      }))
      return true
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setError(message)
      return false
    } finally {
      setLogoutBusy(false)
    }
  }, [])

  useEffect(() => {
    void refreshSession()
  }, [refreshSession])

  return {
    session,
    loading,
    loginBusy,
    logoutBusy,
    error,
    clearError,
    refreshSession,
    login,
    logout,
  }
}
