# EdgeComet

Server-side rendering and caching system for JavaScript websites. Serves pre-rendered HTML to search engine bots (Googlebot, Bingbot) and AI crawlers (GPTBot, ClaudeBot, PerplexityBot).

## Overview

EdgeComet acts as a middleware layer that makes JavaScript content crawlable. When a search engine or AI crawler visits your site, EdgeComet intercepts the request, serving a rendered HTML snapshot from cache or executing the JavaScript using a managed pool of headless Chrome instances.

This approach allows bots to see your content, metadata, and structured data as intended, without requiring changes to your frontend code. It works with React, Vue, Angular, or any JavaScript framework that renders client-side.

### Features

- **Caching**: Redis for distributed coordination and metadata, filesystem for HTML content. Cached pages served in milliseconds.
- **Rendering**: Managed Chrome instance pool with automatic lifecycle handling, error recovery, and distributed locking to prevent duplicate renders.
- **Configuration**: Rules defined globally, per-host, or per-URL pattern using exact matches, wildcards, or regex. Query parameter matching supported.
- **Dimensions**: Separate cache entries for desktop, mobile, and tablet user agents.
- **Monitoring**: Prometheus metrics, structured logging (Zap), stale-while-revalidate support.

### Use cases

**JavaScript rendering**: Bots receive fully rendered HTML while users get the client-side app. First render takes 2-5s, cached responses under 10ms. Supports device-specific dimensions (desktop, mobile, tablet).

**Crawl budget optimization**: Bypass mode caches origin responses without rendering. Reduces origin load and speeds up bot crawls for pages that don't need JS execution.

**AI crawler support**: GPTBot, ClaudeBot, PerplexityBot receive complete HTML with structured data. Works the same as search engine bot rendering.

## Quick start

### Prerequisites

- Go 1.24.2 or higher
- Chrome or Chromium browser (headless mode)
- Redis 6.0 or higher

### Build

```bash
git clone https://github.com/EdgeComet/edgecomet
cd edgecomet

go build -o bin/edge-gateway ./cmd/edge-gateway
go build -o bin/render-service ./cmd/render-service
go build -o bin/cache-daemon ./cmd/cache-daemon
```

### Run

Copy the sample configurations and edit them for your domain:

```bash
cp configs/sample/edge-gateway.yaml configs/my_edge-gateway.yaml
cp configs/sample/render-service.yaml configs/my_render-service.yaml
```

Start the services:

```bash
./bin/render-service -c configs/my_render-service.yaml
./bin/edge-gateway -c configs/my_edge-gateway.yaml
```

### Test

```bash
curl -H "X-Render-Key: your-render-key" \
     -H "User-Agent: Mozilla/5.0 (compatible; Googlebot/2.1)" \
     "http://localhost:10070/render?url=https://example.com/your-page"
```

- `X-Render-Source: rendered` - Freshly rendered by Chrome
- `X-Render-Source: cache` - Served from cache

