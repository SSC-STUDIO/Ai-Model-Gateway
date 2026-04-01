# AI Model Gateway v1.0.0 Release Notes

🎉 **Production Release - v1.0.0**

We're excited to announce the first stable release of AI Model Gateway, a high-performance OpenAI-compatible AI model proxy gateway with multi-provider support, comprehensive admin dashboard, and now featuring a full CLI interface.

---

## 🚀 Major Features

### Full CLI Support
The gateway now includes a comprehensive command-line interface for all operations:

| Command | Description |
|---------|-------------|
| `gateway start` | Start the gateway server (default) |
| `gateway validate` | Validate configuration file |
| `gateway config` | Configuration management (reload, export) |
| `gateway health` | Check gateway health status |
| `gateway version` | Show version information |
| `gateway install` | Install as Windows service |
| `gateway uninstall` | Uninstall Windows service |
| `gateway service-start` | Start Windows service |
| `gateway service-stop` | Stop Windows service |
| `gateway service-status` | Check Windows service status |

**Global Options:**
- `-config string` - Path to config file (default: `configs/config.yaml`)

### Windows Service Support
Native Windows service integration with full lifecycle management:
- Install/uninstall as Windows service
- Start/stop service control
- Status monitoring
- Automatic service recovery

### Architecture Improvements
- **Modular CLI Design**: Clean separation of concerns with testable components
- **Health Check Subsystem**: Dedicated health monitoring with configurable probes
- **Metrics Collection**: Comprehensive telemetry and performance metrics
- **Configuration Builder**: Fluent API for programmatic config construction
- **Interface-Based Testing**: Mockable interfaces for unit testing

---

## 📊 Test Coverage Achievements

| Package | Coverage | Key Tests |
|---------|----------|-----------|
| `internal/cli` | >85% | Command execution, flag parsing, health checks, service management |
| `internal/config` | >80% | Config loading, validation, normalization, saving |
| `internal/router` | >75% | Health monitoring, metrics, strategy selection |

**Testing Highlights:**
- Comprehensive unit tests for all CLI commands
- Mock-based testing for external dependencies
- Edge case coverage (empty commands, nil flags, error paths)
- Context timeout testing
- Service lifecycle testing

---

## 🔐 Security Enhancements

This release includes all security fixes from previous batches:
- CWE-22: Path traversal vulnerability fixes
- CWE-117: Log injection prevention
- CWE-502: Insecure deserialization fixes
- CWE-693: Security headers enhancement
- CWE-770: Rate limiting for admin endpoints
- CWE-521: Admin token minimum length validation
- CWE-269: Enhanced authentication middleware with audit logging
- CWE-918: SSRF vulnerability fixes in proxy handler

---

## ⚠️ Breaking Changes

### Script Deprecation
The following PowerShell scripts have been **replaced** by CLI commands:

| Old Script | New CLI Command |
|------------|-----------------|
| `scripts/start-gateway.ps1` | `gateway start` |
| `scripts/validate-config.ps1` | `gateway validate` |
| `scripts/install-service.ps1` | `gateway install` |
| `scripts/uninstall-service.ps1` | `gateway uninstall` |
| `scripts/check-health.ps1` | `gateway health` |

### Configuration Changes
- No breaking changes to YAML configuration format
- All existing `config.yaml` files remain compatible

---

## 📖 Migration Guide

### From Scripts to CLI

**Starting the Gateway:**
```powershell
# Before (PowerShell script)
.\scripts\start-gateway.ps1

# After (CLI)
.\ai-model-gateway.exe start
# Or simply
.\ai-model-gateway.exe
```

**Validating Configuration:**
```powershell
# Before
.\scripts\validate-config.ps1

# After
.\ai-model-gateway.exe validate
```

**Installing as Windows Service:**
```powershell
# Before (as Administrator)
.\scripts\install-service.ps1

# After (as Administrator)
.\ai-model-gateway.exe install
```

**Checking Health:**
```powershell
# Before
.\scripts\check-health.ps1

# After
.\ai-model-gateway.exe health
```

---

## 📦 Binary Distribution

| Asset | Size | Description |
|-------|------|-------------|
| `ai-model-gateway.exe` | ~12 MB | Windows x64 executable (self-contained) |

**Build Command:**
```bash
go build -trimpath -ldflags="-s -w -X ai-model-gateway/internal/cli.Version=1.0.0" -o ai-model-gateway.exe ./cmd/gateway
```

---

## 🎯 Supported Endpoints

- `POST /v1/chat/completions`
- `POST /v1/completions`
- `POST /v1/embeddings`
- `POST /v1/responses`
- `POST /v1/responses/compact`
- `GET /v1/responses/{response_id}`
- `DELETE /v1/responses/{response_id}`
- `POST /v1/moderations`
- `POST /v1/images/generations`
- `POST /v1/images/edits`
- `POST /v1/images/variations`
- `POST /v1/audio/speech`
- `POST /v1/audio/transcriptions`
- `POST /v1/audio/translations`
- `GET /v1/files`
- `POST /v1/files`
- `GET /v1/files/{file_id}`
- `DELETE /v1/files/{file_id}`
- `GET /v1/files/{file_id}/content`
- `GET /v1/models`
- `GET /-/health`
- `GET /admin` - Admin dashboard overview
- `GET /admin/settings` - Admin settings page
- `GET /-/admin/data` - Telemetry data API
- `GET /-/admin/timeseries` - Time-series metrics API
- `GET|PUT /-/admin/config` - Configuration management
- `GET /-/admin/config/export` - Config export
- `GET /-/admin/config/history` - Config history
- `GET /-/admin/config/history/{version_id}/diff` - Config diff
- `POST /-/admin/config/rollback` - Config rollback

---

## 🔧 Requirements

- Go 1.26+ (for building from source)
- Windows 10/11 or Windows Server 2016+ (for Windows service)
- SQLite3 (bundled)

---

## 🙏 Acknowledgments

Special thanks to all contributors who helped make this release possible through code contributions, bug reports, and feature suggestions.

---

## 📄 License

MIT License - See [LICENSE](LICENSE) for details.

---

**Full Changelog**: [CHANGELOG.md](CHANGELOG.md)
