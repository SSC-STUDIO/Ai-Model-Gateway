# AI Model Gateway CLI

The gateway now provides a comprehensive command-line interface for managing the service.

## Usage

```bash
gateway [global-options] <command> [options]
```

### Global Options

- `-config string` - Path to config file (default: "configs/config.yaml")

## Commands

### Start the Gateway

```bash
# Start with default config
gateway start

# Start with custom config
gateway -config /path/to/config.yaml start

# Legacy mode (backward compatible)
gateway -config /path/to/config.yaml
```

### Validate Configuration

Check if your configuration file is valid without starting the server:

```bash
gateway validate
gateway validate -config /path/to/config.yaml
```

Output:
```
✓ Configuration is valid
  Listen: :18080
  Upstreams: 9
  Admin enabled: true
  Health enabled: true
  Bridge enabled: true
```

### Health Check

Check if the gateway is running and healthy:

```bash
# Default endpoint
gateway health

# Custom endpoint
gateway health -endpoint http://localhost:18080/-/health -timeout 10s
```

### Windows Service Management

On Windows, you can manage the gateway as a system service:

#### Install Service

```bash
# Install with default config
gateway install

# Install with custom config
gateway -config C:\gateway\config.yaml install
```

#### Start Service

```bash
gateway service-start
```

#### Stop Service

```bash
gateway service-stop
```

#### Check Service Status

```bash
gateway service-status
```

#### Uninstall Service

```bash
gateway uninstall
```

### Configuration Management

```bash
# Reload configuration without restart
gateway config -reload

# Export current configuration
gateway config -export -output config.backup.yaml
```

## Examples

### Quick Start

```bash
# Start the gateway
gateway start

# In another terminal, check health
gateway health
```

### Production Deployment on Windows

```bash
# 1. Validate config
gateway validate -config C:\gateway\production.yaml

# 2. Install as service
gateway -config C:\gateway\production.yaml install

# 3. Start the service
gateway service-start

# 4. Check status
gateway service-status

# 5. Monitor health
gateway health -endpoint http://localhost:18080/-/health
```

### Configuration Management

```bash
# Edit config, then validate before applying
gateway validate

# If running as service, reload config
gateway config -reload
```

## Exit Codes

- `0` - Success
- `1` - General error
- `2` - Configuration error
- `3` - Service error (Windows)

## Environment Variables

- `GATEWAY_VERSION` - Version string displayed by `gateway version`
