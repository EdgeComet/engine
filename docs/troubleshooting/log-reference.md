---
title: Log reference
description: Understanding EdgeComet log messages
---

# Log reference

## Log levels

| Level | When to use |
|-------|-------------|
| `error` | Production - errors only |
| `warn` | Production - errors and warnings |
| `info` | Production - normal operation |
| `debug` | Development/troubleshooting |

## Edge Gateway log messages

### Startup
- `Edge Gateway started on :10070` - successful start
- `Loaded N hosts` - host configurations loaded

### Request handling
- `Request received` - incoming request
- `Cache hit` - served from cache
- `Cache miss` - triggering render
- `Bypass mode` - direct fetch, no render

### Errors
- `Authentication failed` - invalid or missing X-Render-Key
- `Host not found` - no matching host configuration
- `No render services available` - registry empty
- `Render timeout` - render exceeded timeout

## Render Service log messages

### Startup
- `Render Service started` - successful start
- `Registered in service registry` - ready to accept requests
- `Chrome pool initialized` - pool ready

### Chrome pool
- `Chrome instance acquired` - render starting
- `Chrome instance released` - render complete
- `Chrome instance restarting` - lifecycle restart

### Rendering
- `Render started` - beginning render
- `Render completed` - successful render with timing
- `Render failed` - error during render

## Cache Daemon log messages

### Scheduler
- `Recache job started` - batch processing begun
- `Recache completed` - batch finished
- `URL queued for recache` - individual URL scheduled

## Error codes

| Code | Meaning |
|------|---------|
| `ERR_AUTH_FAILED` | Authentication failure |
| `ERR_HOST_NOT_FOUND` | Unknown host |
| `ERR_RENDER_TIMEOUT` | Render exceeded timeout |
| `ERR_CHROME_UNAVAILABLE` | No Chrome instances available |
| `ERR_CACHE_WRITE` | Failed to write cache |

## Example log analysis

### Debugging a slow render

```
# 1. Find the request
grep "request_id=abc123" edge-gateway.log

# 2. Check render service
grep "request_id=abc123" render-service.log

# 3. Look for timing info
grep "render_duration" render-service.log | grep "abc123"
```

### Finding cache problems

```
# Check cache hit rate
grep "Cache hit\|Cache miss" edge-gateway.log | tail -100

# Find specific URL issues
grep "url=example.com/page" edge-gateway.log
```
