---
title: Caching
description: How Edge Gateway stores and manages rendered content
---

# Caching

## Overview

Edge Gateway caches rendered HTML and bypass responses to reduce Chrome load and serve bots faster.
Two cache types exist: `render cache` for Chrome-rendered pages, and `bypass cache` for direct origin responses. Both use Redis for metadata and filesystem for content.
On each request, EG checks Redis for cache metadata. On hit, it serves content from disk. On miss, it renders or bypasses, then stores the result.

Cache settings can be configured at global, host, and URL pattern levels.

## Cache storage

### Redis metadata

Redis stores cache metadata for quick lookups without filesystem access. Each cache entry contains the URL and its hash, creation and expiration timestamps, dimension ID, and replication status across EG instances. Edge Gateway checks Redis first on every request to determine cache hits, verify expiration, and locate replicas in sharded deployments. The key format is `cache:{host_id}:{dimension_id}:{url_hash}`.

### Filesystem content

Edge Gateway serves cached content directly from disk, enabling high request throughput with minimal system load.

You configure the base storage path in your edge-gateway.yaml:

```yaml
storage:
  base_path: "cache/"
```

The file path format includes the **expiration** date:

```
{host}/{Y}/{M}/{D}/{H}/{m}/{hash}_{dim}.html
```

This date-based structure allows you to delete expired cache directories with `rm -rf` instead of removing thousands of individual files.

The path uses the expiration date only, not the stale period. The cleanup worker handles stale TTL separately.

### Storage cleanup

The cleanup worker periodically removes expired directories:

```yaml
storage:
  base_path: "cache/"
  cleanup:
    enabled: true
    interval: 1h
    safety_margin: 2h
```

| Parameter | Description |
|-----------|-------------|
| `enabled` | Enable or disable the cleanup worker. |
| `interval` | How often the worker runs. |
| `safety_margin` | Extra time beyond stale TTL before deletion. |

The worker checks directories against cache expiration and stale TTL before removing them.

## Emergency cache deletion

If your disk becomes full or corrupted, you can safely delete the filesystem cache. Edge Gateway attempts to pull cache files from other replicas. If no replicas exist, it sends requests for new renders.

The same applies to Redis. If you encounter OOM errors or other problems, you can clear the Redis database. Edge Gateway switches to bypass mode for a few seconds. Once all service heartbeats renew, it resumes normal rendering and caching.

## Cache configuration

### TTL and status codes

Configure cache duration and which HTTP status codes to cache:

```yaml
render:
  cache:
    ttl: 1h                           # Duration: 30m, 1h, 24h, 7d
    status_codes: [200, 301, 302]     # Default: [200, 301, 302, 307, 308, 404]
```

You can override these settings at host or pattern level.

### Bypass cache

For bypass cache, specify which HTTP status codes to cache:

```yaml
bypass:
  cache:
    enabled: true                     # Default: false
    ttl: 30m                          # Default: 30m
    status_codes: [200]               # Default: [200]
```

Common configurations:

| Use case | Status codes |
|----------|-------------|
| Success only | `[200]` |
| With redirects | `[200, 301, 302]` |
| With not found | `[200, 301, 302, 404]` |

## Expiration strategies

### Delete strategy

Removes cache immediately when TTL expires:

```yaml
render:
  cache:
    ttl: 1h
    expired:
      strategy: "delete"
```

Use this strategy when content changes frequently and stale responses are unacceptable.

### Serve stale strategy

Serves expired content while Edge Gateway requests a fresh render:

```yaml
render:
  cache:
    ttl: 1h
    expired:
      strategy: "serve_stale"
      stale_ttl: 24h
```

The `stale_ttl` defines how long expired content remains available. After this period, the cache entry is removed.

Use this strategy for high-availability scenarios where serving outdated content is preferable to waiting for a new render.

## Bot hit recache

Bot hit recache keeps your cache fresh by automatically re-rendering pages when bots access them.

When a bot hits cached content, Edge Gateway checks the User-Agent against configured patterns. If matched, EG adds the URL to the autorecache queue in Redis with a scheduled time (current time + interval). The bot receives the cached content immediately. Cache Daemon scans the queue and processes URLs when their scheduled time arrives, sending them for re-rendering.

```yaml
bothit_recache:
  enabled: true
  interval: "24h"
  match_ua:
    - $GooglebotSearchDesktop
    - $GooglebotSearchMobile
    - $BingbotDesktop
    - "*Slurp*"
    - "*DuckDuckBot*"
```

| Parameter | Description |
|-----------|-------------|
| `enabled` | Enable or disable bot hit recache. |
| `interval` | Minimum time between recache triggers per URL. |
| `match_ua` | Bot User-Agent patterns that trigger recache. Supports wildcards, regexp, and [aliases](./dimensions.md#available-aliases). |

This ensures bots find reasonably fresh content without manual cache invalidation.

This feature requires Cache Daemon to be running. Edge Gateway only adds URLs to the queue; Cache Daemon processes the queue and triggers re-renders.

## Cache invalidation

Delete cache metadata to force fresh renders on next request.

The API removes cache metadata from Redis immediately. Edge Gateway renders fresh content on the next bot request. Filesystem cleanup runs separately via EG's cleanup worker.

**Endpoint:** `POST /internal/cache/invalidate`

**Headers:** `X-Internal-Auth`, `Content-Type: application/json`

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `host_id` | integer | Yes | Host identifier from configuration. |
| `urls` | array | Yes | URLs to invalidate. |
| `dimension_ids` | array | No | Dimension IDs to invalidate. Empty = all dimensions. |

**Example:**

```bash
curl -X POST http://localhost:10090/internal/cache/invalidate \
  -H "X-Internal-Auth: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "host_id": 1,
    "urls": ["https://example.com/old-page"],
    "dimension_ids": []
  }'
```

## Configuration example

Complete cache configuration with all settings:

```yaml
storage:
  base_path: "cache/"
  cleanup:
    enabled: true
    interval: 1h
    safety_margin: 2h

render:
  cache:
    ttl: 1h
    status_codes: [200, 301, 302, 307, 308, 404]
    expired:
      strategy: "serve_stale"
      stale_ttl: 24h

bypass:
  cache:
    enabled: true
    ttl: 30m
    status_codes: [200]

bothit_recache:
  enabled: true
  interval: "24h"
  match_ua:
    - $GooglebotSearchDesktop
    - $GooglebotSearchMobile
    - $BingbotDesktop
```
