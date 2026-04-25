import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/preact'
import { usePageVisibility } from './usePageVisibility'

describe('usePageVisibility', () => {
  let originalHidden: PropertyDescriptor | undefined

  beforeEach(() => {
    originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
  })

  afterEach(() => {
    if (originalHidden) {
      Object.defineProperty(document, 'hidden', originalHidden)
    }
  })

  it('returns true when document is visible', () => {
    Object.defineProperty(document, 'hidden', { value: false, writable: true })
    const { result } = renderHook(() => usePageVisibility())
    expect(result.current).toBe(true)
  })

  it('returns false when document is hidden', () => {
    Object.defineProperty(document, 'hidden', { value: true, writable: true })
    const { result } = renderHook(() => usePageVisibility())
    expect(result.current).toBe(false)
  })

  it('updates on visibilitychange event', () => {
    Object.defineProperty(document, 'hidden', { value: false, writable: true })
    const { result } = renderHook(() => usePageVisibility())
    expect(result.current).toBe(true)

    Object.defineProperty(document, 'hidden', { value: true, writable: true })
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(result.current).toBe(false)

    Object.defineProperty(document, 'hidden', { value: false, writable: true })
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(result.current).toBe(true)
  })
})
