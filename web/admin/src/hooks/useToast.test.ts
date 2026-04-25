import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/preact'
import { useToast } from './useToast'

describe('useToast', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('starts with empty toasts', () => {
    const { result } = renderHook(() => useToast())
    expect(result.current.toasts).toEqual([])
  })

  it('adds a toast', () => {
    const { result } = renderHook(() => useToast())
    act(() => {
      result.current.addToast('hello', 'info')
    })
    expect(result.current.toasts.length).toBe(1)
    expect(result.current.toasts[0].message).toBe('hello')
    expect(result.current.toasts[0].type).toBe('info')
  })

  it('removes a toast by id', () => {
    const { result } = renderHook(() => useToast())
    let id = ''
    act(() => {
      id = result.current.addToast('test', 'success')
    })
    expect(result.current.toasts.length).toBe(1)

    act(() => {
      result.current.removeToast(id)
    })
    expect(result.current.toasts.length).toBe(0)
  })

  it('auto-removes toast after duration', () => {
    const { result } = renderHook(() => useToast())
    act(() => {
      result.current.addToast('auto', 'warning', 100)
    })
    expect(result.current.toasts.length).toBe(1)

    act(() => {
      vi.advanceTimersByTime(100)
    })
    expect(result.current.toasts.length).toBe(0)
  })

  it('clears timer when toast is manually removed', () => {
    const { result } = renderHook(() => useToast())
    let id = ''
    act(() => {
      id = result.current.addToast('manual', 'error', 5000)
    })

    act(() => {
      result.current.removeToast(id)
    })
    expect(result.current.toasts.length).toBe(0)

    // Ensure timer does not fire later
    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(result.current.toasts.length).toBe(0)
  })

  it('supports different toast types', () => {
    const { result } = renderHook(() => useToast())
    act(() => {
      result.current.addToast('a', 'success')
      result.current.addToast('b', 'error')
      result.current.addToast('c', 'warning')
      result.current.addToast('d', 'info')
    })
    const types = result.current.toasts.map((t) => t.type)
    expect(types).toEqual(['success', 'error', 'warning', 'info'])
  })
})
