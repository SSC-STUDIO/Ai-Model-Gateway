# AI Model Gateway Admin UI 重构设计规范

**日期**: 2026-04-09
**版本**: 1.0
**状态**: 待实现

---

## 1. 概述

### 1.1 目标
完全重写 Admin UI 前端，采用现代化技术栈，保证与后端API和配置格式100%兼容。

### 1.2 技术栈
| 层级 | 技术选型 | 版本要求 |
|------|----------|----------|
| 框架 | Preact + TypeScript | preact@^10.25, typescript@^5.7 |
| 样式 | Tailwind CSS | tailwindcss@^3.4 |
| 图表 | Chart.js | chart.js@^4.4 |
| 状态 | Preact Signals | @preact/signals@^1.3 |
| 构建 | Vite | vite@^6.3 |
| 工具 | clsx + tailwind-merge | 用于class合并 |

### 1.3 兼容性要求
- **后端API**: `/api/admin/*` 所有端点保持不变
- **配置格式**: YAML/JSON 配置结构不变
- **认证机制**: Cookie-based auth 保持不变
- **SSE端点**: `/api/admin/events` 保持不变

---

## 2. 目录架构

```
web/admin/src/
├── main.tsx                     # 应用入口
├── app.tsx                      # 根组件（仅路由逻辑）
├── index.css                    # Tailwind入口 + CSS Variables
│
├── signals/                     # Preact Signals 状态管理
│   ├── index.ts                 # 统一导出
│   ├── auth.ts                  # 认证状态
│   ├── data.ts                  # 数据状态
│   ├── ui.ts                    # UI状态
│   └── sse.ts                   # SSE连接状态
│
├── components/                  # 可复用UI组件
│   ├── index.ts                 # 统一导出
│   │
│   ├── layout/                  # 布局组件
│   │   ├── Shell.tsx            # 应用外壳
│   │   ├── Sidebar.tsx          # 侧边导航
│   │   ├── Header.tsx           # 顶部栏
│   │   └── PageContainer.tsx    # 页面容器
│   │
│   ├── common/                  # 通用组件
│   │   ├── Button.tsx           # 按钮
│   │   ├── Card.tsx             # 卡片
│   │   ├── Table.tsx            # 表格
│   │   ├── Dialog.tsx           # 对话框
│   │   ├── Toast.tsx            # 提示
│   │   ├── Spinner.tsx          # 加载动画
│   │   ├── Badge.tsx            # 徽章
│   │   ├── Select.tsx           # 下拉选择
│   │   ├── Input.tsx            # 输入框
│   │   └── EmptyState.tsx       # 空状态
│   │
│   ├── charts/                  # 图表组件
│   │   ├── Chart.tsx            # Chart.js封装基类
│   │   ├── LineChart.tsx        # 折线图
│   │   ├── DonutChart.tsx       # 环形图
│   │   └── BarChart.tsx         # 柱状图
│   │
│   └── providers/               # Context Providers
│       ├── I18nProvider.tsx     # 国际化
│       └── ThemeProvider.tsx    # 主题
│
├── pages/                       # 页面组件
│   ├── index.ts                 # 统一导出 + 路由配置
│   ├── Login.tsx                # 登录页
│   ├── Overview.tsx             # 概览页
│   ├── Telemetry.tsx            # 遥测页
│   ├── Benchmark.tsx            # 基准测试页
│   ├── TimeSeries.tsx           # 时序数据页
│   ├── Settings.tsx             # 配置页
│   ├── History.tsx              # 配置历史页
│   ├── Probe.tsx                # 探测工具页
│   ├── Logs.tsx                 # 日志页
│   └── Audit.tsx                # 审计页
│
├── hooks/                       # 自定义Hooks
│   ├── index.ts
│   ├── useFetch.ts              # 数据获取
│   ├── useSSE.ts                # SSE连接
│   ├── useDebounce.ts           # 防抖
│   └── useLocalStorage.ts       # 本地存储
│
├── lib/                         # 工具库
│   ├── api.ts                   # API客户端
│   ├── format.ts                # 格式化工具
│   ├── chart.ts                 # 图表配置
│   ├── cn.ts                    # class合并工具
│   └── constants.ts             # 常量定义
│
├── i18n/                        # 国际化
│   ├── index.ts
│   ├── types.ts
│   └── locales/
│       ├── en.json
│       ├── zh.json
│       ├── ja.json
│       ├── ko.json
│       ├── es.json
│       ├── fr.json
│       └── de.json
│
└── types.ts                     # TypeScript 类型定义
```

---

## 3. 状态管理架构

