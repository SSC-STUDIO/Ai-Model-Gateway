import { describe, it, expect, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/preact'
import { useUrlState } from './useUrlState'

describe('useUrlState', () => {
  beforeEach(() => {
    // Reset URL to root
    window.history.replaceState({}, '', '/')
  })

  it('reads initial value from URL param', () => {
    window.history.replaceState({}, '', '/?hours=48')
    const { result } = renderHook(() => useUrlState('hours', '24'))
    expect(result.current[0]).toBe('48')
  })

  it('uses default when param is absent', () => {
    const { result } = renderHook(() => useUrlState('hours', '24'))
    expect(result.current[0]).toBe('24')
  })

  it('writes string value to URL', () => {
    const { result } = renderHook(() => useUrlState<string>('hours', '24'))
    act(() => {
      result.current[1]('48')
    })
    expect(window.location.search).toContain('hours=48')
    expect(result.current[0]).toBe('48')
  })

  it('writes number value to URL', () => {
    const { result } = renderHook(() => useUrlState<number>('count', 0))
    act(() => {
      result.current[1](42)
    })
    expect(window.location.search).toContain('count=42')
    expect(result.current[0]).toBe(42)
  })

  it('writes array value to URL as comma-separated', () => {
    const { result } = renderHook(() => useUrlState('models', [] as string[]))
    act(() => {
      result.current[1](['gpt-4o', 'gpt-4o-mini'])
    })
    expect(window.location.search).toContain('models=gpt-4o%2Cgpt-4o-mini')
    expect(result.current[0]).toEqual(['gpt-4o', 'gpt-4o-mini'])
  })

  it('removes default value from URL', () => {
    window.history.replaceState({}, '', '/?hours=24')
    const { result } = renderHook(() => useUrlState('hours', '24'))
    act(() => {
      result.current[1]('24')
    })
    expect(window.location.search).not.toContain('hours')
  })

  it('supports functional update', () => {
    const { result } = renderHook(() => useUrlState<number>('count', 0))
    act(() => {
      result.current[1]((prev) => prev + 1)
    })
    expect(result.current[0]).toBe(1)
    expect(window.location.search).toContain('count=1')
  })

  it('syncs on popstate', () => {
    const { result } = renderHook(() => useUrlState<string>('hours', '24'))
    act(() => {
      result.current[1]('48')
    })
    expect(result.current[0]).toBe('48')

    // Simulate back button
    window.history.replaceState({}, '', '/?hours=12')
    act(() => {
      window.dispatchEvent(new PopStateEvent('popstate'))
    })
    expect(result.current[0]).toBe('12')
  })
})
