import { useCallback, useEffect, useRef, useState } from 'preact/hooks'
import type { AdminSession, AnyRecord } from '../types'
import { invalidateCache } from './useCachedFetch'
import { fetchJSON } from '../utils/fetch'

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
        const next = normalizeSession(await fetchJSON<unknown>('/api/admin/session'))
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
        await fetchJSON<unknown>('/api/admin/login', {
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
      await fetchJSON('/api/admin/logout', { method: 'POST' })
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
