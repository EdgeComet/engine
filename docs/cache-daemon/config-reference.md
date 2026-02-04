---
title: Configuration reference
description: Complete Cache Daemon configuration parameters
---

# Configuration reference

## Overview

Cache Daemon uses a single YAML configuration file. All settings are documented below with their types, valid values, and defaults.

## Complete configuration example

```yaml
# Path to Edge Gateway configuration file
# Required
eg_config: "edge-gateway.yaml"

# Unique identifier for this daemon instance
# Required
daemon_id: "cache-daemon-1"

redis:
  # Redis server address
  # Required
  addr: "localhost:6379"

  # Redis password
  # Default: ""
  password: ""

  # Redis database number
  # Default: 0
  db: 0

scheduler:
  # How often the scheduler runs its main loop
  # Default: 1s
  # Minimum: 100ms
  tick_interval: 1s

  # How often to check normal and autorecache queues
  # Default: 60s
  # Must be a multiple of tick_interval
  normal_check_interval: 60s

internal_queue:
  # Maximum entries in the internal queue
  # Default: 1000
  max_size: 1000

  # Maximum retry attempts before discarding failed recache
  # Default: 3
  max_retries: 3

  # Base delay for exponential backoff on retries
  # Default: 5s
  retry_base_delay: 5s

recache:
  # Percentage of render service capacity to reserve for online traffic
  # Default: 0.30
  # Valid range: 0.0-1.0
  rs_capacity_reserved: 0.30

  # Timeout for each individual URL recache request
  # Default: 60s
  timeout_per_url: 60s

http_api:
  # Enable the HTTP API server
  # Default: true
  enabled: true

  # Listen address for the HTTP API server
  # Required when enabled
  listen: ":10090"

  # Timeout for incoming API requests
  # Default: 30s
  request_timeout: 30s

  # Enable scheduler control API endpoints (pause/resume)
  # Default: false
  scheduler_control_api: false

logging:
  # Global log level
  # Default: "info"
  # Options: "debug", "info", "warn", "error"
  level: "info"

  console:
    # Enable console output
    # Default: false
    enabled: false

    # Console output format
    # Default: "console"
    # Options: "console", "json"
    format: "console"

    # Console-specific log level (overrides global)
    # level: "warn"

  file:
    # Enable file logging
    # Default: false
    enabled: true

    # Log file path
    # Required when enabled
    path: "/var/log/cache-daemon.log"

    # File output format
    # Default: "json"
    # Options: "text", "json"
    format: "json"

    # File-specific log level (overrides global)
    # level: "debug"

    rotation:
      # Maximum file size in MB before rotation
      # Default: 100
      max_size: 100

      # Maximum age in days to retain old log files
      # Default: 30
      max_age: 30

      # Maximum number of old log files to retain
      # Default: 10
      max_backups: 10

      # Compress rotated log files
      # Default: true
      compress: true

metrics:
  # Enable Prometheus metrics
  # Default: false
  enabled: true

  # Metrics server listen address (must differ from http_api.listen)
  # Required when enabled
  listen: ":10099"

  # Metrics endpoint path
  # Default: "/metrics"
  path: "/metrics"

  # Prometheus metric namespace prefix
  # Default: "EdgeComet"
  namespace: "edge_comet"
```

## Configuration validation

The daemon validates configuration at startup. Common validation rules:

- `eg_config` and `daemon_id` are required
- `redis.addr` is required, `redis.db` must be >= 0
- `scheduler.tick_interval` must be >= 100ms
- `scheduler.normal_check_interval` must be a multiple of `tick_interval`
- `internal_queue.max_size` must be > 0
- `internal_queue.max_retries` must be >= 1
- `recache.rs_capacity_reserved` must be between 0.0 and 1.0
- `http_api.listen` and `metrics.listen` must differ when both enabled
- Log levels must be one of: debug, info, warn, error
- Console format must be: json, console
- File format must be: json, text

Validation errors are logged and the daemon exits with non-zero status.
