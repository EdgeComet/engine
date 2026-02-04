---
title: Bypass mode
description: When and how Edge Gateway fetches content directly from origin
---

# Bypass mode

## Overview
Bypass mode fetches content directly from the origin server without rendering. Use bypass mode in two ways: 
as a configured action for specific URLs or as an automatic fallback when rendering fails.

Configure bypass mode for URLs that don't require JavaScript rendering, such as API endpoints, 
or pages that already render on the server. 
You can also configure bypass mode for specific bot types by setting URL patterns that match based on the detected user agent.

When rendering encounters errors (timeout, service unavailability, or render failure), 
the system automatically falls back to bypass mode to ensure the request completes
successfully. This failsafe behavior prevents downtime and maintains service availability.

Bypass mode supports optional caching with configurable TTL and status code filtering.
Bypass cache never overwrites render cache - render cache always takes priority when both exist for the same URL.

## When bypass occurs

### Automatic fallback
The system automatically falls back to bypass mode when rendering fails or is unavailable. This ensures requests complete successfully even when the render service encounters
problems. Bypass mode triggers when no render services are registered, all services are at capacity, or service selection fails. It also activates during request timeouts,
render service failures, or Chrome process errors.

Bypass mode also serves as a fallback for cache-related issues in distributed environments, including lock acquisition errors, concurrent render timeouts, and cache pull
failures across sharded instances. When you configure `unmatched_dimension_action: bypass`, requests with User-Agents that don't match any configured dimension automatically
use bypass mode. These responses include the `X-Unmatched-Dimension: true` header to indicate the fallback behavior occurred.


### Explicit configuration
Configure bypass mode for URLs that don't require JavaScript rendering:

**URL pattern matching**
```yaml
url_rules:
  - match: "/api/*"
    action: bypass
  - match: "*.pdf"
    action: bypass
  - match: "/static/*"
    action: bypass
```
**Common use cases**
- API endpoints (/api/*, /v1/*)
- Static files (*.pdf, *.zip, *.jpg, *.png)
- Pre-rendered pages (AMP, server-side rendered content)
- Administrative interfaces (/admin/*, /dashboard/*)
- Health check endpoints (/health, /status)

## Bypass configuration

### Available options

Bypass mode has three configuration options:

- `timeout` - Request timeout for fetching from origin (read and write operations)
- `user_agent` - User-Agent string sent to origin servers
- `cache` - Bypass response caching configuration (enabled, ttl, status_codes)

### Default values

**Global defaults** (edge-gateway.yaml):

```yaml
bypass:
  timeout: 30s
  user_agent: "Mozilla/5.0 (compatible; EdgeComet/1.0)"
  cache:
    enabled: false
    ttl: 30m
    status_codes: [200]
```

Bypass caching is disabled by default. Hosts must explicitly opt-in to cache bypass responses.

### Use case: server-side rendered content

Bypass caching benefits websites that already serve server-side rendered HTML but experience performance or load time issues. Caching responses reduces load time from 2-4 seconds to approximately 10ms, improving crawl budget efficiency for search engine and AI bots. This helps maintain fresh content in search indexes and supports faster indexation.

### Configuration hierarchy

Bypass configuration follows a three-level hierarchy: Global → Host → URL Pattern. Each level can override parent settings.

**Global level** (edge-gateway.yaml):

```yaml
bypass:
  timeout: 30s
  user_agent: "Mozilla/5.0 (compatible; EdgeComet/1.0)"
  cache:
    enabled: false
    ttl: 30m
    status_codes: [200]
```

**Host level** (hosts.yaml):

```yaml
hosts:
  - domain: example.com
    bypass:
      timeout: 10s
      user_agent: "MyBot/1.0"
      cache:
        enabled: true
        ttl: 5m
        status_codes: [200, 404]
```

**Pattern level** (hosts.yaml):

```yaml
url_rules:
  - match: "/api/*"
    action: bypass
    bypass:
      timeout: 5s
      cache:
        ttl: 1m
        status_codes: [200]
```

Each level inherits settings from its parent and can override specific fields. Pattern-level settings take highest priority.

### Status code filtering

Configure which HTTP status codes to cache using the `status_codes` array:

```yaml
bypass:
  cache:
    status_codes: [200]  # Only successful responses
```

Common patterns:
- `[200]` - Only successful responses (safest, default)
- `[200, 404]` - Success and not found pages
- `[200, 301, 302]` - Success and redirects
- `[200, 301, 302, 404]` - All cacheable responses

Use restrictive filtering for dynamic content and broader filtering for stable resources.

### TTL settings

Control cache duration with the `ttl` field. Set `ttl: 0` to explicitly disable caching for specific patterns while keeping it enabled at parent levels.

```yaml
bypass:
  cache:
    ttl: 5m   # 5 minutes
    # ttl: 1h   # 1 hour
    # ttl: 24h  # 24 hours
    # ttl: 0    # Disable caching
```

**Recommended values:**
- Short (1-5m) - Dynamic APIs, frequently changing content
- Medium (1h) - Semi-static content, product pages
- Long (24h) - Static resources, archived content

### Cache priority

Render cache always takes precedence over bypass cache. The system never overwrites render cache with bypass cache.

**When render fails after render cache exists:**
- System serves from render cache (even if stale but servable)
- Falls back to bypass only if render cache is unavailable
- Bypass response is NOT cached (render cache preserved)

**When render succeeds after bypass cache exists:**
- New render cache overwrites bypass cache metadata
- Subsequent requests serve from render cache

This priority ensures higher-quality rendered content is always preferred over simple bypass responses.

### Error handling

When bypass requests fail due to network issues, timeouts, or connection errors, the system returns a 502 Bad Gateway response to the client:

```
HTTP/1.1 502 Bad Gateway
Content-Type: text/plain; charset=utf-8
X-Render-Source: bypass

Bad Gateway: Origin unreachable
```

**Common failure scenarios:**
- Network timeout (exceeds configured `timeout` value)
- Connection refused (origin server down)
- DNS resolution failure
- Network unreachable

Bypass mode makes a single attempt to fetch from origin with no retry logic. Configure appropriate timeout values based on your origin server's expected response time.
