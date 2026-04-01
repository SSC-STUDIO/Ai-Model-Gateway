# Installation Guide

This guide covers various methods to install and run the AI Model Gateway.

## Table of Contents

- [System Requirements](#system-requirements)
- [Installation Methods](#installation-methods)
  - [Method 1: Download Pre-built Binary (Recommended)](#method-1-download-pre-built-binary-recommended)
  - [Method 2: Build from Source](#method-2-build-from-source)
  - [Method 3: Run with Go](#method-3-run-with-go)
- [Configuration](#configuration)
- [Running as Windows Service](#running-as-windows-service)
- [Post-Installation Verification](#post-installation-verification)
- [Upgrading](#upgrading)
- [Uninstallation](#uninstallation)

## System Requirements

- **Operating System**: Windows 10/11, Linux, macOS
- **Go**: 1.26+ (only required for building from source)
- **Memory**: 512MB minimum, 1GB recommended
- **Disk**: 100MB for binary + space for logs and SQLite database
- **Network**: Access to upstream AI service endpoints

## Installation Methods

### Method 1: Download Pre-built Binary (Recommended)

1. Visit the [GitHub Releases](https://github.com/SSC-STUDIO/ai-model-gateway/releases) page
2. Download the appropriate binary for your platform:
   - Windows: `gateway-windows-amd64.exe`
   - Linux: `gateway-linux-amd64`
   - macOS: `gateway-darwin-amd64`
3. Rename the binary to `gateway` (or `gateway.exe` on Windows)
4. Place it in your desired installation directory
5. (Optional) Add the directory to your system PATH

### Method 2: Build from Source

```powershell
# Clone the repository
git clone https://github.com/SSC-STUDIO/ai-model-gateway.git
cd ai-model-gateway

# Build the binary
go build -o gateway.exe ./cmd/gateway

# Verify the build
.\gateway.exe version
```

### Method 3: Run with Go

For development or quick testing:

```powershell
# Run directly without building
go run ./cmd/gateway -config ./configs/config.yaml
```

## Configuration

1. Copy the example configuration file:

```powershell
Copy-Item .\configs\config.example.yaml .\configs\config.yaml
```

2. Edit `configs/config.yaml` with your settings:

### Minimum Required Configuration

```yaml
listen: ":18080"

upstreams:
  - name: my-upstream
    base_url: "https://api.openai.com/v1"
    api_key: "sk-your-api-key"
    models:
      - gpt-4
      - gpt-3.5-turbo
    enabled: true

admin:
  enabled: true
  auth_token: "your-secure-admin-token"
```

### Configuration File Locations

The gateway looks for configuration in the following order:

1. Path specified via `-config` flag
2. `configs/config.yaml` (default)
3. `./config.yaml`

### Validate Configuration

Before starting, validate your configuration:

```powershell
.\gateway.exe validate
```

## Running as Windows Service

The gateway can run as a Windows service for production deployments.

### Install as Service

Run PowerShell as Administrator:

```powershell
# Install with default config path
.\gateway.exe install

# Or specify custom config path
.\gateway.exe -config C:\gateway\config.yaml install
```

### Manage the Service

```powershell
# Start the service
.\gateway.exe service-start

# Check status
.\gateway.exe service-status

# Stop the service
.\gateway.exe service-stop

# Uninstall the service
.\gateway.exe uninstall
```

### Alternative: Using PowerShell Scripts

If you prefer using scripts:

```powershell
# Install service
.\scripts\install-service.ps1

# Uninstall service
.\scripts\uninstall-service.ps1
```

## Post-Installation Verification

1. **Check if the gateway is running**:

```powershell
.\gateway.exe health
```

2. **Test the API**:

```powershell
# List available models
curl.exe http://127.0.0.1:18080/v1/models

# Check health endpoint
curl.exe http://127.0.0.1:18080/-/health
```

3. **Access the Admin Dashboard**:
   - Open `http://127.0.0.1:18080/admin` in your browser
   - Set the `aigw_admin_token` cookie to your configured auth token
   - Or use the settings page: `http://127.0.0.1:18080/admin/settings`

## Upgrading

### Binary Upgrade

1. Stop the service (if running as service):
```powershell
.\gateway.exe service-stop
```

2. Backup your configuration:
```powershell
Copy-Item .\configs\config.yaml .\configs\config.yaml.backup
```

3. Download and replace the binary

4. Validate the new binary:
```powershell
.\gateway.exe version
.\gateway.exe validate
```

5. Start the service:
```powershell
.\gateway.exe service-start
```

### Configuration Migration

When upgrading, check the [CHANGELOG.md](../CHANGELOG.md) for any configuration changes. The gateway will validate your config on startup and report any issues.

## Uninstallation

### Remove Windows Service

```powershell
# Run as Administrator
.\gateway.exe service-stop
.\gateway.exe uninstall
```

### Remove Binary and Data

```powershell
# Remove binary
Remove-Item .\gateway.exe

# Remove configuration (optional)
Remove-Item .\configs\config.yaml

# Remove data files (optional)
Remove-Item .\data\telemetry.db
Remove-Item .\data\pricing-cache.json
```

## Troubleshooting

### Service Won't Start

1. Check Windows Event Viewer for errors
2. Verify config file path is correct
3. Ensure the service account has permissions to access the config file
4. Check logs in the `logs/` directory

### Port Already in Use

If port 18080 is already in use:

1. Change the `listen` address in config.yaml:
```yaml
listen: ":18081"  # Use different port
```

2. Or stop the process using port 18080

### Configuration Errors

Use the validate command to check your config:

```powershell
.\gateway.exe validate -config .\configs\config.yaml
```

## Next Steps

- Read the [CLI documentation](cli.md) for command reference
- Check the [README.md](../README.md) for feature documentation
- Review [CONTRIBUTING.md](../CONTRIBUTING.md) for development setup