### 3.1 Signals 设计原则
- **单一职责**: 每个signal文件负责一个领域
- **计算属性**: 使用 `computed` 派生状态
- **副作用**: 使用 `effect` 处理副作用
- **持久化**: 关键状态同步到 localStorage

### 3.2 状态结构

```typescript
// signals/auth.ts
import { signal, computed } from '@preact/signals'

export const authToken = signal<string | null>(null)
export const authLoading = signal(false)
export const authError = signal<string | null>(null)

export const isAuthenticated = computed(() => authToken.value !== null)

export async function login(token: string): Promise<boolean>
export async function logout(): Promise<void>
export async function checkAuth(): Promise<boolean>

// signals/data.ts
export const overview = signal<OverviewData | null>(null)
export const telemetry = signal<TelemetryData | null>(null)
export const timeseries = signal<TimeSeriesData | null>(null)
export const benchmark = signal<BenchmarkData | null>(null)
export const config = signal<ConfigData | null>(null)
export const configHistory = signal<HistoryData | null>(null)

export const dataLoading = signal<Set<string>>(new Set()) // 正在加载的资源
export const dataError = signal<Map<string, string>>(new Map())

// 数据获取函数
export async function fetchOverview(): Promise<void>
export async function fetchTelemetry(): Promise<void>
export async function fetchBenchmark(params: BenchmarkParams): Promise<void>
export async function fetchConfig(): Promise<void>
export async function fetchConfigHistory(): Promise<void>
export async function saveConfig(config: ConfigData): Promise<boolean>
export async function rollbackConfig(versionId: string): Promise<boolean>

// signals/ui.ts
export type TabKey = 'overview' | 'telemetry' | 'benchmark' | 'timeseries' | 'settings' | 'history' | 'probe' | 'logs' | 'audit'
export type Theme = 'light' | 'dark'
export type LocaleKey = 'en' | 'zh' | 'ja' | 'ko' | 'es' | 'fr' | 'de'

export const activeTab = signal<TabKey>('overview')
export const theme = signal<Theme>('light')
export const locale = signal<LocaleKey>('zh')
export const sidebarCollapsed = signal(false)
export const refreshInterval = signal(0) // 0 = off, ms otherwise

// signals/sse.ts
export const sseConnected = signal(false)
export const sseReconnecting = signal(false)

export function connectSSE(): void
export function disconnectSSE(): void
```

### 3.3 状态流转图

```
┌─────────────────────────────────────────────────────────────┐
│                        App Shell                             │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                    UI Signals                        │    │
│  │  activeTab | theme | locale | sidebarCollapsed      │    │
│  └─────────────────────────────────────────────────────┘    │
│                           │                                  │
│                           ▼                                  │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                   Auth Signals                       │    │
│  │  authToken | isAuthenticated | authLoading          │    │
│  └─────────────────────────────────────────────────────┘    │
│                           │                                  │
│              ┌────────────┴────────────┐                    │
│              ▼                         ▼                    │
│  ┌───────────────────┐     ┌───────────────────┐           │
│  │   Data Signals    │     │    SSE Signals    │           │
│  │ overview/telemetry│◄────│ connected/events  │           │
│  │ benchmark/config  │     │ reconnecting      │           │
│  └───────────────────┘     └───────────────────┘           │
└─────────────────────────────────────────────────────────────┘
```

---

## 4. 组件设计规范

### 4.1 组件原则
- **函数组件**: 全部使用函数组件 + Hooks
- **memo优化**: 纯展示组件使用 `memo`
- **Props类型**: 必须定义 TypeScript interface
- **样式**: 使用 Tailwind classes，通过 `cn()` 合并

### 4.2 通用组件规格

#### Button
```typescript
interface ButtonProps {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost'
  size?: 'sm' | 'md' | 'lg'
  loading?: boolean
  disabled?: boolean
  class?: string
  children: ComponentChildren
  onClick?: () => void
}
```

#### Card
```typescript
interface CardProps {
  title?: string
  subtitle?: string
  class?: string
  children: ComponentChildren
}
```

#### Table
```typescript
interface TableProps<T> {
  columns: ColumnDef<T>[]
  data: T[]
  loading?: boolean
  emptyMessage?: string
  class?: string
}

interface ColumnDef<T> {
  key: keyof T | string
  header: string
  width?: string
  align?: 'left' | 'center' | 'right'
  render?: (value: T[keyof T], row: T, index: number) => VNode
}
```

#### Dialog
```typescript
interface DialogProps {
  open: boolean
  title: string
  message?: string
  confirmLabel?: string
  cancelLabel?: string
  onConfirm: () => void
  onCancel: () => void
  loading?: boolean
}
```

