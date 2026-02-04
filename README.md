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

### Installation

Clone the repository and build the binaries:

```bash
git clone https://github.com/EdgeComet/edgecomet
cd edgecomet

# Build all services
go build -o bin/edge-gateway ./cmd/edge-gateway
go build -o bin/render-service ./cmd/render-service
go build -o bin/cache-daemon ./cmd/cache-daemon
```

### Basic configuration

Use the example configurations as starting points:

```bash
# Copy example configs
cp configs/sample/edge-gateway.yaml configs/my_edge-gateway.yaml
cp configs/sample/render-service.yaml configs/my_render-service.yaml
```

Minimal Edge Gateway configuration:

```yaml
server:
  listen: ":10070"
  timeout: 120s

redis:
  addr: "localhost:6379"

storage:
  base_path: "/var/cache/edgecomet"

render:
  dimensions:
    mobile:
      id: 1
      width: 375
      height: 812
      render_ua: "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36"
      match_ua:
        - $SearchBots
        - $AIBots

bypass:
  user_agent: "Mozilla/5.0 (compatible; EdgeComet/1.0)"

hosts:
  include: "hosts.d/"
```

Create a host configuration in `configs/hosts.d/`:

```yaml
# configs/hosts.d/example.com.yaml
hosts:
  - id: 1
    domain: "example.com"
    render_key: "your-secret-render-key-here"
    enabled: true

    render:
      timeout: 30s
      cache:
        ttl: 24h
      events:
        wait_for: "networkIdle"
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0 (compatible; Googlebot/2.1)"
          match_ua:
            - "*Googlebot*"
            - "*Bingbot*"
```

### Running the services

Start the Render Service (registers in Redis):

```bash
./bin/render-service -c configs/my_render-service.yaml
```

Start the Edge Gateway:

```bash
./bin/edge-gateway -c configs/my_edge-gateway.yaml
```

### Test your setup

Send a request to the render endpoint:

```bash
curl -H "X-Render-Key: your-secret-render-key-here" \
     -H "User-Agent: Mozilla/5.0 (compatible; Googlebot/2.1)" \
     "http://localhost:10070/render?url=https://example.com/your-page"
```

Check the response headers to verify rendering:

- `X-Render-Source: rendered` - Freshly rendered by Chrome
- `X-Render-Source: cache` - Served from cache
- `X-Request-ID` - Request tracing identifier

For detailed configuration options, see the [configuration documentation](#documentation).

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

EdgeComet uses a three-level configuration hierarchy that merges at request time:

1. **Global**: Base settings in `edge-gateway.yaml`
2. **Host**: Per-domain settings in `hosts.d/`
3. **URL Pattern**: Per-route rules within host configuration

### URL pattern matching

You can use three types of patterns:

- **Exact**: `/api/users` matches only that exact path
- **Wildcard**: `/blog/*` matches all paths under `/blog` recursively
- **Regexp**: `~/api/v[0-9]+/.*` for pattern-based matching

Query parameter matching is supported with the `match_query` field:

```yaml
- match: "/search"
  match_query:
    q: "*"              # Must have non-empty q parameter
    category: ["tech", "science"]  # Must match one of these values
  action: "render"
```

Rules are automatically sorted by specificity at startup, ensuring predictable matching behavior.

### Configuration validation

Test your configuration before deployment:

```bash
./bin/edge-gateway -c config.yaml -t https://example.com/test-page
```

## Testing

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

- [Edge Gateway overview](https://edgecomet.com/docs/edge-gateway/overview) - Request flow and architecture
- [Edge Gateway configuration](https://edgecomet.com/docs/edge-gateway/configuration) - Complete configuration reference
- [Render Service configuration](https://edgecomet.com/docs/render-service/configuration) - Chrome pool and rendering settings
- [Cache Daemon configuration](https://edgecomet.com/docs/cache-daemon/config-reference) - Automatic recaching configuration

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
