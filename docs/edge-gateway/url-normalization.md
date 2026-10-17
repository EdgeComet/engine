---
title: URL normalization
description: How Edge Gateway normalizes URLs for consistent cache key generation
---

# URL normalization

## Overview

URL normalization ensures that different URL variations pointing to the same content share a single cache entry. Proper normalization reduces the number of renders and decreases cache storage usage.

Many query parameters serve only tracking or marketing purposes. Parameters like `utm_source`, `utm_content`, and `fbclid` do not affect page content and can be stripped before caching.

Other parameters affect content but their order does not. For example, `?sort=price&limit=10` and `?limit=10&sort=price` return identical results. Edge Gateway automatically sorts query parameters alphabetically, ensuring both URLs generate the same cache key.

## Normalization rules

### Scheme

- Converted to lowercase
- URLs without scheme default to `https://`

### Host

- Converted to lowercase
- Trailing dots removed (`example.com.` → `example.com`)
- Default ports removed (`:80` for HTTP, `:443` for HTTPS)

### Path

- Empty paths become `/`
- Percent-escapes decoded (`/caf%C3%A9` → `/café`; `/a%2Cb` and `/a%2cb` → `/a,b`), so every spelling of a path shares one cache entry. A path that would decode to invalid UTF-8 (for example `%FF`) keeps its escaped form
- Duplicate slashes collapsed (`//` → `/`)
- Relative segments resolved (`.` and `..`). This runs after decoding, so `%2F` acts as `/` and `%2E%2E` as `..`
- Trailing slashes preserved

