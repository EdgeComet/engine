---
title: Configuration
description: How to configure Render Service settings
---

# Configuration

## Overview

Render Service is a simple daemon focused on Chrome pool management. All rendering behavior (timeouts, lifecycle events, resource blocking, script stripping) is configured at the EG host and URL pattern level and passed with each request. See [Render mode](../edge-gateway/render-mode.md) for those settings.

RS configuration covers server settings, Chrome pool sizing, restart policies, Redis connection, and metrics. Configuration is validated at startup. Any errors will prevent the daemon from starting.


## Configuration example

::: code-group
```yaml [render-service.yaml]
# Server settings (required)
server:
  id: "rs-1"                          # Unique identifier for this RS instance
  listen: "0.0.0.0:10080"             # Listen address

# Redis connection (required)
redis:
  addr: "localhost:6379"
  password: ""                        # Optional
  db: 0

# Chrome pool settings (required)
chrome:
  pool_size: 10                       # "auto" or integer

  warmup:
    url: "https://example.com/"       # URL to load during instance warmup
    timeout: 10s

  restart:
    after_count: 50                   # Restart instance after N renders
    after_time: 30m                   # Restart instance after duration

  render:
    max_timeout: 50s                  # Hard limit that cancels stuck renders
                                      # Server timeout = max_timeout + 10s

# Logging
log:
  level: "info"                       # Global: debug, info, warn, error
  console:
    enabled: true                     # Default: true if both outputs disabled
    format: "console"                 # "console" or "json" (default: console)
    # level: "warn"                   # Optional override
  file:
    enabled: false
    path: "./log/render-service.log"  # Required if enabled
    format: "text"                    # "text" or "json" (default: text)
    # level: "debug"                  # Optional override
    rotation:
      max_size: 100                   # Megabytes
      max_age: 30                     # Days
      max_backups: 10
      compress: true

# Metrics (optional)
metrics:
  enabled: true
  listen: ":10089"                    # Must differ from server.listen
  path: "/metrics"
  namespace: "edgecomet"              # Prometheus metric prefix
```
:::
