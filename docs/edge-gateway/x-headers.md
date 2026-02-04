---
title: Diagnostic headers
description: X- headers for tracking request processing and troubleshooting
---

# Diagnostic headers

For each request, Edge Gateway exposes several X- headers that help track URL processing and diagnose issues.

## Request headers

Headers you send to Edge Gateway.

### X-Render-Key

Authentication token for host authorization.

| Property | Value |
|----------|-------|
| Required | Yes |
| Value | Render key from host configuration |

### X-Request-ID

Custom request ID for distributed tracing.

| Property | Value |
|----------|-------|
| Required | No |
| Default | Auto-generated UUID |
| Max length | 36 characters |

If provided, Edge Gateway sanitizes and uses this ID for request tracking throughout the system. If absent, a UUID is generated automatically.

Providing a custom request ID allows you to easily debug request processing in high traffic environments.

## Response headers

Headers Edge Gateway returns in every response.

### X-Request-ID

Request tracing identifier. Always present in responses.

Returns either your custom ID (with a 5-character random prefix) or the auto-generated UUID.

### X-Render-Source

Indicates the source of the served content.

| Value | Description |
|-------|-------------|
| `rendered` | Freshly rendered by Chrome |
| `cache` | Served from render cache |
| `bypass` | Direct fetch from origin (no rendering) |
| `bypass_cache` | Served from bypass cache |

### X-Render-Cache

Cache operation status.

| Value | Description |
|-------|-------------|
| `new` | Freshly rendered content (not from cache) |
| `hit` | Cache found and valid |
| `stale` | Cache expired but served within stale TTL |
| `bypass` | Cache was bypassed |

### X-Render-Service

Render service instance that processed the request.

Only present when `X-Render-Source: rendered`.

### X-Cache-Age

Time in seconds since content was cached.

Only present when serving from cache (`X-Render-Source: cache` or `bypass_cache`).

### X-Matched-Rule

ID of the URL pattern rule that matched the request.

Only present when the request matched a configured URL rule with an ID.

### X-Unmatched-Dimension

Set to `true` when the User-Agent didn't match any configured dimension and a fallback was applied.

Only present when fallback behavior is triggered.

### X-Render-Action

Set to `status` when a status action (3xx, 4xx, 5xx) was performed.

Only present for status action responses.

### X-Processed-URL

Normalized URL after tracking parameter stripping.

Only present when tracking parameter removal is enabled and parameters were stripped.

## Troubleshooting with headers

Use these headers to diagnose issues without accessing logs.

### Verify content source

Check `X-Render-Source` to confirm whether content came from cache or was freshly rendered:

```bash
curl -I -H "X-Render-Key: your-key" https://edge.example.com/page
```

### Check cache freshness

Combine `X-Render-Cache` and `X-Cache-Age` to understand cache state:

- `X-Render-Cache: hit` with low `X-Cache-Age` = fresh cache
- `X-Render-Cache: stale` = expired but within stale TTL
- `X-Render-Cache: new` = cache miss, freshly rendered

### Trace requests

Use `X-Request-ID` to correlate logs across Edge Gateway and Render Service:

```bash
curl -H "X-Render-Key: your-key" \
     -H "X-Request-ID: my-trace-123" \
     https://edge.example.com/page
```

### Identify matched rules

Check `X-Matched-Rule` to verify which URL pattern configuration applied to the request.

### Debug dimension matching

If `X-Unmatched-Dimension: true` appears, the User-Agent didn't match any configured dimension. Review your dimension patterns or check that the correct User-Agent was sent.

## Internal headers

Headers used internally between services. Not typically relevant for client integration.

### X-Edge-Render

Added by Render Service to all outgoing Chrome requests. Used to prevent rendering loops when nginx is configured to route crawler traffic to Edge Gateway.

| Property | Value |
|----------|-------|
| Set by | Render Service |
| Value | Render Service ID (e.g., `rs-1`) |
| Purpose | Loop prevention |

When nginx detects this header, it should skip crawler routing and forward the request directly to origin. See [nginx integration](/integrations/nginx#loop-prevention) for configuration details.

### X-Internal-Auth

Authentication header for internal API endpoints between Edge Gateway instances and Cache Daemon.

| Property | Value |
|----------|-------|
| Required | Yes (for internal APIs) |
| Value | `cache_sharding.internal_auth_key` from config |

Used by Edge Gateway-to-Edge Gateway communication (cache pull/push/status) and Cache Daemon-to-Edge Gateway communication (recache).
