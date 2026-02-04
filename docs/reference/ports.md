---
title: Port reference
description: Default network ports used by EdgeComet services
---

# Port reference

## Port allocation scheme

EdgeComet uses the 10xxx port range, grouped by service:
- 1007x: Edge Gateway
- 1008x: Render Service
- 1009x: Cache Daemon

Metrics ports always end in 9.

## Edge Gateway

| Port | Config Key | Description |
|------|------------|-------------|
| 10070 | `server.listen` | Main HTTP server for client requests |
| 10071 | `internal.listen` | Internal API for inter-EG and daemon communication |
| 10079 | `metrics.listen` | Prometheus metrics endpoint |

## Render Service

| Port | Config Key | Description |
|------|------------|-------------|
| 10080 | `server.listen` | Render API for Edge Gateway requests |
| 10089 | `metrics.listen` | Prometheus metrics endpoint |

## Cache Daemon

| Port | Config Key | Description |
|------|------------|-------------|
| 10090 | `http_api.listen` | HTTP API for cache operations |
| 10099 | `metrics.listen` | Prometheus metrics endpoint |

## External dependencies

| Port | Service | Description |
|------|---------|-------------|
| 6379 | Redis | Cache metadata and coordination (standard Redis port) |

## Port selection rationale

- 10xxx range avoids conflicts with common development tools (8080, 9000, etc.)
- Each service gets a 10-port block for future expansion
- Metrics ports ending in 9 are easy to remember and firewall

## Configuration

All ports are configurable via the `listen` keys. These keys accept a bind address string (e.g., `":10070"`, `"0.0.0.0:10070"`, or `"127.0.0.1:10070"`).

::: code-group
```yaml [Global - edge-gateway.yaml]
server:
  listen: ":10070"

internal:
  listen: "192.168.1.2:10071"

metrics:
  enabled: true
  listen: ":10079"
```

```yaml [Global - render-service.yaml]
server:
  listen: "0.0.0.0:10080"

metrics:
  enabled: true
  listen: ":10089"
```

```yaml [Global - cache-daemon.yaml]
http_api:
  listen: ":10090"

metrics:
  enabled: true
  listen: ":10099"
```
:::

## Security considerations

The metrics ports (ending in 9) expose internal application metrics and should never be publicly accessible.

Configure your firewall or network policies to:
1. Allow public access to the Edge Gateway main port (10070)
2. Restrict all internal API ports (10071, 10080, 10090) to the private network
3. Restrict all metrics ports (10079, 10089, 10099) to your monitoring system IPs only