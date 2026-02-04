---
title: URL rules
description: How to match requests and configure actions using path patterns and query parameters
---

# URL rules

URL patterns are the way to override almost any setting, configure an action, or simply block it.
Patterns match the URL path only - query parameters are ignored during pattern matching.
Use `match_query` for query parameter conditions.

## Actions

Each URL rule requires an `action` that determines how Edge Gateway handles matching requests.

### render

Render the page using headless Chrome and cache the result. Settings merge with host/global configuration - you only need to specify overrides.

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  # Override only cache TTL, inherit everything else from host/global
  - match: "/blog/*"
    action: "render"
    render:
      cache:
        ttl: 4h

  # Override multiple settings
  - match: "/heavy-page"
    action: "render"
    render:
      timeout: 60s
      cache:
        ttl: 2h
        expired:
          strategy: "serve_stale"
          stale_ttl: 24h
      events:
        additional_wait: 500ms
      blocked_resource_types:
        - Image
        - Media
```
:::

### bypass

Fetch the page directly from the origin server without rendering. Settings merge with host/global configuration - you only need to specify overrides.

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  # Enable caching for this pattern, inherit timeout from host/global
  - match: "/api/*"
    action: "bypass"
    bypass:
      cache:
        enabled: true
        ttl: 5m

  # Override timeout and enable caching
  - match: "/slow-api/*"
    action: "bypass"
    bypass:
      timeout: 30s
      cache:
        enabled: true
        ttl: 1m
        status_codes: [200, 201]
```
:::

### block / status_403

Return 403 Forbidden. `block` is an alias for `status_403`.

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  - match: ["/admin/*", "/wp-admin/*"]
    action: "status_403"
    status:
      reason: "Admin areas not available for bots"
```
:::

### status_404

Return 404 Not Found. Use for soft-deleted content.

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  - match: ["/removed/*", "/archived/*"]
    action: "status_404"
    status:
      reason: "Content no longer available"
      headers:
        X-Removal-Date: "2024-01-15"
```
:::

### status_410

Return 410 Gone. Use for permanently removed content.

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  - match: "/discontinued/*"
    action: "status_410"
    status:
      reason: "Product line discontinued"
```
:::

### status

Return a custom status code (3xx, 4xx, 5xx) with optional headers and reason.

**Required fields:**
- `status.code` - HTTP status code (300-599)
- `status.headers.Location` - required for 3xx redirects

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  # Permanent redirect
  - match: "/old-homepage"
    action: "status"
    status:
      code: 301
      headers:
        Location: "https://example.com/"

  # Temporary redirect
  - match: "/maintenance"
    action: "status"
    status:
      code: 302
      headers:
        Location: "https://example.com/maintenance-page"

  # Rate limiting
  - match: "/api/rate-limited/*"
    action: "status"
    status:
      code: 429
      reason: "Too many requests"
      headers:
        Retry-After: "3600"
```
:::

## Path patterns

### Exact patterns

Match the path exactly as written. No special characters.

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  - match: "/about"
    action: "render"
```
:::

Matches: `/about`, `/about?ref=homepage`
Does not match: `/about/team`, `/about-us`

### Wildcard patterns

Use `*` for recursive matching at any depth. There is no `**` syntax - a single `*` matches any number of path segments.

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  - match: "/blog/*"
    action: "render"
  - match: "*.pdf"
    action: "bypass"
```
:::

`/blog/*` matches:
- `/blog/post`
- `/blog/2024/post`
- `/blog/2024/jan/post`

`*.pdf` matches:
- `/document.pdf`
- `/files/report.pdf`
- `/archive/2024/q1/summary.pdf?download=true`

### Regexp patterns

Use `~` prefix for case-sensitive matching, `~*` for case-insensitive.

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  - match: "~/api/v[0-9]+/.*"
    action: "bypass"
  - match: "~*/.*\\.(jpg|png|gif)$"
    action: "bypass"
```
:::

`~/api/v[0-9]+/.*` matches: `/api/v1/users`, `/api/v2/posts`

`~*/.*\\.(jpg|png|gif)$` matches: `/photo.JPG`, `/images/logo.PNG`

## Query parameter matching

Use `match_query` to add conditions based on query parameters. This works alongside path patterns.

### Matching logic

- **Between parameters**: AND logic - all specified parameters must match
- **Within array values**: OR logic - parameter must match one of the values

### Pattern types

Query values support the same pattern types as paths:

- **Exact**: `"tech"` matches only `tech`
- **Wildcard**: `"*"` matches any non-empty value
- **Regexp**: `"~^[0-9]+$"` for case-sensitive, `"~*^[a-z]+$"` for case-insensitive

### Examples

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  # Parameter must exist with any non-empty value
  - match: "/search"
    match_query:
      q: "*"
    action: "render"

  # Parameter must match one of the values (OR)
  - match: "/products"
    match_query:
      category: ["electronics", "clothing", "home"]
    action: "render"

  # Multiple parameters must all match (AND)
  - match: "/api/data"
    match_query:
      format: "json"
      page: "~^[0-9]+$"
    action: "bypass"
```
:::

The last rule matches `/api/data?format=json&page=5` but not `/api/data?format=xml&page=5` (wrong format) or `/api/data?format=json&page=abc` (page not numeric).

## Testing rules

Use the `-t` flag to test your configuration and see how URLs will be processed without starting the server.

### Validate configuration

```bash
./edge-gateway -c configs/edge-gateway.yaml -t
```

Output shows validation status and any warnings:

```
configuration file configs/edge-gateway.yaml syntax is ok
configuration test is successful
```

### Test a specific URL

Pass a URL as the first argument to see which rule matches and what action will be taken:

```bash
# Test absolute URL against specific host
./edge-gateway -c configs/edge-gateway.yaml -t https://example.com/blog/post

# Test relative path against all configured hosts
./edge-gateway -c configs/edge-gateway.yaml -t /api/users
```

Output shows:
- Normalized URL and hash (for cache key debugging)
- Matched pattern (or "default" if no pattern matched)
- Action and resolved configuration

Example output:

```
=== Host: example.com (host_id: 1) ===
URL: https://example.com/blog/post
Normalized URL: https://example.com/blog/post
URL Hash: a1b2c3d4e5f6

Matched Pattern: /blog/*
Action: render

Cache TTL: 7200s (2h)

Rendering:
  - Timeout: 30s
  - Wait Until: networkIdle
```

Exit code is 0 for success, 1 for errors.
