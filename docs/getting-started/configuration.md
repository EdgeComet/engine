---
title: Quick Configuration basics
description: Essential configuration to get EdgeComet running
---

# Configuration basics

EdgeComet uses YAML configuration files for each service. This page covers the essentials to get started. For complete parameter references, see the linked documentation for each service.

## Configuration files

| Service | File | Purpose |
|---------|------|---------|
| Edge Gateway | `edge-gateway.yaml` | Receives requests, manages cache, routes to render services |
| Render Service | `render-service.yaml` | Manages Chrome instances, renders pages |
| Cache Daemon | `cache-daemon.yaml` | Maintains cache freshness (optional, for production) |

## Edge Gateway

Minimal configuration to start the Edge Gateway:

```yaml
eg_id: "eg-1"

server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"

storage:
  base_path: "/var/cache/edgecomet"

log:
  level: "info"
  file:
    enabled: true
    path: "/var/log/edgecomet/edge-gateway.log"
```

See [Edge Gateway configuration](../edge-gateway/configuration.md) for all parameters and [Sharding](../edge-gateway/sharding.md) for multi-gateway setups.

## Host configuration

Hosts define which domains EdgeComet handles. Place host files in the `hosts.d/` directory:

```
configs/
  edge-gateway.yaml
  hosts.d/
    example.com.yaml
    another-site.com.yaml
```

Minimal host configuration:

```yaml
hosts:
  - id: "example"
    domain: "example.com"
    render_key: "your-secret-key"
    render:
      cache:
        ttl: 24h
      events:
        wait_for: "networkIdle"
        additional_wait: 500ms
```

Required fields:
- `id` - Unique identifier for the host
- `domain` - Domain name to match
- `render_key` - API key for authentication (sent via `X-Render-Key` header)

See [Render mode](../edge-gateway/render-mode.md) for details on render events and timing.

## URL rules

URL rules control how specific paths are handled. Define them within a host configuration:

```yaml
hosts:
  - id: "example"
    domain: "example.com"
    render_key: "secret"
    url_rules:
      - match: "/api/*"
        action: "bypass"
      - match: "/admin/*"
        action: "status_403"
```

Common actions:
- `render` - Render with Chrome (default)
- `bypass` - Fetch directly from origin without rendering
- `status_403`, `status_404`, `status_410` - Return status codes

See [URL rules](../edge-gateway/url-rules.md) for pattern syntax and all available actions.

## Render Service

Minimal configuration for the Render Service:

```yaml
server:
  id: "rs-1"
  listen: ":10080"

redis:
  addr: "localhost:6379"

chrome:
  pool_size: 10

log:
  level: "info"
  file:
    enabled: true
    path: "/var/log/edgecomet/render-service.log"
```

Key parameters:
- `server.id` - Unique identifier, used for service discovery
- `chrome.pool_size` - Number of Chrome instances to maintain

See [Render Service configuration](../render-service/configuration.md) for all parameters.

## Testing configuration

Validate your configuration before starting the service:

```bash
./edge-gateway -c configs/edge-gateway.yaml -t
```

The `-t` flag checks for syntax errors and invalid values without starting the server.

## Next steps

- [Edge Gateway configuration](../edge-gateway/configuration.md) - Complete parameter reference
- [Render Service configuration](../render-service/configuration.md) - Chrome pool and render settings
- [URL rules](../edge-gateway/url-rules.md) - Pattern matching and actions
- [Cache Daemon configuration](../cache-daemon/config-reference.md) - Cache maintenance setup