The decoded path is the cache key. The origin can receive a different spelling, see [What the origin receives](#what-the-origin-receives).

### Query parameters

- Parameters sorted alphabetically by key
- Multiple values for same key preserved in original order
- Names and values re-encoded with uppercase hex (`a=1,2` → `a=1%2C2`, `q=%ce%b1` → `q=%CE%B1`, a space → `+`)
- Empty values handled (`?key` vs `?key=`)

### Fragments

- Removed by default (content after `#`)

### Non-ASCII characters

- Paths: decoded to UTF-8 text (`/%CE%B1` → `/α`), as described under [Path](#path)
- Query parameter names and values: percent-encoded using UTF-8 with uppercase hex (`α` → `%CE%B1`)

## Tracking parameter removal

Edge Gateway strips common tracking and analytics parameters by default. You can customize this list per host.

### Default parameters

| Parameter | Source |
|-----------|--------|
| `utm_source` | Google Analytics |
| `utm_content` | Google Analytics |
| `utm_medium` | Google Analytics |
| `utm_campaign` | Google Analytics |
| `utm_term` | Google Analytics |
| `gclid` | Google Ads |
| `fbclid` | Facebook |
| `msclkid` | Microsoft Ads |
| `_ga` | Google Analytics |
| `_gl` | Google Analytics |
| `mc_cid` | Mailchimp |
| `mc_eid` | Mailchimp |
| `_ke` | Klaviyo |
| `ref` | Generic referrer |
| `referrer` | Generic referrer |

### Configuration

Use `params` to completely replace the built-in defaults, or `params_add` to extend them with additional patterns.

::: code-group
```yaml [Replace defaults - edge-gateway.yaml]
tracking_params:
  params:                       # Replaces all defaults
    - "utm_*"
    - "fbclid"
```

```yaml [Extend defaults - edge-gateway.yaml]
tracking_params:
  params_add:                   # Keep defaults, add custom
    - "affiliate_id"
    - "tracking_*"
```
:::

Host-level configuration adds to global parameters:

::: code-group
```yaml [Host override - hosts.yaml]
hosts:
  - domain: example.com
    id: 1
    render_key: "secret-key"
    tracking_params:
      params_add:
        - "custom_param"
    render:
      timeout: 30s
      # ... other host settings
```
:::

To disable tracking parameter stripping entirely:

::: code-group
```yaml [Disable globally - edge-gateway.yaml]
tracking_params:
  strip: false
```

```yaml [Disable for host - hosts.yaml]
hosts:
  - domain: example.com
    id: 1
    render_key: "secret-key"
    tracking_params:
      strip: false
    # ...
```
:::

URL pattern level configuration for specific paths:

::: code-group
```yaml [URL pattern - hosts.yaml]
hosts:
  - domain: example.com
    id: 1
    render_key: "secret-key"
    url_rules:
      - match: "/api/*"
        action: "bypass"
        tracking_params:
          strip: false              # Keep all params for API
      - match: "/campaigns/*"
        action: "render"
        tracking_params:
          params_add:
            - "campaign_*"          # Strip additional params
```
:::

Configuration follows the standard hierarchy: Global → Host → URL pattern. Parameters accumulate down the hierarchy when using `params_add`. Use `params` at any level to discard all inherited parameters and start fresh with only the specified list.

### Pattern types

| Type | Syntax | Example | Case |
|------|--------|---------|------|
| Exact | No prefix | `"fbclid"` | Insensitive |
| Wildcard | `*` | `"utm_*"` | Insensitive |
| Regexp | `~` prefix | `"~^gclid.*"` | Sensitive |
| Regexp | `~*` prefix | `"~*^gclid.*"` | Insensitive |

## Cache key generation

After normalization, Edge Gateway generates a hash using XXHash64. This hash becomes part of the Redis cache key and HTML filename.

- **Algorithm**: XXHash64 (fast, non-cryptographic hash)
- **In-memory type**: `uint64`
- **Serialized format**: decimal `uint64` everywhere — Redis cache keys, on-disk filenames, JSON APIs, structured logs, and event log lines (e.g., `11399234567890123456`)
- **Deterministic**: Same normalized URL always produces same hash

Example transformation:
```
Input:  HTTPS://Example.Com:443/path//to/../page?z=1&a=2&utm_source=google#section
Output: https://example.com/path/page?a=2&z=1
Hash:   11651889946454949520
```

The hash is used in:
- Redis key: `cache:{host_id}:{dimension_id}:{hash}`
- Filename: `{hash}_{dimension}.html`


## Normalization examples

| Input URL | Normalized URL | Notes |
|-----------|----------------|-------|
| `HTTP://EXAMPLE.COM/Page` | `http://example.com/Page` | Scheme/host lowercased, path case preserved |
| `https://example.com:443/` | `https://example.com/` | Default port removed |
| `example.com/path//file` | `https://example.com/path/file` | Scheme added, double slash collapsed |
| `https://example.com/a/../b` | `https://example.com/b` | Parent reference resolved |
| `https://example.com/page?b=2&a=1` | `https://example.com/page?a=1&b=2` | Query params sorted |
| `https://example.com/?a=2&b=1&a=1` | `https://example.com/?a=2&a=1&b=1` | Multiple values preserved |
| `https://example.com/page?utm_source=google` | `https://example.com/page` | Tracking param stripped |
| `https://example.com/page?id=5&fbclid=abc123` | `https://example.com/page?id=5` | fbclid stripped, id preserved |
| `https://example.com/?gclid=xyz&ref=twitter&q=test` | `https://example.com/?q=test` | Multiple tracking params stripped |
| `/path?b=2&a=1` | Error | Missing host |

## What the origin receives

The normalized URL is the cache key, not always the URL Edge Gateway fetches. Origins can treat spellings of one path differently: some answer `/a,b` with a 301 to `/a%2Cb`, and fetching the decoded path would send the bot a redirect to the URL it requested. So when it is safe, Edge Gateway sends the origin the path as the bot spelled it, on render, bypass and recache:

- Percent-escapes keep their spelling and hex case (`%2C` stays `%2C`, `%2c` stays `%2c`)
- Escapes of unreserved characters (letters, digits, `-`, `.`, `_`, `~`) are decoded (`%41` → `A`, `%7E` → `~`), as before
- Scheme, host and query come from the normalized URL: the query is sorted and tracking parameters are stripped

Edge Gateway sends the normalized path instead when the received path:

- contains `//`, a `.` or `..` segment, `%2F`, `%2E` or `%5C` (any hex case), or a raw `\`
- contains a raw character other than letters, digits, `-._~` and `$&+,;=:@/`, for example `!`, `'`, `(`, `)`, `*`, `[`, `]`, `|`, a space or non-ASCII text

| Path received | Path sent to the origin | Cache key path |
|---------------|-------------------------|----------------|
| `/blog/a%2Cb/53` | `/blog/a%2Cb/53` | `/blog/a,b/53` |
| `/blog/a%2cb/53` | `/blog/a%2cb/53` | `/blog/a,b/53` |
| `/blog/a,b/53` | `/blog/a,b/53` | `/blog/a,b/53` |
| `/caf%C3%A9` | `/caf%C3%A9` | `/café` |
| `/p%3Fq` | `/p%3Fq` | `/p?q` |
| `/x%7Ey` | `/x~y` | `/x~y` |
| `/a//b%2Cc` | normalized path | `/a/b,c` |
| `/a!b` | normalized path | `/a!b` |

The received path is the `url` parameter after Edge Gateway decodes it once. Integrations that encode the target URL (Cloudflare Worker, CloudFront, Fastly) keep the bot's spelling. The nginx and HAProxy examples pass the target URL unencoded, so escapes in its path are decoded on arrival and the origin receives the decoded path.

Recache has no incoming request, so it fetches the spelling stored with the cache entry by the request that produced it, as long as that spelling still maps to the same cache key. Otherwise it fetches the normalized URL, as it does for a URL that was never requested live.

### Cached redirects to the same URL

All spellings of a path share one cache entry, so a 301 that the origin returned for `/a,b` would be served to a request for `/a%2Cb` and point the bot back at its own URL. When a request keeps its spelling, Edge Gateway ignores a cached 301, 302, 307 or 308 whose `Location` normalizes to the request's own cache key, unless the same spelling produced it. It fetches the origin instead, and a cacheable response replaces the entry. This also covers lock waiters and stale fallbacks: an ignored entry is never served as stale.

A URL that redirects to itself for its own spelling is fetched once more and then cached and served as usual.

Each request that ignores a cached redirect logs `Cached redirect ignored for different spelling` at Info and increments `eg_cached_redirect_ignored_total{host}`.
