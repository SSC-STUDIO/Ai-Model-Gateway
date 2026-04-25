import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/preact'
import { useFlashValue } from './useFlashValue'

describe('useFlashValue', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns false initially', () => {
    const { result } = renderHook(() => useFlashValue('initial'))
    expect(result.current).toBe(false)
  })

  it('returns true when value changes', () => {
    const { result, rerender } = renderHook(
      ({ value }) => useFlashValue(value),
      { initialProps: { value: 'a' } }
    )

    rerender({ value: 'b' })
    expect(result.current).toBe(true)
  })

  it('returns false after delay', () => {
    const { result, rerender } = renderHook(
      ({ value }) => useFlashValue(value, 100),
      { initialProps: { value: 'a' } }
    )

    rerender({ value: 'b' })
    expect(result.current).toBe(true)

    act(() => {
      vi.advanceTimersByTime(100)
    })
    expect(result.current).toBe(false)
  })

  it('does not flash when value stays the same', () => {
    const { result, rerender } = renderHook(
      ({ value }) => useFlashValue(value),
      { initialProps: { value: 'a' } }
    )

    rerender({ value: 'a' })
    expect(result.current).toBe(false)
  })

  it('extends flash when value changes rapidly', () => {
    const { result, rerender } = renderHook(
      ({ value }) => useFlashValue(value, 100),
      { initialProps: { value: 'a' } }
    )

    rerender({ value: 'b' })
    expect(result.current).toBe(true)

    act(() => {
      vi.advanceTimersByTime(50)
    })

    rerender({ value: 'c' })
    expect(result.current).toBe(true)

    act(() => {
      vi.advanceTimersByTime(100)
    })
    expect(result.current).toBe(false)
  })
})