#### Toast
```typescript
interface ToastProps {
  message: string
  type: 'success' | 'error' | 'warning' | 'info'
  duration?: number
  onClose: () => void
}
```

### 4.3 图表组件规格

所有图表组件继承自基础 Chart 组件：

```typescript
// components/charts/Chart.tsx
interface ChartProps {
  data: unknown
  title?: string
  height?: number
  class?: string
  options?: ChartConfiguration['options']
}

// components/charts/LineChart.tsx
interface LineChartProps extends ChartProps {
  data: { timestamp: number; value: number }[]
  color?: string
  unit?: string
  fill?: boolean
}

// components/charts/DonutChart.tsx
interface DonutChartProps extends ChartProps {
  data: { label: string; value: number; color?: string }[]
  showLegend?: boolean
  showPercentage?: boolean
}

// components/charts/BarChart.tsx
interface BarChartProps extends ChartProps {
  data: { label: string; value: number; color?: string }[]
  horizontal?: boolean
  unit?: string
}
```

---

## 5. 页面组件设计

### 5.1 Login 页面
- 居中登录表单
- Token 输入框（password类型）
- 登录按钮 + loading状态
- 错误提示

### 5.2 Overview 页面
- 时间窗口卡片组（1m/5m/1h/24h）
- 请求量折线图
- 模型分布环形图
- 运行时信息卡片
- 可用模型列表

### 5.3 Telemetry 页面
- 刷新控制栏
- 请求/延迟/成功率图表
- 错误列表表格
- 最近请求表格
- 费用追踪模块

### 5.4 Benchmark 页面
- 时间范围选择器
- 模型过滤选择器
- 性能指标表格
- 成本分析图表

### 5.5 TimeSeries 页面
- 时间窗口选择器
- 自动刷新控制
- 多图表网格布局
- 数据下钻功能

### 5.6 Settings 页面
- 结构化编辑视图（默认）
- JSON 编辑视图（高级）
- 配置分区卡片
- 保存确认对话框
- 操作成功/失败提示

### 5.7 History 页面
- 版本选择下拉
- Diff 视图（新增/删除/修改高亮）
- 回滚确认对话框

### 5.8 Probe 页面
- Provider 配置表单
- 测试结果展示
- 响应详情

### 5.9 Logs 页面
- SSE 实时日志流
- 级别过滤
- 搜索过滤
- 暂停/继续控制
- 导出功能

### 5.10 Audit 页面
- 审计日志表格
- 时间/操作/操作者/详情列
- 分页（如需要）

---

## 6. API 客户端设计

### 6.1 基础配置
```typescript
// lib/api.ts
const API_BASE = '/api/admin'

async function request<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const resp = await fetch(path, {
    credentials: 'same-origin',
    headers: {
      'Accept': 'application/json',
      'Content-Type': 'application/json',
      ...options.headers,
    },
    ...options,
  })

  if (!resp.ok) {
    const text = await resp.text()
    throw new Error(text || `${resp.status} ${resp.statusText}`)
  }

  return resp.json()
}
```

### 6.2 API 方法
```typescript
export const api = {
  // === Auth ===
  login: (token: string) =>
    request<void>(`${API_BASE}/auth/login`, {
      method: 'POST',
      body: JSON.stringify({ token }),
    }),

  logout: () =>
    request<void>(`${API_BASE}/auth/logout`, { method: 'POST' }),

  // === Overview & Telemetry ===
  getOverview: () =>
    request<OverviewData>(`${API_BASE}/overview`),

  getTelemetry: () =>
    request<TelemetryData>(`${API_BASE}/data`),

  getTimeSeries: () =>
    request<TimeSeriesData>(`${API_BASE}/timeseries`),

  // === Benchmark ===
  getBenchmark: (params: { hours: number; models?: string[] }) => {
    const query = new URLSearchParams()
    query.append('hours', String(params.hours))
    params.models?.forEach(m => query.append('models', m))
    return request<BenchmarkData>(`${API_BASE}/models/benchmark?${query}`)
  },

  // === Config ===
  getConfig: () =>
    request<ConfigData>(`${API_BASE}/config`),

  saveConfig: (config: ConfigData) =>
    request<ConfigData>(`${API_BASE}/config`, {
      method: 'PUT',
      body: JSON.stringify(config),
    }),

  getConfigHistory: () =>
    request<HistoryData>(`${API_BASE}/config/history`),

  getConfigDiff: (versionId: string) =>
    request<DiffData>(`${API_BASE}/config/history/${encodeURIComponent(versionId)}/diff`),

  rollbackConfig: (versionId: string) =>
    request<ConfigData>(`${API_BASE}/config/rollback`, {
      method: 'POST',
      body: JSON.stringify({ version_id: versionId }),
    }),

  // === Probe ===
  testUpstream: (upstream: UpstreamConfig) =>
    request<ProbeResult>(`${API_BASE}/upstreams/test`, {
      method: 'POST',
      body: JSON.stringify({ upstream }),
    }),
}
```

