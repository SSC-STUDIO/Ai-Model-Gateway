import { describe, expect, it } from 'vitest'
import { collapseLabeledValues, describeDonutArc } from './charting'

describe('charting', () => {
  describe('describeDonutArc', () => {
    it('renders a full donut segment as two arcs per ring', () => {
      const path = describeDonutArc(120, 120, 52, 82, -Math.PI / 2, Math.PI * 1.5)

      expect(path).toContain('A 82 82 0 1 1')
      expect(path).toContain('A 52 52 0 1 0')
      expect(path.match(/A /g)?.length).toBe(4)
    })
  })

  describe('collapseLabeledValues', () => {
    it('keeps top values and rolls the tail into other', () => {
      const values = Array.from({ length: 10 }, (_, index) => ({
        label: `item-${index}`,
        value: 10 - index,
        color: `color-${index}`,
      }))

      const result = collapseLabeledValues(values, 8, 'Other', '#94a3b8')

      expect(result).toHaveLength(9)
      expect(result.slice(0, 8).map((item) => item.label)).toEqual(values.slice(0, 8).map((item) => item.label))
      expect(result[8]).toEqual({ label: 'Other', value: 3, color: '#94a3b8' })
    })
  })
})
