---
title: Monitoring
description: How to observe Edge Gateway behavior and diagnose issues using Prometheus metrics
---

# Monitoring

Edge Gateway follows the open box monitoring principle, exposing detailed Prometheus metrics that provide visibility into request processing, cache behavior, render coordination, and system health. Use these metrics to understand system behavior and identify potential issues during operation.

## Configuration

Enable metrics by setting a port separate from the main server port:

::: code-group
```yaml [Global - edge-gateway.yaml]
server:
  listen: ":10070"

metrics:
  enabled: true
  listen: ":10079"
```
:::

The metrics endpoint is available at `http://localhost:10079/metrics`.

## Metrics reference

All metrics use the `eg_` prefix and include labels for filtering by host, dimension, or other attributes.

### Request metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_requests_total` | counter | `host`, `dimension`, `status` | Total number of render requests processed. |
| `eg_request_duration_seconds` | histogram | `host`, `dimension`, `status` | Time taken to process render requests. Buckets: 5ms to 10s. |
| `eg_active_requests` | gauge | — | Number of currently active render requests. |
| `eg_errors_total` | counter | `error_type`, `host` | Total number of errors by type. |

### Cache metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_cache_hits_total` | counter | `host`, `dimension` | Total number of cache hits. |
| `eg_cache_misses_total` | counter | `host`, `dimension` | Total number of cache misses. |
| `eg_cache_hit_ratio` | gauge | `host`, `dimension` | Cache hit ratio (0-1) for each host and dimension. |
| `eg_stale_cache_served_total` | counter | `host`, `dimension` | Total number of stale cache entries served when render fails. |
| `eg_cache_size_bytes` | gauge | — | Total size of cached content in bytes. |

### Render service metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_render_service_duration_seconds` | histogram | `host`, `dimension`, `service_id` | Time taken by render service to process requests. Buckets: 100ms to 30s. |
| `eg_status_code_responses_total` | counter | `host`, `dimension`, `status_range` | Total rendered responses by status code range (2xx, 3xx, 4xx, 5xx). |

### Bypass metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_bypass_total` | counter | `host`, `reason` | Total number of requests that used bypass mode instead of rendering. |

### Wait and coordination metrics

These metrics track requests waiting for concurrent renders to complete (thundering herd protection).

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_wait_total` | counter | `host`, `dimension`, `outcome` | Total requests that waited for concurrent renders. Outcome: `success` or `timeout`. |
| `eg_wait_duration_seconds` | histogram | `host`, `dimension`, `outcome` | Time spent waiting for concurrent renders. Buckets: 10ms to 5s. |
| `eg_wait_timeouts_total` | counter | `host`, `dimension` | Total wait timeouts while waiting for concurrent renders. |

### Sharding metrics

These metrics are available when cache sharding is enabled across multiple Edge Gateway instances.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_sharding_requests_total` | counter | `operation`, `status`, `target_eg_id` | Total inter-EG requests for cache operations. |
| `eg_sharding_request_duration_seconds` | histogram | `operation` | Duration of inter-EG requests. Buckets: 10ms to 5s. |
| `eg_sharding_bytes_transferred_total` | counter | `operation`, `direction` | Total bytes transferred in inter-EG communication. |
| `eg_sharding_cluster_size` | gauge | — | Number of healthy Edge Gateways in the cluster. |
| `eg_sharding_under_replicated_total` | counter | `host_id` | Cache entries created with fewer replicas than target. |
| `eg_sharding_errors_total` | counter | `error_type` | Inter-EG communication errors. |
| `eg_sharding_push_failures_total` | counter | `target_eg_id` | Failed push operations per target Edge Gateway. |
| `eg_sharding_local_cache_entries` | gauge | — | Number of cache entries stored locally on this instance. |

### Filesystem cleanup metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `eg_filesystem_cleanup_runs_total` | counter | `host_id`, `status` | Total cleanup runs per host. |
| `eg_filesystem_cleanup_directories_deleted_total` | counter | `host_id` | Total directories deleted during cleanup. |
| `eg_filesystem_cleanup_duration_seconds` | histogram | `host_id` | Duration of cleanup operations. Buckets: 100ms to 60s. |
| `eg_filesystem_cleanup_errors_total` | counter | `host_id`, `error_type` | Cleanup errors by type. |

## Example queries

### Cache performance

```txt
# Cache hit ratio over last 5 minutes
rate(eg_cache_hits_total[5m]) / (rate(eg_cache_hits_total[5m]) + rate(eg_cache_misses_total[5m]))

# Stale cache usage rate
rate(eg_stale_cache_served_total[5m])
```

### Request latency

```txt
# 95th percentile request duration
histogram_quantile(0.95, rate(eg_request_duration_seconds_bucket[5m]))

# Render service latency by service
histogram_quantile(0.95, rate(eg_render_service_duration_seconds_bucket[5m]))
```

### Error monitoring

```txt
# Error rate by type
rate(eg_errors_total[5m])

# Wait timeout rate
rate(eg_wait_timeouts_total[5m])
```

### Sharding health

```txt
# Cluster size over time
eg_sharding_cluster_size

# Under-replication issues
rate(eg_sharding_under_replicated_total[5m])
```
