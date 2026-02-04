# Quick Start

Get EdgeComet running locally and make your first render request in under 10 minutes.

## Prerequisites

### Required Software
- **Go 1.21+**: For building services

- **Redis 6.0+**: For coordination and caching

- **Chrome/Chromium**: For rendering

### System Requirements
- 4+ core CPU and 8-16GB of RAM to run 10 rendering threads
- macOS or Linux (Linux recommended for production)

## Step 1: Build Services

### Clone Repository
```bash
git clone <repository-url>
cd edgecomet
```

### Build All Services
```bash
# Build Edge Gateway
go build -o bin/edge-gateway ./cmd/edge-gateway

# Build Render Service
go build -o bin/render-service ./cmd/render-service
```

### Verify Builds
```bash
./bin/edge-gateway -h
./bin/render-service -h
```

You should see usage information for each service.

## Step 2: Configure Services

### Create Configuration Directory
```bash
mkdir -p configs/quickstart/hosts.d
```

### Edge Gateway Configuration

Create `configs/quickstart/edge-gateway.yaml`:
```yaml
eg_id: "eg-1"

server:
  listen: ":10070"
  timeout: 120s

internal:
  listen: "localhost:10071"
  auth_key: "your-secret-key"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

storage:
  base_path: "/tmp/render-cache"

log:
  level: "info"
  console:
    enabled: true
    format: "console"

metrics:
  enabled: true
  listen: ":10079"

hosts:
  include: "configs/quickstart/hosts.d/"
```

Key parameters:
- `eg_id`: Unique identifier for this Edge Gateway instance, could be an IP
- `server.listen`: Main HTTP server address (`:10070`)
- `metrics.listen`: Separate port for Prometheus metrics (`:10079`)
- `storage.base_path`: Directory for storing rendered HTML files
- `render.cache.ttl`: How long to cache rendered content (1 hour)
- `hosts.include`: Directory containing host configurations

### Host Configuration

Create `configs/quickstart/hosts.d/example.yaml`:
```yaml
hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key-123"
    enabled: true

    render:
      timeout: 30s
      unmatched_dimension: "desktop"
      cache:
        ttl: 1h

      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
          match_ua: ["Googlebot", "bingbot", "Slurp"]

      events:
        wait_for: "networkIdle"
        additional_wait: 1s

```

Key parameters:
- `id`: Unique host identifier (used in cache keys)
- `domain`: The domain this configuration applies to
- `render_key`: Authentication key for API requests (use `X-Render-Key` header)
- `dimensions.desktop.id`: Unique dimension identifier (must be stable)
- `match_ua`: User-Agent patterns that trigger rendering for this dimension

### Render Service Configuration

Create `configs/quickstart/render-service.yaml`:
```yaml
server:
  id: "rs-1"
  listen: "0.0.0.0:10080"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

chrome:
  pool_size: 5

  render:
    max_timeout: 50s

log:
  level: "info"
  console:
    enabled: true

metrics:
  enabled: true
  listen: ":10089"
```

Key parameters:
- `server.id`: Unique identifier for this Render Service instance
- `server.port`: Main HTTP server port (10080)
- `metrics.port`: Separate port for Prometheus metrics (10089)
- `chrome.pool_size`: Number of Chrome instances in the pool (5 for quickstart)

## Step 3: Start Services

### Verify Redis is Running

### Start Services

**Terminal 1** - Render Service (start first):
```bash
./bin/render-service -c configs/quickstart/render-service.yaml
```

**Terminal 2** - Edge Gateway (start second):
```bash
./bin/edge-gateway -c configs/quickstart/edge-gateway.yaml
```

Wait for these log messages:
- `"Loaded 1 hosts"`
- `"Edge Gateway started on :10070"`

## Step 4: Make Your First Render Request

### Test with External URL

Request a render of example.com:
```bash
curl -v \
  -H "X-Render-Key: test-key-123" \
  "http://localhost:10070/render?url=https://example.com/"
```

### Verify Response Headers

The first request should return these headers:
- `X-Request-ID`: Request tracing identifier (auto-generated)
- `X-Render-Source: rendered`: Content was freshly rendered by Chrome
- `X-Render-Service: rs-1`: Which Render Service handled the request

### Make Second Request (Cache Hit)

