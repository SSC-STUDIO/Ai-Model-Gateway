# Migration Guide

## v1.2.0 Breaking Changes

### Cache Configuration

The cache limit metric has changed from **byte-based** to **item-based**.

**Before:**
```yaml
cache:
  enabled: true
  max_size_mb: 256
  ttl_sec: 300
```

**After:**
```yaml
cache:
  enabled: true
  max_entries: 1000
  ttl_seconds: 300
```

- `max_size_mb` (int, MB) is replaced by `max_entries` (int, item count)
- `ttl_sec` is renamed to `ttl_seconds`
- Default: `max_entries = 1000`, `ttl_seconds = 300`

### Compression Configuration

The compression level is now an **integer** instead of a string.

**Before:**
```yaml
compression:
  enabled: true
  min_bytes: 1024
  level: "default"
```

**After:**
```yaml
compression:
  enabled: true
  min_size_bytes: 1024
  level: 5
```

- `level` is now an integer (0-9), where:
  - `0` or `-1` = default compression
  - `1` = best speed
  - `9` = best compression
- `min_bytes` is renamed to `min_size_bytes`
- Default: `level = 5`, `min_size_bytes = 1024`

### Provider Configuration

The `fallback_models` field is now properly compiled into the runtime snapshot.

```yaml
providers:
  - id: openai-demo
    name: OpenAI Demo
    base_url: https://api.openai.com
    fallback_models:
      - gpt-3.5-turbo
```

### Security Improvements

- Unknown roles in cookie verification now default to `viewer` instead of `admin`
- SSRF checker now correctly blocks path traversal and user info in URLs
- Random ID generation now uses `crypto/rand` instead of `time.Now().Nanosecond()`
- URL-based token login (`/admin?token=...`) has been removed. Use POST `/api/admin/login` with JSON body instead.
- Cookie-authenticated write requests to `/api/admin/config/*` now require same-origin `Origin` or `Referer` headers.
- Bearer token authentication bypasses same-origin checks for API access.
- Default `configs/config.yaml` no longer contains hardcoded demo credentials. Use environment variable placeholders.
