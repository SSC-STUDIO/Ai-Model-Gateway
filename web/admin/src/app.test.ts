import { describe, expect, it } from 'vitest'
import { canonicalAdminHref, inferTab } from './app'

describe('admin route compatibility', () => {
  it('maps legacy monitoring routes to the unified monitoring workspace', () => {
    expect(inferTab('/admin/telemetry')).toBe('monitoring')
    expect(inferTab('/admin/pricing')).toBe('monitoring')
    expect(canonicalAdminHref('/admin/telemetry?hours=24')).toBe('/admin/monitoring?hours=24&view=telemetry')
    expect(canonicalAdminHref('/admin/pricing?hours=168')).toBe('/admin/monitoring?hours=168&view=pricing')
  })

  it('maps legacy ops routes to the unified ops workspace', () => {
    expect(inferTab('/admin/audit')).toBe('ops')
    expect(inferTab('/admin/probe')).toBe('ops')
    expect(inferTab('/admin/diagnostics')).toBe('ops')
    expect(canonicalAdminHref('/admin/audit')).toBe('/admin/ops?view=audit')
    expect(canonicalAdminHref('/admin/probe')).toBe('/admin/ops?view=probe')
    expect(canonicalAdminHref('/admin/diagnostics')).toBe('/admin/ops?view=diagnostics')
    expect(canonicalAdminHref('/admin/ops?view=updates')).toBe('/admin/ops?view=updates')
  })

  it('keeps canonical primary routes unchanged', () => {
    expect(inferTab('/admin/logs')).toBe('logs')
    expect(canonicalAdminHref('/admin/monitoring?view=pricing&bucket=5')).toBe('/admin/monitoring?view=pricing&bucket=5')
  })
})
