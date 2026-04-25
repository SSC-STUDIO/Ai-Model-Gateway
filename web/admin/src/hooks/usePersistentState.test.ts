import { describe, it, expect, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/preact'
import { usePersistentState } from './usePersistentState'

describe('usePersistentState', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('uses initial value when localStorage is empty', () => {
    const { result } = renderHook(() => usePersistentState('test-key', 'default'))
    expect(result.current[0]).toBe('default')
  })

  it('reads existing value from localStorage', () => {
    localStorage.setItem('test-key', JSON.stringify('stored'))
    const { result } = renderHook(() => usePersistentState('test-key', 'default'))
    expect(result.current[0]).toBe('stored')
  })

  it('persists value to localStorage on change', () => {
    const { result } = renderHook(() => usePersistentState('test-key', 'default'))
    act(() => {
      result.current[1]('updated')
    })
    expect(result.current[0]).toBe('updated')
    expect(localStorage.getItem('test-key')).toBe(JSON.stringify('updated'))
  })

  it('supports functional update', () => {
    const { result } = renderHook(() => usePersistentState<number>('count', 0))
    act(() => {
      result.current[1]((prev) => prev + 1)
    })
    expect(result.current[0]).toBe(1)
    expect(localStorage.getItem('count')).toBe(JSON.stringify(1))
  })

  it('falls back to initial value on corrupt localStorage', () => {
    localStorage.setItem('bad-key', 'not-json')
    const { result } = renderHook(() => usePersistentState('bad-key', 'fallback'))
    expect(result.current[0]).toBe('fallback')
  })

  it('uses custom serializer when provided', () => {
    const serializer = {
      stringify: (v: number) => String(v * 2),
      parse: (v: string) => Number(v) / 2,
    }
    const { result } = renderHook(() => usePersistentState('custom-key', 10, serializer))
    act(() => {
      result.current[1](20)
    })
    expect(result.current[0]).toBe(20)
    expect(localStorage.getItem('custom-key')).toBe('40')
  })
})
