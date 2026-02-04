# Load Testing Tool for EdgeComet Edge Gateway

Command-line load testing tool designed to generate realistic HTTP traffic for testing the Edge Gateway under various load conditions.

## Features

- CSV-based URL input with optional status validation
- Dynamic concurrency with ±30% random variation
- Random User-Agent selection (desktop, mobile, tablet)
- Comprehensive per-host metrics tracking
- Multiple gateway support for load balancer testing
- Graceful shutdown with detailed final report

## Installation

```bash
go build -o ../../bin/loadtest .
```

## Usage

### Basic Command

```bash
./bin/loadtest \
  -urls=urls.csv \
  -gateway=http://localhost:10070 \
  -key=sk_live_abc123xyz789 \
  -concurrency=50
```

### Parameters

#### Required Parameters

- `-urls` - Path to CSV file containing URLs
- `-gateway` - Edge Gateway base URL(s), comma-separated
- `-key` - X-Render-Key for authentication
- `-concurrency` - Base number of simultaneous requests

#### Optional Parameters

- `-duration` - Test duration limit (e.g., `5m`, `1h`) (default: unlimited)
- `-timeout` - HTTP request timeout (default: `60s`)

## CSV Format

The CSV file must have a header row and two columns:

```csv
url,expected_status
https://example.com/page1,200
https://example.com/page2,404
https://another.com/article,
```

- **url** (required): Full URL including protocol
- **expected_status** (optional): Expected HTTP status code (empty = no validation)

## Examples

### Timed Test

```bash
./bin/loadtest \
  -urls=urls.csv \
  -gateway=http://localhost:10070 \
  -key=sk_live_abc123xyz789 \
  -concurrency=100 \
  -duration=10m \
  -timeout=30s
```

### Multiple Gateways

```bash
./bin/loadtest \
  -urls=urls.csv \
  -gateway=http://eg1:10070,http://eg2:10070,http://eg3:10070 \
  -key=sk_live_abc123xyz789 \
  -concurrency=200 \
  -duration=1h
```

### High Concurrency Stress Test

```bash
./bin/loadtest \
  -urls=urls.csv \
  -gateway=http://staging-eg:10070 \
  -key=sk_test_staging_key \
  -concurrency=500 \
  -duration=5m
```

## Output

### Real-Time Display

During the test, statistics are displayed and updated every 1 second, showing:

- Elapsed time and current status
- Total requests and current RPS (requests per second)
- Active concurrent requests vs base concurrency
- Response time percentiles (min, p50, p95, p99, max)
- Status code distribution with counts and percentages
- Network error breakdown (timeouts, connection errors)
- Render source distribution (cache, rendered, bypass)
- Bandwidth total and current rate
- Expected status mismatches

The display automatically refreshes using ANSI codes to show live progress.

Example:

```
================================================================================
[00:02:15] Load Test - Running

CURRENT METRICS
--------------------------------------------------------------------------------
Total Requests: 5,234 | Current RPS: 42.5 | Active Concurrent: 87/100 (base)

Response Time (ms):  min=45    p50=245   p95=1,203   p99=3,421   max=8,234

Status Codes:        2xx: 95.2%   4xx: 3.1%   5xx: 0.8%   Net Err: 0.9%
                     (4,982)    (162)      (42)        (48)

Network Errors:      Timeout: 18   Connection: 30

Render Sources:      cache: 82.3%   rendered: 15.2%   bypass: 2.5%
                     (4,307)      (796)          (131)

Bandwidth:           Total: 523.4 MB   Rate: 10.2 MB/s

Status Mismatches:   12 requests (0.2%) - expected vs actual mismatch

================================================================================
```

### Final Report

The tool generates a comprehensive final report upon completion that includes:

- Test duration and timestamp information
- Total requests and success/failure rates
- Status code distribution (2xx, 4xx, 5xx, network errors)
- Response time statistics (min, p50, p95, p99, max)
- Throughput metrics (RPS, bandwidth)
- Render source distribution (cache, rendered, bypass)
- Per-host breakdown with detailed metrics

### Example Report

