---
title: Configuration
description: How to structure Edge Gateway configuration files
---

# Configuration

## Overview

Edge Gateway uses a three-level configuration hierarchy that lets you set defaults globally and override them for specific hosts or URL patterns:

1. **Global** - Default settings for all hosts
2. **Host** - Per-domain overrides
3. **URL Rule** - Path-specific behavior within a host

Almost all settings can be overridden at any level, only a few must be defined at a specific level, such as server configuration at the global level or host identifiers at the host level.

## Configuration validation

Edge Gateway follows a fail-early principle, if the configuration has any issues, the application won't start. This ensures you catch problems during deployment rather than at runtime.

Use the `-t` flag to validate your configuration without starting the server:

```bash
./edge-gateway -c configs/edge-gateway.yaml -t
```

You can also test how specific URLs will be processed by passing a URL as an argument. See [Testing rules](url-rules.md#testing-rules) for details.

## Merge behavior

When settings exist at multiple levels, they merge as follows:

- **Scalar values** - More specific level wins
- **Array values** - Full replacement (no merging of array items)

### Scalar example

::: code-group
```yaml [Global - edge-gateway.yaml]
render:
  timeout: 30s
```

```yaml [Host - example.com.yaml]
render:
  timeout: 45s
```
:::

Result: `timeout` is `45s` for example.com.

### Array example

::: code-group
```yaml [Global - edge-gateway.yaml]
render:
  blocked_resource_types:
    - Image
    - Font
    - Stylesheet
```

```yaml [Host - example.com.yaml]
render:
  blocked_resource_types:
    - Image
```
:::

Result: `blocked_resource_types` is `[image]` for example.com - the host array fully replaces the global array.

## Configuration example

Complete edge-gateway.yaml with all parameters, descriptions, and default values:

```yaml
# Unique identifier for this Edge Gateway instance
# Required for cache sharding
# Default: ""
eg_id: "eg-1"

server:
  # HTTP server listen address
  # Required
  listen: ":10070"

  # Request timeout
  # Required
  timeout: 120s

  # TLS/HTTPS configuration (optional)
  # When enabled, Edge Gateway serves HTTPS alongside HTTP
  tls:
    # Enable HTTPS server
    # Default: false
    enabled: false

    # HTTPS server listen address (must differ from server.listen)
    # Required if enabled
    listen: ":10073"

    # Path to PEM-formatted certificate file
    # Relative paths resolved from config file directory
    # Required if enabled
    cert_file: "/path/to/certificate.pem"

    # Path to PEM-formatted private key file
    # Relative paths resolved from config file directory
    # Required if enabled
    key_file: "/path/to/private-key.pem"

# Internal server for inter-EG and daemon communication
# Required if cache sharding enabled
internal:
  # Listen address (host:port)
  # Default: ""
  listen: "192.168.1.10:10071"

  # Shared secret for authentication
  # Default: ""
  auth_key: "your-secret-key"

redis:
  # Redis connection address
  # Required
  addr: "localhost:6379"

  # Redis authentication password
  # Default: ""
  password: ""

  # Redis database number
  # Default: 0
  db: 0

storage:
  # Base path for cached HTML files
  # Required
  base_path: "/var/cache/edgecomet"

  cleanup:
    # Enable filesystem cleanup worker
    # Required
    enabled: true

    # How often cleanup runs
    # Required
    interval: 1h

    # Extra time beyond stale_ttl before deletion
    # Required
    safety_margin: 2h

render:
  cache:
    # Global render cache TTL
    # Default: 1h
    ttl: 1h

    # HTTP status codes to cache
    # Default: [200, 301, 302, 307, 308, 404]
    status_codes: [200, 301, 302, 307, 308, 404]

    expired:
      # Expiration strategy: "serve_stale" or "delete"
      # Default: "delete"
      strategy: "serve_stale"

      # How long to serve stale cache after expiration
      # Only used with strategy: "serve_stale"
      # Default: nil (disabled)
      stale_ttl: 24h

  events:
    # Page ready event
    # Options: "DOMContentLoaded", "load", "networkIdle", "networkAlmostIdle"
    # Default: "networkIdle"
    wait_for: "networkIdle"

    # Additional wait time after lifecycle event
    # Default: 0s
    additional_wait: 0s

  # Behavior for unmatched User-Agent
  # Options: "bypass", "block"
  # Default: "bypass"
  unmatched_dimension: "bypass"

  # Resource types to block during rendering
  # Options: "Document", "Stylesheet", "Image", "Media", "Font", "Script", "TextTrack", "XHR", "Fetch", "Prefetch", "EventSource", "WebSocket", "Manifest", "SignedExchange", "Ping", "CSPViolationReport", "Preflight", "Other"
  # Default: []
  blocked_resource_types:
    - Image
    - Media
    - Font

  # URL patterns to block during rendering
  # Default: []
  blocked_patterns:
    - "*.google-analytics.com"
    - "*.doubleclick.net"

  # Remove executable scripts from rendered HTML
  # Default: true
  strip_scripts: true

  # Viewport dimensions for different user agents
  # Required at global or host level
  dimensions:
    desktop:
      # Unique numeric identifier
      # Required
      id: 1

      # Viewport width in pixels
      # Required
      width: 1920

      # Viewport height in pixels
      # Required
      height: 1080

      # User-Agent string for rendering
      # Required
      render_ua: "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"

      # User-Agent patterns to match this dimension
      # Supports: exact, wildcard (*), regexp (~), case-insensitive regexp (~*)
      # Required
      match_ua:
        - "*Googlebot*"
        - "*Bingbot*"
        - "*baiduspider*"

    mobile:
      id: 2
      width: 412
      height: 915
      render_ua: "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Mobile Safari/537.36"
      match_ua:
        - "*Googlebot-Mobile*"
        - "*iPhone*"

bypass:
  # Timeout for origin requests
  # Default: 30s
  timeout: 30s

  # User-Agent for origin requests
  # Required
  user_agent: "EdgeComet/1.0"

  cache:
    # Enable bypass response caching
    # Default: false
    enabled: false

    # TTL for bypass cache
    # Default: 30m
    ttl: 30m

    # Status codes to cache in bypass mode
    # Default: [200]
    status_codes: [200]

registry:
  # Render service selection strategy
  # Options: "least_loaded", "most_available"
  # Default: "least_loaded"
  selection_strategy: "least_loaded"

log:
  # Global log level
  # Options: "debug", "info", "warn", "error", "dpanic", "panic", "fatal"
  # Required
  level: "info"

  console:
    # Enable console output
    # Default: true (if no outputs enabled)
    enabled: true

    # Console format
    # Options: "console" (with colors), "json"
    # Default: "console"
    format: "console"

    # Override global level for console
    # Default: uses global level
    level: "debug"

  file:
    # Enable file output
    # Default: false
    enabled: false

    # Path to log file
    # Required if enabled
    path: "/var/log/edgecomet/edge-gateway.log"

    # File format
    # Options: "text", "json"
    # Default: "text"
    format: "json"

    # Override global level for file
    # Default: uses global level
    level: "info"

    rotation:
      # Max file size in MB
      # Required if file logging enabled
      max_size: 100

      # Max age in days
      # Required if file logging enabled
      max_age: 30

      # Max number of backup files
      # Required if file logging enabled
      max_backups: 5

      # Compress rotated files
      # Default: false
      compress: true

metrics:
  # Enable Prometheus metrics
  # Default: false
  enabled: true

  # Metrics server listen address (MUST differ from server.listen)
  # Required if enabled
  listen: ":10079"

  # Metrics endpoint path
  # Default: "/metrics"
  path: "/metrics"

  # Prometheus metric prefix
  # Default: "EdgeComet"
  namespace: "edgecomet"

tracking_params:
  # Enable parameter stripping
  # Default: true
  strip: true

  # Replace all default params with custom list
  # Use this to completely override defaults
  # Supports: exact, wildcard (*), regexp (~, ~*)
  # params:
  #   - "my_tracking_param"

  # Extend default params with additional patterns
  # Built-in: utm_*, gclid, fbclid, msclkid, _ga, _gl, mc_cid, mc_eid, _ke, ref, referrer
  # Supports: exact, wildcard (*), regexp (~, ~*)
  params_add:
    - "sid"
    - "session_*"

bothit_recache:
  # Enable automatic recache on bot visits
  # Default: false
  enabled: false

  # Recache interval (30m - 24h range)
  # Default: 24h
  interval: 24h

  # Bot User-Agent patterns
  # Supports: exact, wildcard (*), regexp (~, ~*)
  # Default: []
  match_ua:
    - "*Googlebot*"
    - "*Bingbot*"

cache_sharding:
  # Enable cache sharding across multiple EG instances
  # Default: false
  enabled: false

  # Number of EG instances to replicate cache to
  # Default: 2
  replication_factor: 2

  # Distribution strategy
  # Options: "hash_modulo", "random", "primary_only"
  # Default: "hash_modulo"
  distribution_strategy: "hash_modulo"

  # Push rendered cache to replicas immediately
  # Default: true
  push_on_render: true

  # Store pulled cache locally
  # Default: true
  replicate_on_pull: true

# HTTP headers to pass through from responses
# Default: ["Content-Type", "Cache-Control", "Expires", "Last-Modified", "ETag", "Location"]
safe_headers:
  - "Content-Type"
  - "Cache-Control"
  - "Expires"
  - "Last-Modified"
  - "ETag"
  - "Location"

hosts:
  # Glob pattern to load host configurations
  # Required
  include: "hosts.d/*.yaml"
```

## Host configuration example

Example host file (hosts.d/example.com.yaml):

```yaml
hosts:
  - # Unique host identifier
    # Required
    id: 1

    # Domain name
    # Required
    domain: "example.com"

    # API key for client authentication (X-Render-Key header)
    # Required
    render_key: "your-api-key-here"

    # Enable/disable this host
    # Required
    enabled: true

    render:
      # Chrome render timeout
      # Required
      timeout: 45s

      cache:
        # Override global cache TTL
        ttl: 2h

        # Override status codes to cache
        status_codes: [200, 301, 404]

        expired:
          strategy: "serve_stale"
          stale_ttl: 48h

      events:
        # Override page ready event
        wait_for: "networkIdle"
        additional_wait: 500ms

      # Override unmatched User-Agent behavior
      unmatched_dimension: "bypass"

      # Override blocked resources (replaces global array)
      blocked_resource_types:
        - Image
        - Media

      # Override blocked patterns (replaces global array)
      blocked_patterns:
        - "*.facebook.com"
        - "*.twitter.com"

      # Override script stripping
      strip_scripts: true

      # Override dimensions (replaces global map)
      # Omit to inherit global dimensions
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
          match_ua:
            - "*Googlebot*"
            - "*Bingbot*"

    bypass:
      timeout: 15s
      user_agent: "EdgeComet/1.0 (example.com)"

      cache:
        enabled: true
        ttl: 1h
        status_codes: [200, 301]

    tracking_params:
      strip: true
      params_add:
        - "campaign_id"

    bothit_recache:
      enabled: true
      interval: 12h
      match_ua:
        - "*Googlebot*"

    # Override safe headers (replaces global array)
    safe_headers:
      - "Content-Type"
      - "Cache-Control"
      - "X-Custom-Header"

    # URL pattern rules
    # See url-patterns.md for pattern syntax
    url_rules:
      # Render blog pages with custom TTL
      - match: "/blog/*"
        action: "render"
        render:
          cache:
            ttl: 4h

      # Bypass API endpoints
      - match: "~/api/v[0-9]+/.*"
        action: "bypass"
        bypass:
          timeout: 10s
          cache:
            enabled: true
            ttl: 5m

      # Block admin pages
      - match: "/admin/*"
        action: "block"

      # Return 404 for removed pages
      - match: "/old-section/*"
        action: "status_404"

      # Redirect with custom status
      - match: "/legacy"
        action: "status"
        status:
          code: 301
          headers:
            Location: "https://example.com/new-page"

      # Render search pages only when query param exists
      - match: "/search"
        match_query:
          q: "*"
        action: "render"

      # Bypass static assets
      - match: "~*\\.(css|js|woff2?)$"
        action: "bypass"
```

## TLS/HTTPS configuration

Edge Gateway can serve HTTPS alongside HTTP when TLS is enabled. Both servers run simultaneously, sharing the same request handler and configuration.

### Configuration options

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | boolean | No | `false` | Enable HTTPS server |
| `listen` | string | Yes (if enabled) | - | HTTPS listen address (e.g., `:10073`) |
| `cert_file` | string | Yes (if enabled) | - | Path to PEM-formatted certificate |
| `key_file` | string | Yes (if enabled) | - | Path to PEM-formatted private key |

### Path resolution

Certificate and key file paths can be absolute or relative:

- **Absolute paths**: Used as-is (e.g., `/etc/ssl/certs/server.pem`)
- **Relative paths**: Resolved from the config file's directory

::: code-group
```yaml [Absolute paths]
server:
  tls:
    enabled: true
    listen: ":10073"
    cert_file: "/etc/ssl/certs/server.crt"
    key_file: "/etc/ssl/private/server.key"
```

```yaml [Relative paths]
# If config is at /opt/edgecomet/configs/edge-gateway.yaml
# cert resolves to /opt/edgecomet/configs/certs/server.crt
server:
  tls:
    enabled: true
    listen: ":10073"
    cert_file: "certs/server.crt"
    key_file: "certs/server.key"
```
:::

### Security notes

- **TLS 1.3 minimum**: Only TLS 1.3 connections are accepted. Clients using TLS 1.2 or earlier will fail to connect.
- **No HTTP/2**: FastHTTP does not support HTTP/2, so HTTPS connections use HTTP/1.1.
- **Certificate changes require restart**: Edge Gateway loads certificates at startup. To use new certificates, restart the service.

### Port conflict detection

The TLS listen port must not conflict with:

- HTTP server port (`server.listen`)
- Metrics server port (`metrics.listen`)
- Internal server port (`internal.listen`)

Configuration validation fails if any ports conflict.

### Error messages

| Error | Cause |
|-------|-------|
| `TLS enabled but tls.listen not specified` | Missing listen address |
| `TLS enabled but tls.cert_file not specified` | Missing certificate path |
| `TLS enabled but tls.key_file not specified` | Missing key path |
| `TLS listen address invalid: {address}` | Invalid address format or port |
| `TLS cert_file not found: {path}` | Certificate file doesn't exist |
| `TLS key_file not found: {path}` | Key file doesn't exist |
| `TLS cert_file not readable: {path}` | Certificate file permission denied |
| `TLS key_file not readable: {path}` | Key file permission denied |
| `TLS listen port conflicts with server.listen` | Same port as HTTP server |
| `TLS listen port conflicts with metrics.port` | Same port as metrics server |
| `TLS listen port conflicts with internal_server.listen` | Same port as internal server |
