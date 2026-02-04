---
title: Metrics reference
description: Prometheus metrics exposed by EdgeComet services
---

# Metrics reference

All services expose Prometheus metrics on a dedicated port configured via `metrics.listen`.

## Edge Gateway metrics

### Request metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_requests_total` | counter | `host`, `status`, `source` | Total requests processed |
| `eg_request_duration_seconds` | histogram | `host`, `source` | Request processing time |

### Cache metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_cache_hits_total` | counter | `host`, `dimension` | Cache hit count |
| `eg_cache_misses_total` | counter | `host`, `dimension` | Cache miss count |
| `eg_cache_stale_total` | counter | `host`, `dimension` | Stale cache served count |

### Render metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_render_requests_total` | counter | `host`, `status` | Render requests sent |
| `eg_render_duration_seconds` | histogram | `host` | Render request duration |
| `eg_render_errors_total` | counter | `host`, `error_type` | Render errors |

### Bypass metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_bypass_requests_total` | counter | `host`, `status` | Bypass requests |
| `eg_bypass_cache_hits_total` | counter | `host` | Bypass cache hits |

## Render Service metrics

### Chrome pool metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rs_chrome_pool_size` | gauge | | Configured pool size |
| `rs_chrome_pool_active` | gauge | | Currently active instances |
| `rs_chrome_pool_available` | gauge | | Available instances |

### Render metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rs_render_total` | counter | `status` | Total renders |
| `rs_render_duration_seconds` | histogram | | Render duration |
| `rs_render_errors_total` | counter | `error_type` | Render errors |

### Instance lifecycle

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `rs_chrome_restarts_total` | counter | `reason` | Chrome instance restarts |
| `rs_chrome_requests_per_instance` | histogram | | Requests before restart |

## Cache Daemon metrics

### Queue metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `cd_queue_size` | gauge | `host`, `priority` | Queue size by priority |
| `cd_queue_due_now` | gauge | `host`, `priority` | Entries ready to process |

### Processing metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `cd_recache_total` | counter | `host`, `status` | Recache operations |
| `cd_recache_duration_seconds` | histogram | `host` | Recache duration |

## Grafana dashboard queries

### Request rate
```promql
rate(eg_requests_total[5m])
```

### Cache hit ratio
```promql
sum(rate(eg_cache_hits_total[5m])) / sum(rate(eg_cache_hits_total[5m]) + rate(eg_cache_misses_total[5m]))
```

### Chrome pool utilization
```promql
rs_chrome_pool_active / rs_chrome_pool_size
```

### P99 render latency
```promql
histogram_quantile(0.99, rate(rs_render_duration_seconds_bucket[5m]))
```

## Alert examples

### High error rate
```yaml
alert: HighRenderErrorRate
expr: rate(eg_render_errors_total[5m]) / rate(eg_render_requests_total[5m]) > 0.05
for: 5m
```

### Chrome pool exhaustion
```yaml
alert: ChromePoolExhausted
expr: rs_chrome_pool_available == 0
for: 1m
```

### Low cache hit rate
```yaml
alert: LowCacheHitRate
expr: sum(rate(eg_cache_hits_total[5m])) / sum(rate(eg_cache_hits_total[5m]) + rate(eg_cache_misses_total[5m])) < 0.5
for: 10m
```
