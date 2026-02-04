# URL Tester Config Test Fixtures

Test fixtures for validating URL testing functionality in the configtest package.

## Purpose

These fixtures provide minimal configuration for testing the `configtest.TestURL()` function, which tests URL pattern matching, action resolution, config merging, and cache key generation.

## Structure

```
configtest-url-tester/
├── edge-gateway.yaml          # Main config with global render/bypass settings
└── hosts.d/
    ├── 01-example.yaml        # Comprehensive host with varied URL rules
    ├── 02-shop.yaml           # Custom dimensions with explicit IDs
    ├── 03-blog.yaml           # Minimal host (for relative URL tests)
    └── 04-some-eshop.yaml     # Minimal host (for relative URL tests)
```

## Host Configurations

### Host: example.com (id: 1)

Primary test host with comprehensive URL pattern coverage:

**Dimensions**:
- **desktop** (id: 1): 1920x1080, matches `*Googlebot*`, `*bingbot*`
- **mobile** (id: 2): 375x812, matches `*Googlebot-Mobile*`, `*iPhone*`

**URL Rules** (in priority order):
1. `/admin/*` → `status_403` (403 Forbidden)
2. `/account/*` → `bypass` (no cache)
3. `/` → `render` (15m cache TTL)
4. `/blog/*` → `render` (2h cache TTL)
5. `/static/*` → `bypass` with cache (24h TTL, status_codes: [200])

**Default Action**: `render` (1h cache TTL)

### Host: shop.example.com (id: 2)

E-commerce host demonstrating custom dimension IDs:

**Dimensions** (non-sequential IDs):
- **mobile** (id: 5): 375x812, matches `$GooglebotSearchMobile`, `$BingbotMobile`, `iPhone`
- **desktop** (id: 10): 1440x900, matches `$GooglebotSearchDesktop`, `$BingbotDesktop`
- **tablet** (id: 15): 768x1024, matches `iPad`, `tablet`

**Cache TTL**: 30m (vs 1h default)
**Unmatched Dimension**: `bypass` (skip rendering for unknown bots)

### Host: blog.example.com (id: 3)

Minimal host configuration for testing relative URL matching across multiple hosts.

**Default Action**: `render` (1h cache TTL)

### Host: some-eshop.com (id: 4)

Minimal host configuration for testing relative URL matching across multiple hosts.

**Default Action**: `render` (1h cache TTL)

## Global Configuration

### Render Settings

**Dimensions** (inherited by hosts 3-4):
- **desktop** (id: 1): Bot aliases `$GooglebotSearchDesktop`, `$BingbotDesktop`, `*Slurp*`
- **mobile** (id: 2): Bot aliases `$GooglebotSearchMobile`, `$BingbotMobile`
- **ai_bots** (id: 3): Bot aliases `$ChatGPTUserBot`, `$PerplexityBot`

**Cache**:
- TTL: 1h
- Expired strategy: `serve_stale`
- Stale TTL: 24h

**Events**:
- Wait for: `networkIdle`
- Additional wait: 1s

**Resource Blocking**:
- Image, Media, Font

### Bypass Settings

- Timeout: 30s
- User-Agent: `Mozilla/5.0 (compatible; EdgeComet/1.0)`
- Cache: enabled (30m TTL, status_codes: [200])

## Test Scenarios Covered

### 1. Absolute URL Testing
- Blog post URL (`/blog/post-123`) → render action
- Admin URL (`/admin/users`) → status_403 action
- Account page (`/account/settings`) → bypass action
- Static file (`/static/main.js`) → bypass with cache
- Unknown host → error with available hosts list

### 2. Relative URL Testing
- Relative path (`/blog/post`) tested against all 4 hosts
- Admin path (`/admin/users`) tested against all 4 hosts
- Root path (`/`) tested against all 4 hosts

### 3. Pattern Matching
- Wildcard: `/blog/*` matches `/blog/2024/jan/post` (recursive)
- Exact: `/` matches root only
- Default: unmatched paths use host default action
- Priority: first matching pattern wins

### 4. Configuration Resolution
- Cache TTL inheritance and overrides (15m, 1h, 2h, 24h)
- Bypass cache configuration (enabled/disabled, TTL, status_codes)
- Status action configuration (code, reason)
- Dimension matching and fallback behavior

### 5. URL Normalization
- Host lowercasing: `EXAMPLE.COM` → `example.com`
- Query parameter sorting: `?z=3&a=1&m=2` → `?a=1&m=2&z=3`
- Trailing slash handling: `/blog/post/` → `/blog/post`
- URL hash generation (XXHash64)

### 6. Host Lookup
- Exact domain match: `example.com`
- Case-insensitive: `EXAMPLE.COM` → `example.com`
- Port stripping: `example.com:443` → `example.com`
- Subdomain matching: `shop.example.com`

## Configuration Details

- **Redis**: `localhost:6379` (db: 0)
- **Storage**: `/tmp/cache/test-url-tester`
- **Server Port**: `9070`
- **Metrics Port**: `9091`
- **Render Keys**: `test-render-key-example`, `test-render-key-shop`, `test-render-key-blog`, `test-render-key-someeshop`

## Usage

These fixtures are used by unit tests in `internal/edge/configtest/url_tester_test.go`:

```go
result, err := validate.ValidateConfiguration("../../../tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml")
require.NoError(t, err)

urlResult, err := TestURL("https://example.com/blog/post", result)
require.NoError(t, err)
```

## Related Files

- `internal/edge/configtest/url_tester.go` - TestURL implementation
- `internal/edge/configtest/url_tester_test.go` - Tests using these fixtures
- `internal/edge/configtest/output.go` - Output formatting for CLI
- `internal/edge/validate/validate.go` - Configuration validation
