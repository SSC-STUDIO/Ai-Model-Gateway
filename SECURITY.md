# Security Policy

## Supported versions

This project currently supports only the latest state of the `main` branch.

If you are running an older fork or local copy, reproduce the issue against the latest code before reporting it.

## Reporting a vulnerability

Please do not open public GitHub issues for sensitive security problems.

Instead:

1. Prepare a minimal report with reproduction steps, impact, and affected endpoints or config areas.
2. Redact all secrets, API keys, tokens, local file paths, and customer data.
3. Send the report to the repository maintainer through a private channel available to the maintainer account.

Include:

- Affected version or commit SHA
- Whether the issue requires authenticated admin access
- Whether the issue can leak secrets, route traffic incorrectly, or bypass compatibility boundaries
- Logs or request/response samples with secrets removed

## Scope

Security-sensitive areas in this repository include:

- admin auth and config endpoints
- admin browser same-origin write boundary for cookie-auth sessions
- upstream request forwarding and header propagation
- secret handling in config and logs
- file and multipart proxy routes
- pricing / telemetry persistence if it can expose sensitive request data

## Admin browser threat model

- 浏览器中的 admin cookie 会话仅面向同源上下文；对 `POST /api/admin/config/publish`、`POST /api/admin/config/rollback` 的 cookie-auth 写请求必须通过同源 `Origin` 或 `Referer` 校验。
- Bearer token 面向脚本、CLI 与自动化调用，不依赖浏览器来源头。
- 如需报告 admin 相关问题，请说明是否涉及同源校验绕过、cookie 跨站写入，或 Bearer / cookie 模式边界混淆。

## Authentication

- Cookie-based browser sessions must be established via POST `/api/admin/login` with a JSON body containing the token.
- URL-based token login (`/admin?token=...`) is no longer supported to prevent token exposure in browser history, proxy logs, and referrer headers.
- Bearer token authentication is available for API access and bypasses same-origin checks.

## Configuration secrets

- Never commit real secrets to version control.
- Use environment variable placeholders (`${VAR_NAME}`) in config files.
- The default `configs/config.yaml` uses placeholder values; operators must set actual secrets via environment variables.
