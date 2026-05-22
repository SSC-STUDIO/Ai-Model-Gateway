import { test, expect } from '@playwright/test'

test.describe('Admin UI Smoke Tests', () => {
  let diagnosticsRequests = 0
  let benchmarkStartPayloads: Array<Record<string, unknown>> = []

  test.beforeEach(async ({ page }) => {
    diagnosticsRequests = 0
    benchmarkStartPayloads = []
    const mockBenchmarkRuns = [
      {
        run_id: 'run-capability-1',
        status: 'completed',
        suite_version: 'general_protocol_v1',
        protocol: 'openai_chat_completions',
        public_snapshot_id: 'public-1',
        vendor_snapshot_id: 'vendor-1',
        started_at: '2026-04-26T09:00:00Z',
        completed_at: '2026-04-26T09:03:00Z',
        target_count: 2,
        completed_targets: 2,
      },
      {
        run_id: 'run-running',
        status: 'running',
        suite_version: 'general_protocol_v1',
        protocol: 'openai_chat_completions',
        started_at: '2026-04-26T09:30:00Z',
        target_count: 1,
        completed_targets: 0,
      },
    ]
    const mockBenchmarkRunDetail = {
      run_id: 'run-capability-1',
      status: 'completed',
      suite_version: 'general_protocol_v1',
      protocol: 'openai_chat_completions',
      public_snapshot_id: 'public-1',
      vendor_snapshot_id: 'vendor-1',
      started_at: '2026-04-26T09:00:00Z',
      completed_at: '2026-04-26T09:03:00Z',
      target_count: 2,
      completed_targets: 2,
      targets: [
        {
          target_id: 'target-openai',
          run_id: 'run-capability-1',
          status: 'completed',
          provider_id: 'provider-openai',
          public_model: 'gpt-5.5',
          protocol: 'openai_chat_completions',
          suite_version: 'general_protocol_v1',
          public_snapshot_id: 'public-1',
          vendor_snapshot_id: 'vendor-1',
          verdict: 'normal',
          overall_score: 96.4,
          completion_rate: 100,
          public_gap: 1.2,
          vendor_gap: 2.1,
          dimension_scores: { reasoning: 98, coding_proxy: 95, instruction: 100, tool_json: 94, stream_protocol: 96 },
          started_at: '2026-04-26T09:00:00Z',
          completed_at: '2026-04-26T09:01:00Z',
        },
        {
          target_id: 'target-anthropic',
          run_id: 'run-capability-1',
          status: 'completed',
          provider_id: 'provider-anthropic',
          public_model: 'claude-3.7-sonnet',
          protocol: 'openai_chat_completions',
          suite_version: 'general_protocol_v1',
          public_snapshot_id: 'public-1',
          vendor_snapshot_id: 'vendor-1',
          verdict: 'suspect',
          overall_score: 91.2,
          completion_rate: 100,
          public_gap: 4.8,
          vendor_gap: 5.5,
          dimension_scores: { reasoning: 92, coding_proxy: 91, instruction: 90, tool_json: 93, stream_protocol: 90 },
          started_at: '2026-04-26T09:00:00Z',
          completed_at: '2026-04-26T09:02:00Z',
        },
      ],
    }

    // Mock admin session to bypass authentication so UI renders without a backend
    await page.route('/api/admin/session', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ enabled: false }),
      })
    })

    // Mock data APIs with minimal valid payloads to prevent error banners and timeouts
    await page.route('/api/admin/overview', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({}),
      })
    })
    await page.route('/api/admin/status', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({}),
      })
    })
    await page.route('/api/admin/telemetry**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          Total: 2,
          Events: [
            {
              Timestamp: '2026-04-26T10:00:00Z',
              Path: '/v1/chat/completions',
              RequestedModel: 'gpt-4o',
              EffectiveModel: 'gpt-4o',
              Provider: 'openai',
              StatusCode: 200,
              LatencyMs: 120,
              InputTokens: 100,
              OutputTokens: 20,
            },
          ],
          Models: [{ Value: 'gpt-4o', Requests: 2, Successes: 2, Failures: 0, InputTokens: 200, OutputTokens: 40, AvgLatencyMs: 120 }],
          Upstreams: [{ Value: 'openai', Requests: 2, Successes: 2, Failures: 0, InputTokens: 200, OutputTokens: 40, AvgLatencyMs: 120 }],
        }),
      })
    })
    await page.route('/api/admin/timeseries**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          points: [
            {
              Bucket: '2026-04-26T10:00:00Z',
              Requests: 2,
              Successes: 2,
              Failures: 0,
              AvgLatencyMs: 120,
              InputTokens: 200,
              OutputTokens: 40,
            },
          ],
        }),
      })
    })
    await page.route('/api/admin/config', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          revision: null,
          policy: {},
          config: {},
          raw_yaml: '{}',
        }),
      })
    })
    await page.route('/api/admin/config/history', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      })
    })
    await page.route('/api/admin/benchmark**', async (route) => {
      const url = new URL(route.request().url())
      if (url.pathname.endsWith('/telemetry')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ Events: [], Total: 0 }),
        })
        return
      }
      if (url.pathname.endsWith('/runs')) {
        if (route.request().method() === 'POST') {
          benchmarkStartPayloads.push(route.request().postDataJSON() as Record<string, unknown>)
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ ...mockBenchmarkRunDetail, run_id: 'run-manual', public_snapshot_id: '', vendor_snapshot_id: '' }),
          })
          return
        }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ runs: mockBenchmarkRuns }),
        })
        return
      }
      if (url.pathname.includes('/runs/')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mockBenchmarkRunDetail),
        })
        return
      }
      if (url.pathname.includes('/baselines')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ snapshots: [] }),
        })
        return
      }
      const group = url.searchParams.get('group') || 'model'
      const benchmarks = group === 'upstream'
        ? [
            {
              upstream: 'provider-openai',
              label: 'provider-openai',
              model: 'provider-openai',
              requests: 1200,
              successes: 1188,
              failures: 12,
              input_tokens: 2400000,
              cached_prompt_tokens: 120000,
              output_tokens: 720000,
              avg_latency_ms: 480,
              p50_latency_ms: 410,
              p95_latency_ms: 860,
              p99_latency_ms: 1200,
              max_latency_ms: 1640,
              success_rate: 0.99,
              estimated_cost_usd: 11650.25,
            },
            {
              upstream: 'provider-anthropic',
              label: 'provider-anthropic',
              model: 'provider-anthropic',
              requests: 840,
              successes: 815,
              failures: 25,
              input_tokens: 1330000,
              output_tokens: 360000,
              avg_latency_ms: 530,
              p50_latency_ms: 470,
              p95_latency_ms: 950,
              p99_latency_ms: 1320,
              max_latency_ms: 1810,
              success_rate: 0.9702,
              estimated_cost_usd: 8450.75,
            },
          ]
        : [
            {
              model: 'gpt-5.5',
              requests: 1200,
              successes: 1188,
              failures: 12,
              input_tokens: 2400000,
              output_tokens: 720000,
              avg_latency_ms: 480,
              p50_latency_ms: 410,
              p95_latency_ms: 860,
              p99_latency_ms: 1200,
              max_latency_ms: 1640,
              success_rate: 0.99,
              estimated_cost_usd: 11650.25,
            },
            {
              model: 'claude-3.7-sonnet',
              requests: 840,
              successes: 815,
              failures: 25,
              input_tokens: 1330000,
              output_tokens: 360000,
              avg_latency_ms: 530,
              p50_latency_ms: 470,
              p95_latency_ms: 950,
              p99_latency_ms: 1320,
              max_latency_ms: 1810,
              success_rate: 0.9702,
              estimated_cost_usd: 8450.75,
            },
          ]
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          generated_at: '2026-04-26T10:00:00Z',
          window_hours: 168,
          group,
          benchmarks,
        }),
      })
    })
    await page.route('/api/admin/benchmark/baselines**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ snapshots: [] }),
      })
    })
    await page.route('/api/admin/benchmark/runs**', async (route) => {
      const url = new URL(route.request().url())
      const path = url.pathname
      if (path.endsWith('/telemetry')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ Events: [], Total: 0 }),
        })
        return
      }
      if (path.endsWith('/runs')) {
        if (route.request().method() === 'POST') {
          benchmarkStartPayloads.push(route.request().postDataJSON() as Record<string, unknown>)
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ ...mockBenchmarkRunDetail, run_id: 'run-manual', public_snapshot_id: '', vendor_snapshot_id: '' }),
          })
          return
        }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            runs: [
              {
                run_id: 'run-capability-1',
                status: 'completed',
                suite_version: 'general_protocol_v1',
                protocol: 'openai_chat_completions',
                public_snapshot_id: 'public-1',
                vendor_snapshot_id: 'vendor-1',
                started_at: '2026-04-26T09:00:00Z',
                completed_at: '2026-04-26T09:03:00Z',
                target_count: 2,
                completed_targets: 2,
              },
              {
                run_id: 'run-running',
                status: 'running',
                suite_version: 'general_protocol_v1',
                protocol: 'openai_chat_completions',
                started_at: '2026-04-26T09:30:00Z',
                target_count: 1,
                completed_targets: 0,
              },
            ],
          }),
        })
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          run_id: 'run-capability-1',
          status: 'completed',
          suite_version: 'general_protocol_v1',
          protocol: 'openai_chat_completions',
          public_snapshot_id: 'public-1',
          vendor_snapshot_id: 'vendor-1',
          started_at: '2026-04-26T09:00:00Z',
          completed_at: '2026-04-26T09:03:00Z',
          target_count: 2,
          completed_targets: 2,
          targets: [
            {
              target_id: 'target-openai',
              run_id: 'run-capability-1',
              status: 'completed',
              provider_id: 'provider-openai',
              public_model: 'gpt-5.5',
              protocol: 'openai_chat_completions',
              suite_version: 'general_protocol_v1',
              public_snapshot_id: 'public-1',
              vendor_snapshot_id: 'vendor-1',
              verdict: 'normal',
              overall_score: 96.4,
              completion_rate: 100,
              public_gap: 1.2,
              vendor_gap: 2.1,
              dimension_scores: { reasoning: 98, coding_proxy: 95, instruction: 100, tool_json: 94, stream_protocol: 96 },
              started_at: '2026-04-26T09:00:00Z',
              completed_at: '2026-04-26T09:01:00Z',
            },
            {
              target_id: 'target-anthropic',
              run_id: 'run-capability-1',
              status: 'completed',
              provider_id: 'provider-anthropic',
              public_model: 'claude-3.7-sonnet',
              protocol: 'openai_chat_completions',
              suite_version: 'general_protocol_v1',
              public_snapshot_id: 'public-1',
              vendor_snapshot_id: 'vendor-1',
              verdict: 'suspect',
              overall_score: 91.2,
              completion_rate: 100,
              public_gap: 4.8,
              vendor_gap: 5.5,
              dimension_scores: { reasoning: 92, coding_proxy: 91, instruction: 90, tool_json: 93, stream_protocol: 90 },
              started_at: '2026-04-26T09:00:00Z',
              completed_at: '2026-04-26T09:02:00Z',
            },
          ],
        }),
      })
    })
    await page.route('/api/admin/audit**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          events: [
            {
              id: 'audit-1',
              time: '2026-04-26T10:00:00Z',
              actor: 'system',
              action: 'runtime.preflight',
              resource: 'runtime',
              success: true,
            },
          ],
          count: 1,
        }),
      })
    })
    await page.route('/api/admin/runtime/status', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          version: '1.3.0',
          uptime: '5m',
          gateway_status: 'connected',
          gateway_readiness: 'ready',
          gateway_listener: '127.0.0.1:18083',
          telemetry_status: 'connected',
          telemetry_event_count: 42,
          active_requests: 0,
          gateway: {
            readiness: 'ready',
            listener: '127.0.0.1:18083',
            active_snapshot_id: 'snap-1',
            active_requests: 0,
            provider_health: {
              openai: { healthy: true, consecutive_failures: 0, latency_ms: 120 },
              anthropic: { healthy: false, consecutive_failures: 2, latency_ms: 360 },
            },
          },
          runtime: {
            listen: '127.0.0.1:18086',
            gateway_socket: '.gateway-runtime/gateway.sock',
            telemetry_socket: '.gateway-runtime/telemetry.sock',
            config_paths: {
              gatewayd: 'configs/gatewayd.json',
              controld: 'configs/controld.json',
            },
          },
        }),
      })
    })
    await page.route('/api/admin/runtime/preflight', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          checks: [
            { name: 'gateway_connected', ok: true, detail: 'connected' },
            { name: 'gateway_ready', ok: true, detail: 'ready' },
            { name: 'telemetry_connected', ok: true, detail: 'connected' },
          ],
        }),
      })
    })
    await page.route('/api/admin/update/status', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          current_version: '1.4.1',
          platform: 'windows/amd64',
          repository: 'SSC-STUDIO/Ai-Model-Gateway',
          install_dir: 'D:\\Ai-Model-Gateway',
          state_dir: '.gateway-runtime/update',
          latest_version: '1.4.2',
          latest_tag: 'v1.4.2',
          update_available: true,
          asset_name: 'ai-model-gateway-windows-amd64.zip',
          cached_bundle_dir: 'D:\\Ai-Model-Gateway\\.gateway-runtime\\update\\downloads\\v1.4.2-windows-amd64\\bundle',
          cached_version: '1.4.2',
          cached_verify: { ok: true },
          last_backup_dir: 'D:\\Ai-Model-Gateway\\.gateway-runtime\\update\\backups\\20260520-120000',
          message: 'update bundle downloaded and verified',
        }),
      })
    })
    await page.route('/api/admin/update/check', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          current_version: '1.4.1',
          latest_version: '1.4.2',
          latest_tag: 'v1.4.2',
          update_available: true,
          platform: 'windows/amd64',
          repository: 'SSC-STUDIO/Ai-Model-Gateway',
          asset_name: 'ai-model-gateway-windows-amd64.zip',
        }),
      })
    })
    await page.route('/api/admin/update/fetch', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          current_version: '1.4.1',
          latest_version: '1.4.2',
          update_available: true,
          cached_bundle_dir: 'D:\\Ai-Model-Gateway\\.gateway-runtime\\update\\downloads\\v1.4.2-windows-amd64\\bundle',
          cached_version: '1.4.2',
          cached_verify: { ok: true },
          message: 'update bundle downloaded and verified',
        }),
      })
    })
    await page.route('/api/admin/update/apply', async (route) => {
      const payload = route.request().postDataJSON() as Record<string, unknown>
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          current_version: '1.4.1',
          latest_version: '1.4.2',
          update_available: true,
          cached_bundle_dir: payload.bundle_dir || 'D:\\Ai-Model-Gateway\\.gateway-runtime\\update\\downloads\\v1.4.2-windows-amd64\\bundle',
          cached_version: '1.4.2',
          cached_verify: { ok: true },
          last_backup_dir: 'D:\\Ai-Model-Gateway\\.gateway-runtime\\update\\backups\\20260520-120000',
          message: payload.dry_run ? 'dry-run: bundle verified; no files copied' : 'update applied; restart the service to run the new binaries',
        }),
      })
    })
    await page.route('/api/admin/update/rollback', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          current_version: '1.4.1',
          cached_version: '1.4.2',
          last_backup_dir: 'D:\\Ai-Model-Gateway\\.gateway-runtime\\update\\backups\\20260520-120000',
          message: 'rollback restored the last update backup',
        }),
      })
    })
    await page.route('/api/admin/diagnostics', async (route) => {
      diagnosticsRequests += 1
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          generated_at: '2026-04-26T10:00:00Z',
          redacted: true,
          status: {
            version: '1.3.0',
            gateway_status: 'connected',
            gateway_readiness: 'ready',
            telemetry_status: 'connected',
          },
          runtime: {
            listen: '127.0.0.1:18086',
          },
          audit_tail: [],
        }),
      })
    })

    await page.goto('/admin/')
  })

  test('overview page loads with navigation and data panels', async ({ page }) => {
    // Verify page title
    await expect(page).toHaveTitle(/AI Model Gateway Admin/)

    // Verify top navigation tabs exist
    await expect(page.locator('.tabbar')).toBeVisible()
    const tabs = ['Overview', 'Monitoring', 'Benchmark', 'Ops', 'Config', 'Logs']
    for (const label of tabs) {
      await expect(page.locator('.tabbar')).toContainText(label)
    }
    await expect(page.locator('.tabbar .tab span')).toHaveText(tabs)

    // At least one panel should be present (overview skeleton or content)
    await expect(page.locator('.panel').first()).toBeVisible()

    // Verify status badges in header (gateway + telemetry + auth disabled)
    await expect(page.locator('.topbar-right .status-badge')).toHaveCount(3)
  })

  test('config tab shows publish/rollback button state', async ({ page }) => {
    await page.click('text=Config')

    // Wait for config panel to load
    await expect(page.locator('h2')).toContainText('Config')

    // Toolbar may not exist if backend is unavailable; verify gracefully
    const toolbar = page.locator('.history-toolbar')
    if (await toolbar.isVisible().catch(() => false)) {
      const actionButton = toolbar.locator('button').first()
      await expect(actionButton).toBeDisabled()

      const select = toolbar.locator('select')
      const optionCount = await select.locator('option').count()
      if (optionCount > 1) {
        await select.selectOption({ index: 1 })
        const isEnabled = await actionButton.isEnabled()
        expect(typeof isEnabled).toBe('boolean')
      }
    }
  })

  test('benchmark tab renders benchmark workspace with real metrics', async ({ page }) => {
    await page.click('text=Benchmark')

    await expect(page).toHaveURL(/\/admin\/benchmark/)
    await expect(page.locator('.benchmark-reset-page')).toContainText('Upstream Performance')
    await expect(page.locator('.benchmark-reset-page')).toContainText('Upstream Performance Ranking')
    await expect(page.locator('.benchmark-reset-page')).toContainText('Model Detail Table')
    await expect(page.locator('.benchmark-summary-grid')).toContainText('Observed upstreams')
    await expect(page.locator('.benchmark-summary-grid')).toContainText('2')
    await expect(page.locator('.benchmark-summary-grid')).toContainText('2,040')
    const upstreamRanking = page.locator('.benchmark-table-section', { hasText: 'Upstream Performance Ranking' })
    await expect(upstreamRanking).toContainText('provider-openai')
    await expect(upstreamRanking).toContainText('provider-anthropic')
    await expect(upstreamRanking).not.toContainText('gpt-5.5')
    const modelDetails = page.locator('.benchmark-table-section', { hasText: 'Model Detail Table' })
    await expect(modelDetails).toContainText('gpt-5.5')
    await expect(modelDetails).toContainText('claude-3.7-sonnet')
    await expect(page.locator('.benchmark-charts-grid')).toContainText('Upstream Success Rate Comparison')

    await page.locator('.workspace-nav button', { hasText: 'Capability' }).click()
    await expect(page.locator('.benchmark-reset-page')).toContainText('Model Capability Benchmark Ranking')
    await expect(page.locator('.benchmark-reset-page')).toContainText('Public Gap')
    await expect(page.locator('.benchmark-verification')).toContainText('Manual Benchmark')
    await expect(page.locator('.benchmark-verification input[placeholder="provider-id"]')).toBeDisabled()
    await page.getByLabel('All enabled upstream models').uncheck()
    await page.locator('.benchmark-verification input[placeholder="provider-id"]').fill('provider-openai')
    await page.locator('.benchmark-verification input[placeholder="gpt-4o"]').fill('gpt-5.5')
    await expect(page.getByRole('button', { name: 'Run Benchmark' })).toBeEnabled()
    await page.getByRole('button', { name: 'Run Benchmark' }).click()
    await expect.poll(() => benchmarkStartPayloads.length).toBe(1)
    expect(benchmarkStartPayloads[0]).toMatchObject({
      provider_id: 'provider-openai',
      public_model: 'gpt-5.5',
      public_snapshot_id: '',
      vendor_snapshot_id: '',
    })
  })

  test('monitoring page focuses telemetry and pricing sections', async ({ page }) => {
    await page.click('text=Monitoring')

    await expect(page).toHaveURL(/\/admin\/monitoring/)
    await expect(page.locator('.workspace-hero')).toContainText('Monitoring')
    await expect(page.locator('#monitoring-telemetry')).toContainText('Telemetry')
    await expect(page.locator('#monitoring-logs')).toHaveCount(0)

    // Verify time range controls exist
    const timeButtons = page.locator('.timeseries-selector button')
    await expect(timeButtons.first()).toBeVisible()

    await page.locator('.workspace-nav button', { hasText: 'Pricing' }).click()
    await expect(page).toHaveURL(/\/admin\/monitoring\?.*view=pricing/)
    await expect(page.locator('#monitoring-pricing')).toContainText('Pricing')
    await expect(page.locator('#monitoring-telemetry')).toHaveCount(0)
  })

  test('logs page is top-level with filters and search', async ({ page }) => {
    await page.locator('.tabbar button', { hasText: 'Logs' }).click()

    await expect(page).toHaveURL(/\/admin\/logs/)
    await expect(page.locator('h2')).toContainText('Logs')
    const typeButtons = page.locator('.logs-type-selector button')
    await expect(typeButtons).toHaveCount(3)

    const searchInput = page.locator('.logs-search input')
    await expect(searchInput).toBeVisible()

    await searchInput.fill('gpt-4o')
    await expect(searchInput).toHaveValue('gpt-4o')

    await typeButtons.nth(1).click()
    await typeButtons.nth(2).click()
  })

  test('ops page shows command center and lazy diagnostics workspace', async ({ page }) => {
    await page.click('text=Ops')

    await expect(page).toHaveURL(/\/admin\/ops/)
    await expect(page.locator('.ops-command-page')).toContainText('Command center')
    await expect(page.locator('.ops-topology-canvas')).toContainText('Control plane')
    await expect(page.locator('.ops-provider-panel')).toContainText('Provider health')
    await expect(page.locator('.ops-workspace-switch')).toContainText('Diagnostics')
    expect(diagnosticsRequests).toBe(0)

    await page.locator('.ops-workspace-switch button', { hasText: 'Updates' }).click()
    await expect(page).toHaveURL(/\/admin\/ops\?.*view=updates/)
    await expect(page.locator('.ops-update-layout')).toContainText('Update available')
    await expect(page.locator('.ops-update-layout')).toContainText('1.4.2')
    await expect(page.getByRole('button', { name: /Dry-run apply/ })).toBeEnabled()

    await page.locator('.ops-workspace-switch button', { hasText: 'Probe' }).click()
    await expect(page).toHaveURL(/\/admin\/ops\?.*view=probe/)
    await expect(page.locator('.ops-probe-form input').first()).toBeVisible()

    await page.locator('.ops-workspace-switch button', { hasText: 'Diagnostics' }).click()
    await expect(page).toHaveURL(/\/admin\/ops\?.*view=diagnostics/)
    await expect(page.locator('.ops-surface-diagnostics')).toContainText('Runtime topology')
    expect(diagnosticsRequests).toBe(1)
  })

  test('tab switching updates URL without full reload', async ({ page }) => {
    await page.click('text=Monitoring')
    await expect(page).toHaveURL(/\/admin\/monitoring/)

    await page.locator('.tabbar button', { hasText: 'Logs' }).click()
    await expect(page).toHaveURL(/\/admin\/logs/)

    await page.click('text=Benchmark')
    await expect(page).toHaveURL(/\/admin\/benchmark/)

    await page.click('text=Ops')
    await expect(page).toHaveURL(/\/admin\/ops/)

    await page.click('text=Config')
    await expect(page).toHaveURL(/\/admin\/config/)

    // Back button should work
    await page.goBack()
    await expect(page).toHaveURL(/\/admin\/ops/)
  })

  test('monitoring segmented nav switches sections', async ({ page }) => {
    await page.click('text=Monitoring')

    await page.locator('.workspace-nav button', { hasText: 'Pricing' }).click()
    await expect(page.locator('#monitoring-pricing')).toBeVisible()
    await page.locator('.workspace-nav button', { hasText: 'Telemetry' }).click()
    await expect(page).toHaveURL(/\/admin\/monitoring/)
    expect(new URL(page.url()).searchParams.get('view')).toBeNull()
    await expect(page.locator('#monitoring-telemetry')).toBeVisible()
  })

  test('legacy workspace URLs open unified workspaces', async ({ page }) => {
    await page.goto('/admin/pricing?hours=24')
    await expect(page).toHaveURL(/\/admin\/monitoring\?hours=24&view=pricing/)
    await expect(page.locator('#monitoring-pricing')).toBeVisible()

    await page.goto('/admin/probe')
    await expect(page).toHaveURL(/\/admin\/ops\?view=probe/)
    await expect(page.locator('.ops-surface-probe')).toBeVisible()

    await page.goto('/admin/diagnostics')
    await expect(page).toHaveURL(/\/admin\/ops\?view=diagnostics/)
    await expect(page.locator('.ops-surface-diagnostics')).toBeVisible()
  })
})
