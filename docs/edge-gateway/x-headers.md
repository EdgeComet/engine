---
title: Diagnostic headers
description: EC- headers for tracking request processing and troubleshooting
---

# Diagnostic headers

For each request, Edge Gateway exposes a small set of `EC-` headers that help track URL
processing and diagnose issues. EdgeComet's own informational headers use the `EC-` prefix;
functional and internal headers (`X-Render-Key`, `X-Edge-Render`, `X-Internal-Auth`) keep the
`X-` prefix.

## Request headers

Headers you send to Edge Gateway.

### X-Render-Key

Authentication token for host authorization.

| Property | Value |
|----------|-------|
| Required | Yes |
| Value | Render key from host configuration |

### EC-Request-ID

Custom request ID for distributed tracing.

| Property | Value |
|----------|-------|
| Required | No |
| Default | Auto-generated UUID |
| Max length | 36 characters |

If provided, Edge Gateway sanitizes and uses this ID for request tracking throughout the
system. If absent, a UUID is generated automatically. Only `EC-Request-ID` is read on the
way in; an inbound `X-Request-ID` is ignored.

Providing a custom request ID allows you to easily debug request processing in high traffic
environments.

## Response headers

EdgeComet adds at most these informational headers to a response.

### EC-Request-ID

Request tracing identifier. Always present in responses.

Returns either your custom ID (with a 5-character random prefix) or the auto-generated UUID.

### EC-Source

Indicates how the response was produced. Present on every served response.

| Value | Description |
|-------|-------------|
| `render` | Freshly rendered by Chrome |
| `bypass` | Direct fetch from origin (no rendering) |
| `render_cache` | Fresh content from the render cache |
| `bypass_cache` | Fresh content from the bypass cache |
| `render_stale` | Expired render cache served within stale TTL |
| `bypass_stale` | Expired bypass cache served within stale TTL |
| `status` | A configured status action (3xx/4xx/5xx rule) produced the response |

### EC-Cache-Age

Time in seconds since content was cached.

Only present when serving from cache (`EC-Source: render_cache`, `bypass_cache`,
`render_stale`, or `bypass_stale`).

### EC-Matched-Rule

ID of the URL pattern rule that matched the request.

Only present when the request matched a configured URL rule with an ID.

## Troubleshooting with headers

Use these headers to diagnose issues without accessing logs.

### Verify content source

Check `EC-Source` to confirm whether content came from cache or was freshly rendered:

```bash
curl -I -H "X-Render-Key: your-key" https://edge.example.com/page
```

### Check cache freshness

Read `EC-Source` together with `EC-Cache-Age` to understand cache state:

- `EC-Source: render_cache` (or `bypass_cache`) with low `EC-Cache-Age` = fresh cache
- `EC-Source: render_stale` (or `bypass_stale`) = expired but served within stale TTL
- `EC-Source: render` = cache miss, freshly rendered

### Trace requests

Use `EC-Request-ID` to correlate logs across Edge Gateway and Render Service:

```bash
curl -H "X-Render-Key: your-key" \
     -H "EC-Request-ID: my-trace-123" \
     https://edge.example.com/page
```

### Identify matched rules

Check `EC-Matched-Rule` to verify which URL pattern configuration applied to the request.

## Internal headers

Headers used internally between services. Not typically relevant for client integration.

### X-Edge-Render

Added to all EdgeComet-originated origin fetches: Render Service Chrome requests and Edge Gateway bypass fetches (including bypass pre-cache/recache). Used to prevent loops when the integration routes crawler traffic to Edge Gateway.

| Property | Value |
|----------|-------|
| Set by | Render Service and Edge Gateway (bypass fetches) |
| Value | Render Service ID (e.g., `rs-1`) for renders; `edge-gateway` for bypass fetches |
| Purpose | Loop prevention |

When the integration detects this header, it must skip crawler routing and forward the request directly to origin. Without this, a bypass fetch loops back into the Edge Gateway and is served from the stale bypass cache, so a bypass recache never refreshes. See [nginx integration](/integrations/nginx#loop-prevention) for configuration details.

### X-Internal-Auth

Authentication header for internal API endpoints between Edge Gateway instances and Cache Daemon.

| Property | Value |
|----------|-------|
| Required | Yes (for internal APIs) |
| Value | `cache_sharding.internal_auth_key` from config |

Used by Edge Gateway-to-Edge Gateway communication (cache pull/push/status) and Cache Daemon-to-Edge Gateway communication (recache).