```
================================================================================
LOAD TEST FINAL REPORT
================================================================================
Test Duration:       5m 23s
Test Started:        2025-10-31 14:23:45
Test Ended:          2025-10-31 14:29:08
Total Requests:      12,456
Successful:          11,859 (95.2%)
Failed:              597 (4.8%)

================================================================================
GLOBAL STATISTICS
================================================================================

Status Code Distribution
--------------------------------------------------------------------------------
2xx Success:         11,859 requests (95.2%)
4xx Client Error:    386 requests (3.1%)
5xx Server Error:    99 requests (0.8%)
Network Errors:      112 requests (0.9%)
  - Timeout:         45 (0.4%)
  - Connection:      67 (0.5%)

Response Time Statistics (milliseconds)
--------------------------------------------------------------------------------
Minimum:             42 ms
50th Percentile:     256 ms
95th Percentile:     1,423 ms
99th Percentile:     3,812 ms
Maximum:             12,543 ms

Throughput
--------------------------------------------------------------------------------
Average RPS:         38.6 requests/second
Total Bandwidth:     1.2 GB
Average Bandwidth:   3.8 MB/s

Render Source Distribution
--------------------------------------------------------------------------------
cache:               10,234 requests (82.2%)
rendered:            1,895 requests (15.2%)
bypass:              327 requests (2.6%)
bypass_cache:        0 requests (0.0%)
```

## Architecture

### Components

- **main.go** - CLI parsing, signal handling, lifecycle coordination, shared HTTP client
- **loader.go** - CSV loading and validation
- **requester.go** - HTTP request execution and response handling
- **stats.go** - Thread-safe metrics collection with percentiles
- **reporter.go** - Final report generation
- **useragents.go** - Random User-Agent selection

### Concurrency Model

- Continuous request spawner with 10ms control loop
- Dynamic concurrency with ±30% random variation
- Atomic counters for thread-safe metrics tracking
- Context-based graceful shutdown with proper request completion wait
- 5-second timeout for in-flight requests during shutdown
- All active requests complete before final report (no lost statistics)

### HTTP Client Architecture

- **Shared HTTP client** across all requests (not per-request)
- **Connection pooling** enabled with configurable pool size
- **Connection reuse** eliminates redundant TCP/TLS handshakes
- Pool size: `baseConcurrency * 2` (allows for bursts)
- Idle connection timeout: 90 seconds
- Performance: 5-10x faster than per-request client creation

**Why this matters:** Real clients (browsers, apps) maintain persistent connections. This architecture simulates realistic traffic patterns and prevents the load testing client from becoming a bottleneck. Without connection reuse, every request pays the TCP handshake penalty (~10-50ms) and TLS handshake penalty (~20-100ms for HTTPS), artificially lowering RPS measurements.

## Metrics Tracked

### Global Metrics

- Total requests, RPS, active concurrent requests
- Response times (min, max, p50, p95, p99)
- Status code distribution (2xx, 4xx, 5xx, network errors)
- Render source distribution (cache, rendered, bypass, bypass_cache)
- Bandwidth (total bytes, rate)
- Expected status mismatches

### Per-Host Metrics

All global metrics broken down by target URL host.

## Graceful Shutdown

When the test completes (duration limit or Ctrl+C), the tool:

1. Stops spawning new requests
2. Waits up to 5 seconds for all active requests to complete
3. Prints status updates for in-flight requests
4. Generates final report with complete statistics

This ensures no requests are lost and all statistics are accurate.

## Troubleshooting

### Issue: Low RPS despite high concurrency

**Cause**: Edge Gateway overwhelmed or render services at capacity

**Solution**: Check EG and RS health, verify available Chrome tabs

### Issue: High timeout rate

**Cause**: Timeout too short for render operations

**Solution**: Increase `-timeout` (e.g., 90s or 120s)

### Issue: Connection refused errors

**Cause**: Edge Gateway not running or incorrect URL

**Solution**: Verify gateway is accessible with curl

## Dependencies

- `github.com/google/uuid` - Request ID generation
- `github.com/HdrHistogram/hdrhistogram-go` - Percentile calculations

## License

Internal tool for EdgeComet testing.
