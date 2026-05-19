import { describe, expect, it } from 'vitest'
import { generatePageNumbers } from './pagination'

describe('generatePageNumbers', () => {
  it('returns every page for short ranges', () => {
    expect(generatePageNumbers(1, 1)).toEqual([1])
    expect(generatePageNumbers(3, 7)).toEqual([1, 2, 3, 4, 5, 6, 7])
  })

  it('adds an ending gap near the beginning', () => {
    expect(generatePageNumbers(2, 10)).toEqual([1, 2, 3, '...', 10])
  })

  it('adds both gaps around the middle', () => {
    expect(generatePageNumbers(5, 10)).toEqual([1, '...', 4, 5, 6, '...', 10])
  })

  it('adds a leading gap near the end', () => {
    expect(generatePageNumbers(9, 10)).toEqual([1, '...', 8, 9, 10])
  })
})
