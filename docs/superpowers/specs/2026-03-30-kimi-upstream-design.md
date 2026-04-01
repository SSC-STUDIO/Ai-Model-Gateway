# Kimi Upstream Gateway Design

**Date:** 2026-03-30

**Goal:** 接通本机 `AI-Model-Gateway` 的 Kimi 官方上游，使当前运行实例能在 `http://127.0.0.1:18080` 暴露 `kimi-k2.5`，并同步补齐仓库示例与使用文档。

## Scope

- 让当前运行实例实际加载包含 `kimi-official` 的配置并成功启动
- 验证 `/v1/models` 能返回 `kimi-k2.5`
- 验证 `/v1/responses` 能通过 Kimi 上游成功响应
- 更新 `README.md` 与 `configs/config.example.yaml`
- 补充如何让 `kimi-cli` 通过本地网关访问 Kimi 的文档说明

## Non-Goals

- 不重构网关的密钥管理机制
- 不引入新的 provider 类型或协议桥接能力
- 不清理与本任务无关的现有 upstream 条目
- 不做发布、打 tag 或提交 git commit

## Current Findings

- 仓库当前分支为 `codex/ai-Ai-Model-Gateway`
- 项目已支持一般化 upstream 配置，`internal/config`、`internal/router`、`internal/proxy`、`internal/server` 都已经覆盖所需字段
- 仓库中的 `configs/config.yaml` 已存在 `kimi-official` upstream 条目，模型白名单包含 `kimi-k2.5`
- 当前检查时本机 `127.0.0.1:18080` 没有监听进程，因此本轮不仅要“配置存在”，还要让实际实例启动并加载该配置

## Recommended Approach

### Runtime instance

- 以项目根目录下的 `configs/config.yaml` 作为运行配置源
- 使用项目现有的 `scripts/rebuild-and-restart.ps1` 或等效启动路径，避免手工拼接零散命令
- 启动后先验证 `/-/health`，再验证 `/v1/models`，最后验证 `/v1/responses`
- 如果当前实例仍未暴露 `kimi-k2.5`，优先排查：
  - 实际启动是否使用了其他配置文件
  - `configs/config.yaml` 是否在重载时被覆盖
  - Kimi upstream 是否因配置校验、网络失败或健康检查失败被排除

### Repository docs

- 在 `configs/config.example.yaml` 中加入脱敏的 `kimi-official` 示例
- 在 `README.md` 中补充：
  - Kimi upstream 示例配置
  - 验证步骤
  - `kimi-cli` 通过本地网关访问 Kimi 的最小配置思路

## Risks

- `scripts/rebuild-and-restart.ps1` 可能需要管理员权限来接管或修复 Windows 服务注册
- 当前 `configs/config.yaml` 是运行配置，修改时必须避免误改其他有效 upstream
- Kimi 官方上游可用性、配额或地域网络问题可能导致启动后模型存在但请求失败

## Verification Plan

- 运行项目测试：`go test ./...`
- 冒烟检查：
  - `GET /-/health`
  - `GET /v1/models`
  - `POST /v1/responses` with model `kimi-k2.5`
- 若脚本重启成功，确认端口 `18080` 有监听且返回新实例响应

## Deliverables

- 可运行的本地网关实例，包含 Kimi upstream
- 更新后的 `README.md`
- 更新后的 `configs/config.example.yaml`
- 必要时对运行配置做最小修正
