# Admin UI Upgrade — Archived Review Evidence

**Date**: 2026-04-17  
**Version bumped**: 1.1.0 → 1.1.1  
**Scope**: Frontend CSS & component polish only (historical snapshot)

> Archived note: this document captures a transient review state from April 17, 2026.
> Since then, the default `controld` admin shell has been reduced to `overview / telemetry / timeseries / history / benchmark`,
> and the old `settings / probe / logs / audit` source modules and related legacy theme blocks referenced below are no longer part of the active default UI.

---

## Changed Files

| File | Change |
|---|---|
| `web/admin/src/theme/index.css` | +~2000 lines: skeleton screens, empty states, animations, micro-interactions, scrollbar, focus glow, switch, toast, input icons, copy button, spinner, table sort, shine effects |
| `web/admin/src/app.tsx` | Tab content animation wrapper, background orbs, ToastContainer integration, addToast prop drilling |
| `web/admin/src/components/Switch.tsx` | New toggle switch component |
| `web/admin/src/components/CopyButton.tsx` | New copy-to-clipboard button with feedback |
| `web/admin/src/components/ToastContainer.tsx` | New global toast notification container |
| `web/admin/src/hooks/useToast.ts` | New toast state management hook |
| `web/admin/src/hooks/useFlashValue.ts` | New value-change flash detection hook |
| `web/admin/src/hooks/index.ts` | Export new hooks |
| `web/admin/src/components/Charts.tsx` | Empty chart icon upgrade, line glow filter |
| `web/admin/src/components/LogViewer.tsx` | Connection status upgrade, empty state upgrade |
| `web/admin/src/components/tabs/OverviewTab.tsx` | Skeleton grid, flash values, panel-stagger |
| `web/admin/src/components/tabs/TelemetryTab.tsx` | Skeleton grid, chart skeletons, flash summary metrics |
| `web/admin/src/components/tabs/TimeSeriesTab.tsx` | Chart skeletons, upgraded empty states |
| `web/admin/src/components/tabs/HistoryTab.tsx` | Upgraded empty state box |
| `web/admin/src/components/tabs/AuditTab.tsx` | Skeleton on loading, upgraded empty state |
| `web/admin/src/components/tabs/BenchmarkTab.tsx` | Chart skeletons, upgraded empty states |
| `web/admin/src/components/tabs/SettingsTab.tsx` | Switch for booleans, copy button, toast integration, spinner |
| `web/admin/src/components/tabs/ProbeTab.tsx` | Switch for toggles, input icons, copy button, toast integration, spinner |
| `CHANGELOG.md` | Added v1.1.1 entry |
| `VERSION` | Bumped to 1.1.1 |

---

## Build Validation

### Frontend
```
$ npm run build
> tsc && vite build
✓ 48 modules transformed.
dist/index.html                   1.24 kB │ gzip:  0.61 kB
dist/assets/index-DOZGi-oy.css   90.73 kB │ gzip: 15.55 kB
dist/assets/vendor-dFasNT4X.js   19.52 kB │ gzip:  7.79 kB
dist/assets/index-CBk6PPOU.js   155.28 kB │ gzip: 46.76 kB
✓ built in 2.03s
```
**Result**: PASS

### Backend
```
$ go build ./...
# ai-model-gateway/cmd/controld (PRE-EXISTING RPC mismatch, unrelated)
```
**Result**: UI-only change, no backend impact

---

## UI Improvements Summary (3 Rounds)

### Round 1 — Loading & Empty States
- Skeleton shimmer loading screens for all tabs
- Beautiful empty state boxes with icons, titles, and hints
- Tab switch fade-in + translateY animation
- Panel stagger entrance animation (cards fade in sequentially)
- SSE status dot pulse ring animation
- Button press scale effect
- Panel glass shine edge line
- Topbar gradient border
- Custom scrollbar (theme-aware)
- Focus glow ring on inputs

### Round 2 — Atmosphere & Micro-interactions
- 3 floating background orbs with drift animation
- Login page brand icon breathe animation
- Metric value count-up flash effect (useFlashValue hook)
- Chart line glow SVG filter
- Global Toast notification system (4 types, icons, slide animations)
- Switch toggle component replacing all native checkboxes
- LogViewer connection status with pulse dot
- LogViewer empty state upgrade
- Diff line stagger animation
- Table row hover micro-slide
- Text selection color customization

### Round 3 — Form Polish & Utilities
- Button loading spinner animation (replaces text when busy)
- Input prefix icons for ProbeTab forms (🏷️🔗🔒🤖⚙️⏱️⚖️🔄)
- Copy-to-clipboard button with "copied" feedback state
- Settings JSON editor copy button
- Probe result area copy button
- Table sortable header indicators
- Form divider line
- Section badge component
- Inline code styling
- Progress bar component
- Keyboard shortcut badge (kbd)
- Hover reveal actions pattern
- Shine sweep effect on hover
- Animated border rotation effect
- Glass card variant

---

## Evidence Paths

- Build output: `web/admin/dist/` (fresh build artifacts)
- This review note: `web/admin/docs/ui-upgrade-2026-04-17.md`
