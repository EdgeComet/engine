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
- Duplicate slashes collapsed (`//` → `/`)
- Relative segments resolved (`.` and `..`)
- Trailing slashes preserved

### Query parameters

- Parameters sorted alphabetically by key
- Multiple values for same key preserved in original order
- Consistent URL encoding
- Empty values handled (`?key` vs `?key=`)

### Fragments

- Removed by default (content after `#`)

### Non-ASCII characters

- Percent-encoded using UTF-8 (e.g., `α` becomes `%CE%B1`)
- Uppercase hex digits in encoding (e.g., `%2f` normalized to `%2F`)
- Applies to paths, query parameter names, and values

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
- **Output format**: 16-character lowercase hexadecimal (e.g., `a1b2c3d4e5f67890`)
- **Deterministic**: Same normalized URL always produces same hash

Example transformation:
```
Input:  HTTPS://Example.Com:443/path//to/../page?z=1&a=2&utm_source=google#section
Output: https://example.com/path/page?a=2&z=1
Hash:   a1b2c3d4e5f67890
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
