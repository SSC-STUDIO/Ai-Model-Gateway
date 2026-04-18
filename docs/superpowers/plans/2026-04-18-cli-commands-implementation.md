# CLI 命令实现计划

**Goal:** 实现完整的 CLI 命令集

**Architecture:** 基于 internal/cli/client.go，在 cmd/gateway/commands/ 中实现命令

**Tech Stack:** Go 1.21+, flag, tabwriter, json

---

## 文件结构

已完成：
- internal/cli/client.go (416 行)
- internal/gateway/converter/ (协议转换层)

需要实现：
- cmd/gateway/commands/config.go
- cmd/gateway/commands/provider.go
- cmd/gateway/commands/telemetry.go
- cmd/gateway/commands/publish.go
- cmd/gateway/commands/test.go

需要修改：
- cmd/gateway/main.go

---

详细实现请参考设计文档：docs/superpowers/specs/2026-04-18-protocol-conversion-and-cli-design.md
