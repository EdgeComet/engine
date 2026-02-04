---
title: Troubleshooting overview
description: How to diagnose and resolve common issues in EdgeComet
---

# Troubleshooting overview

## Introduction

Typical issues fall into two categories, each with different debugging approaches:

- **General processing issues** - pages not reaching rendering, cache timing problems, recache failures
- **JavaScript rendering issues** - pages not rendering properly, missing content, JavaScript errors

## Before you start

- Check that all three services are running
- For cluster installations, verify all instances can communicate with each other
- Verify Redis connectivity and memory limits
- Confirm Chrome is installed and accessible

## Firewalls

Ensure no firewall rules block rendering requests. It’s a very common issue with rendering.

Enterprise websites often have multilayered firewall systems where EdgeComet requests pass CDN-level checks but encounter application-level restrictions, such as IP rate limits or time-based rules.

If you use the Googlebot user agent for rendering (configured via `render_ua` in dimension settings), requests may be blocked as fake bot traffic even after whitelisting IPs.

## Enable debug logging

Debug logging is verbose but provides the complete request processing log. Log configuration supports different levels for console and file output. Set the debug level for file logging to capture detailed information without flooding your console:

::: code-group
```yaml [Global - edge-gateway.yaml]
log:
  level: info
  file:
    enabled: true
    level: debug
    path: /var/log/edgecomet/edge-gateway.log
```
```yaml [Global - render-service.yaml]
log:
  level: info
  file:
    enabled: true
    level: debug
    path: /var/log/edgecomet/render-service.log
```
:::

## Request tracing

Each request has a request ID that appears in both Edge Gateway and Render Service logs. Use grep with this ID to trace the full processing flow:

```bash
grep "abc123-request-id" /var/log/edgecomet/*.log
```

To identify your test requests, add a custom `X-Request-ID` header:

```bash
curl -H "X-Request-ID: my-test-001" https://example.com/page
```

## Key metrics to monitor

Prometheus metrics provide visibility into system behavior. A properly configured system typically runs without issues, but when problems occur, the metrics dashboard is the best starting point for investigation. Common triggers include traffic spikes, parsing or scraping activity, and cache misconfiguration.

- `render_requests_total` - request volume
- `render_duration_seconds` - rendering time
- `cache_hits_total` / `cache_misses_total` - cache effectiveness
- `chrome_pool_active` - pool utilization

## Which logs to check

| Symptom | Check first |
|---------|-------------|
| 403/401 errors | Edge Gateway |
| Timeout errors | Render Service |
| Cache misses | Edge Gateway |
| Blank pages | Render Service |
| Service unavailable | Both services |

## Common issues

- [Rendering failures](./rendering-failures.md)
- [Cache issues](./cache-issues.md)
- [Chrome pool problems](./chrome-pool.md)

## Log reference

- [Understanding log messages](./log-reference.md)