---

## 7. 样式系统

### 7.1 Tailwind 配置
```javascript
// tailwind.config.js
module.exports = {
  darkMode: 'class',
  content: ['./src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        primary: {
          50: '#eff6ff',
          500: '#3b82f6',
          600: '#2563eb',
          700: '#1d4ed8',
        },
        success: '#22c55e',
        warning: '#f59e0b',
        danger: '#ef4444',
      },
      fontFamily: {
        sans: ['-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'sans-serif'],
        mono: ['SF Mono', 'Monaco', 'Cascadia Code', 'Consolas', 'monospace'],
      },
    },
  },
  plugins: [],
}
```

### 7.2 CSS Variables（用于主题切换）
```css
/* index.css */
@tailwind base;
@tailwind components;
@tailwind utilities;

@layer base {
  :root {
    --color-bg: 255 255 255;
    --color-text: 17 24 39;
    --color-border: 0 0 0;
  }

  .dark {
    --color-bg: 15 23 42;
    --color-text: 241 245 249;
    --color-border: 255 255 255;
  }
}
```

### 7.3 组件样式约定
- 使用 `dark:` 前缀处理暗色模式
- 使用 `hover:`, `focus:`, `active:` 处理交互状态
- 使用 `transition-*` 添加过渡动画
- 遵循 4px/8px 间距系统

---

## 8. 国际化

### 8.1 结构
```typescript
// i18n/index.ts
import { signal } from '@preact/signals'
import en from './locales/en.json'
import zh from './locales/zh.json'
// ...其他语言

const locales = { en, zh, ja, ko, es, fr, de }

export const locale = signal<LocaleKey>('zh')

export function t(key: string): string {
  const keys = key.split('.')
  let value: unknown = locales[locale.value]
  for (const k of keys) {
    value = value?.[k]
  }
  return typeof value === 'string' ? value : key
}
```

### 8.2 使用方式
```tsx
// 组件中使用
import { t } from '../i18n'

function MyComponent() {
  return <h1>{t('overview.title')}</h1>
}
```

---

## 9. 实现计划

### Phase 1: 基础设施（预计 2-3 小时）
1. 安装依赖：tailwindcss, chart.js, @preact/signals, clsx, tailwind-merge
2. 配置 Tailwind
3. 创建目录结构
4. 实现 `cn()` 工具函数

### Phase 2: 状态层（预计 2 小时）
1. 实现 signals/auth.ts
2. 实现 signals/data.ts
3. 实现 signals/ui.ts
4. 实现 signals/sse.ts

### Phase 3: 通用组件（预计 3-4 小时）
1. Button, Card, Spinner
2. Table, Dialog, Toast
3. Badge, Select, Input, EmptyState
4. 图表组件（Chart, LineChart, DonutChart, BarChart）

### Phase 4: 布局组件（预计 2 小时）
1. Shell
2. Sidebar
3. Header
4. PageContainer

### Phase 5: 页面组件（预计 4-5 小时）
1. Login
2. Overview
3. Telemetry
4. Benchmark
5. TimeSeries
6. Settings
7. History
8. Probe
9. Logs
10. Audit

### Phase 6: 集成测试（预计 1-2 小时）
1. 构建测试
2. 功能测试
3. 样式调整
4. 性能优化

---

## 10. 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Preact Signals 兼容性 | 中 | 已验证与 Preact 10.x 完全兼容 |
| Chart.js 打包体积 | 低 | 按需引入，支持 tree-shaking |
| Tailwind 学习曲线 | 低 | 已有成熟规范，快速上手 |
| API 兼容性 | 高 | 严格遵循现有 API 契约 |

---

## 11. 验收标准

1. **功能完整**: 所有现有功能正常工作
2. **API兼容**: 后端无需修改
3. **性能达标**: 首屏加载 < 2s
4. **响应式**: 支持 320px - 2560px 屏幕
5. **暗色模式**: 完整支持 light/dark 主题
6. **国际化**: 支持 7 种语言
7. **代码质量**: TypeScript 无错误，ESLint 通过