For a step-by-step walkthrough, see the [Quick Start guide](https://edgecomet.com/docs/quick-start.html).

## Architecture

EdgeComet uses a multi-service architecture with clear separation of concerns:

```
Client Request
    ↓
Edge Gateway
    ↓
├─→ Cache Check (Redis + Filesystem)
│   └─→ Cache Hit → Return HTML
│
└─→ Cache Miss
    ↓
    Render Service (Chrome Pool)
    ↓
    ├─→ Acquire Chrome Instance
    ├─→ Execute JavaScript
    ├─→ Capture Rendered HTML
    └─→ Store in Cache
    ↓
Return HTML
```

### Components

**Edge Gateway**

Handles incoming requests, manages cache lookups, and coordinates with render services. Uses FastHTTP for high performance and supports bot detection, authentication, and flexible URL pattern matching.

**Render Service**

Manages a pool of headless Chrome instances, renders JavaScript pages, and registers availability in Redis. Implements automatic restart policies to prevent memory leaks and handles graceful shutdown.

**Cache Daemon** (optional)

Monitors bot traffic and automatically triggers recaching for frequently accessed pages. Schedules recaching based on bot traffic patterns to keep popular content fresh.

## High availability and sharding

EdgeComet supports multi-instance deployments with distributed cache sharing across Edge Gateway instances. Cache sharding reduces cache misses in horizontal scaling scenarios and provides redundancy for high availability.

When you deploy multiple Edge Gateway instances with sharding enabled, the system automatically distributes cache entries across instances using consistent hashing. You configure a replication factor to control how many instances store each cache entry. For example, with replication factor 2, every rendered page is stored on two different Edge Gateway instances.

The distributed architecture tolerates instance failures gracefully. If an Edge Gateway goes offline, requests automatically route to remaining instances that hold the cache replicas. When you add new instances to the cluster, they automatically discover existing instances via Redis and begin participating in cache distribution.

## Configuration

EdgeComet uses a three-level configuration hierarchy: global settings in `edge-gateway.yaml`, per-domain settings in `hosts.d/`, and per-route URL rules within each host. Settings merge at request time, with more specific levels overriding broader ones.

Sample configurations are in `configs/sample/`. For full details, see the [Edge Gateway configuration](https://edgecomet.com/docs/edge-gateway/configuration.html) reference.

## Testing

The codebase carries 62k lines of tests against 26k lines of source (~2.4x ratio). Unit tests cover individual packages, acceptance tests spin up full service stacks with embedded Redis and real Chrome instances to verify rendering, caching, sharding, and invalidation end-to-end.

All test commands run from the `tests/` directory:

```bash
cd tests
```

### Unit tests

- `make unit` - run tests for all internal packages
- `make unit-verbose` - run with verbose output and coverage report

### Acceptance tests

- `make test` - run basic acceptance tests (single EG-RS configuration)
- `make basic FOCUS="..."` - run specific test suites by name
- `make basic-verbose` - run with verbose output for debugging
- `make basic-single FILE=...` - run a single test file
- `make sharding` - run sharding tests (multi-EG distributed cache)
- `make recache` - run recache tests (automatic bot-triggered recaching)
- `make help` - view all available test commands

### Load testing

The load testing tool evaluates system performance under realistic traffic conditions. It requires a CSV file with URLs, sends concurrent requests to your Edge Gateway, and provides detailed metrics on response times, cache efficiency, and throughput.

Quick example:

```bash
cd tests/loadtest
go run main.go \
  -urls test.csv \
  -gateway http://localhost:10070 \
  -key your-api-key \
  -concurrency 10 \
  -duration 5m
```

## Documentation

- [Edge Gateway overview](https://edgecomet.com/docs/edge-gateway/overview.html) - Request flow and architecture
- [Edge Gateway configuration](https://edgecomet.com/docs/edge-gateway/configuration.html) - Complete configuration reference
- [Render Service configuration](https://edgecomet.com/docs/render-service/configuration.html) - Chrome pool and rendering settings
- [Cache Daemon configuration](https://edgecomet.com/docs/cache-daemon/config-reference.html) - Automatic recaching configuration

## Monitoring

### Metrics

EdgeComet exposes Prometheus metrics on a dedicated port. Configure in your service YAML:

```yaml
metrics:
  enabled: true
  listen: ":10079"  # Must differ from server.listen
```

Available metrics include:
- Request rates and response times
- Cache hit/miss ratios
- Chrome pool utilization
- Render success/failure rates
- Service registry status

### Logging

Structured logging with Zap provides detailed operational insights:

```json
{
  "level": "info",
  "ts": "2025-01-08T12:34:56.789Z",
  "msg": "Request rendered successfully",
  "request_id": "abc123",
  "url": "https://example.com/page",
  "render_time_ms": 1234,
  "cache_stored": true
}
```

Log levels: DEBUG, INFO, WARN, ERROR


## License

Apache-2.0