Request the same URL again:
```bash
curl -v \
  -H "X-Render-Key: test-key-123" \
  "http://localhost:10070/render?url=https://example.com/"
```

The second request should return these headers:
- `X-Render-Source: cache`: Served from render cache
- `X-Render-Cache: hit`: Cache hit
- `X-Cache-Age: <duration>`: How long content has been cached (e.g., "5s", "2m30s")

The response should be instant (no Chrome rendering, served from filesystem cache).

## Step 5: Test Different Scenarios

### Test Bot User-Agent Detection

Make a request with a bot User-Agent:
```bash
curl -v \
  -H "X-Render-Key: test-key-123" \
  -H "User-Agent: Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)" \
  "http://localhost:10070/render?url=https://example.com/"
```

Should trigger rendering because User-Agent matches `match_ua` pattern.

### Test Regular User-Agent (Should Use Cache)

Make a request with a regular browser User-Agent:
```bash
curl -v \
  -H "X-Render-Key: test-key-123" \
  -H "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36" \
  "http://localhost:10070/render?url=https://example.com/"
```

Should serve from cache if content was previously rendered.

### Test Invalid Authentication

Try a request with wrong API key:
```bash
curl -v \
  -H "X-Render-Key: wrong-key" \
  "http://localhost:10070/render?url=https://example.com/"
```

Should return `403 Forbidden` with error message.

### Test Bypass Mode

Add a bypass rule to `configs/quickstart/hosts.d/example.yaml`:
```yaml
url_rules:
  - match: "*.png"
    action: bypass
  - match: "*.jpg"
    action: bypass
  - match: "*"
    action: render
```

Restart Edge Gateway (Ctrl+C, then restart), then test:
```bash
curl -v \
  -H "X-Render-Key: test-key-123" \
  "http://localhost:10070/render?url=https://example.com/image.png"
```

Should return header: `X-Render-Source: bypass` (direct fetch, no rendering)

## Step 6: Verify System Health

### Check Metrics Endpoints

Edge Gateway metrics:
```bash
curl http://localhost:10079/metrics
```

Render Service metrics:
```bash
curl http://localhost:10089/metrics
```

Both should return Prometheus-format metrics.

### Monitor Logs

Watch Edge Gateway logs for request patterns:
- Cache hits: `"Cache hit"` messages
- Renders: `"Rendering requested"` messages
- Bypasses: `"Bypass mode"` messages

Watch Render Service logs for Chrome activity:
- Pool operations: `"Chrome instance acquired"`, `"Chrome instance released"`
- Render completions: `"Render completed"` with timing information

### Test Cache Expiration

Wait for TTL to expire (1 hour with default config) or change `render.cache.ttl` to shorter duration (e.g., `30s`) for testing.

After TTL expires, the next request should show:
- `X-Render-Source: rendered` (fresh render)
- No `X-Cache-Age` header

## Troubleshooting

### Render Not Working

#### "No render services available"
- Check Render Service is running and logs show "Registered in service registry"
- Verify Redis registry: `redis-cli KEYS "service:registry:*"`
- Check Render Service heartbeat logs (should appear every 10s)
- Ensure Render Service started before Edge Gateway

#### "Render timeout"
- Check the target URL is accessible from the server
- Increase `render.timeout` in host config (default: 30s)
- Check Render Service logs for specific errors
- Verify `render.max_timeout` in Render Service config is higher than host timeout

#### "401 Unauthorized" or "403 Forbidden"
- Verify `X-Render-Key` header matches `render_key` in host configuration
- Check host domain matches requested URL
- Ensure host is `enabled: true` in configuration

### Cache Issues

#### "Cache not working" (Always X-Render-Source: rendered)
- Check `storage.base_path` directory exists and is writable
- Verify sufficient disk space (check `cache.max_size`)
- Review Edge Gateway logs for cache write errors
- Ensure `render.cache.ttl` is set (default: 1h)

#### "Stale content served"
- Cache TTL may be too long, adjust `render.cache.ttl` in host config
- Clear cache manually (development only):
  ```bash
  redis-cli FLUSHDB
  rm -rf cache/*
  ```
- Check cache timestamp in `X-Cache-Age` header

#### "Permission denied" writing cache files
- Check filesystem permissions on `storage.base_path` directory
- Ensure the user running Edge Gateway has write permissions:
  ```bash
  chmod 755 cache/
  ```

