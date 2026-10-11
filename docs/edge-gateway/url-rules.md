---
title: URL rules
description: How to match requests and configure actions using path patterns and query parameters
---

# URL rules

URL patterns are the way to override almost any setting, configure an action, or simply block it.
Patterns match the URL path only - query parameters are ignored during pattern matching.
Use `match_query` for query parameter conditions and `match_ua` for User-Agent conditions.

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

## User-Agent matching

Use `match_ua` to apply a rule only to clients whose User-Agent matches one of the listed patterns. With the existing actions this lets a host block one bot, answer it with another status, stop it from rendering or filling the cache, or change render settings for it - on the whole site or on chosen paths and query parameters.

### Matching logic

- **Within the list**: OR logic - the rule matches when any pattern matches the User-Agent
- **With `match` and `match_query`**: AND logic - the path, the query conditions and the User-Agent must all match
- **Without `match_ua`**: the rule applies to every client

A request without a User-Agent header never matches a rule with `match_ua`, whatever its patterns are (even `~^$`). When a request carries more than one `User-Agent` header, Edge Gateway matches the last one.

Edge Gateway resolves rules without a client User-Agent for its own operations, so rules with `match_ua` never apply to them: precache and recache, Cache Daemon cache operations, `/debug/har/render` and `-t` without `-ua`. A URL that a `match_ua` rule blocks for one bot is still precached and stored for every other client.

`match_ua` matches the User-Agent the client claims. A spoofed ClaudeBot User-Agent is treated as ClaudeBot.

### Pattern types

`match_ua` uses the same pattern syntax and [aliases](./dimensions.md#available-aliases) as dimension `match_ua`:

| Type | Syntax | Case | Example |
|------|--------|------|---------|
| Exact | No prefix | Insensitive | `"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"` |
| Wildcard | `*` | Insensitive | `"*ClaudeBot*"` |
| Regexp | `~` prefix | Sensitive | `"~ClaudeBot/[0-9]+"` |
| Regexp | `~*` prefix | Insensitive | `"~*claudebot"` |
| Alias | `$` prefix | Set by the alias | `"$AnthropicBot"` |

Exact and wildcard patterns match the whole User-Agent string. An exact pattern matches case-insensitively, as for dimensions, so `"claudebot"` matches a User-Agent that is exactly `ClaudeBot` and nothing longer. Use `"*ClaudeBot*"` to match a token inside a longer string. Regexp patterns match anywhere in the string unless anchored with `^` or `$`.

### Examples

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  # Block ClaudeBot on the whole site
  - match: "*"
    match_ua: ["$AnthropicBot"]
    action: "status_403"

  # Ask Meta to back off on filter URLs only
  - match: "*"
    match_query:
      inches: "*"
    match_ua: ["$MetaExternalAgent", "$MetaWebIndexer"]
    action: "status"
    status:
      code: 429
      headers:
        Retry-After: "3600"

  # Cache-only for AI crawlers on filter URLs: a cached render if there is one,
  # otherwise origin HTML; never store, never render
  - match: "*"
    match_query:
      inches: "*"
    match_ua: ["$AnthropicBot", "$Amazonbot"]
    action: "bypass"
    bypass:
      cache:
        enabled: false
    bothit_recache:
      enabled: false

  # Render one bot with a different dimension and timeout
  - match: "/products/*"
    match_ua: ["$AnthropicBot"]
    action: "render"
    render:
      dimension: "mobile"
      timeout: 20s

  # Always fresh for price checks on a bypass host: Storebot reaches the origin
  # on every request, every other bot keeps the bypass cache
  - match: "/product/*"
    match_ua: ["$StorebotGoogle"]
    action: "bypass"
    bypass:
      cache:
        enabled: false
```
:::

### Behavior per action

What a matching bot receives:

| Rule action | Something is cached for the URL | Nothing is cached |
|-------------|---------------------------------|-------------------|
| `block`, `status_403`, `status_404`, `status_410`, `status` | The status response. Status actions run before any cache lookup, so the bot never receives cached HTML | The status response |
| `bypass` with `bypass.cache.enabled: false` (cache-only) | A fresh render is served (`EC-Source: render_cache`). A stale render and any bypass entry are not served: the bot gets origin HTML instead | Origin HTML (`EC-Source: bypass`). Nothing is stored, nothing is rendered, no render lock is taken |
| `bypass` | A fresh render is served, otherwise a fresh bypass entry | Origin HTML, stored as a bypass entry |
| `render` (with overrides) | The cached page is served | Rendered and stored |

Cache entries are shared per slot - host, dimension and URL. Entries that a `render` rule or a `bypass` rule with the bypass cache on stores for the bot are served to every other client in the same slot afterwards, so the rule's cache settings (TTL, status codes) apply to them too.

A `render` rule with `render.dimension` moves the bot's renders into that dimension's slot. Clients in the bot's detected dimension, for example desktop Googlebot, do not see those renders.

Cache-only on a render host serves unrendered origin HTML to the bot for every page without a cached render. For an AI crawler on a JavaScript-built site that is the content rendering exists to replace. Use no rule at all when the bot should see rendered content.

On a host with [bot hit recache](./caching.md#bot-hit-recache) enabled, a cached render served to a bot that `bothit_recache.match_ua` lists schedules a background render, under a cache-only rule as well. Add `bothit_recache: {enabled: false}` to the rule so the bot never causes a render.

When the origin cannot be reached under a cache-only rule, the bot gets a 502. There is no stale fallback, because cache-only reads no bypass entry.

### Cache-only

Cache-only serves a cached render if there is one and otherwise origin HTML, and never stores or renders anything. It stops one bot from filling the cache or triggering renders on a URL space where its fetches are rarely reused, faceted filter URLs being the common case, while other bots keep caching:

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  - match: "/catalog/*"
    match_ua: ["$AnthropicBot", "$Amazonbot"]
    action: "bypass"
    bypass:
      cache:
        enabled: false
    bothit_recache:
      enabled: false
```
:::

Every uncached request from a matching User-Agent, spoofed or not, reaches the origin. Do not put a high-volume crawler on a cache-only rule when the origin cannot take its full fetch rate.

### Always fresh for one bot (price verification)

On a bypass host, cache-only gives one bot the origin's current response on every request while every other bot keeps the bypass cache. Google Storebot is the typical case: it compares landing-page prices with the Merchant Center feed, and a cached page with an old price gets the item flagged.

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  - match: "/product/*"
    match_ua: ["$StorebotGoogle"]
    action: "bypass"
    bypass:
      cache:
        enabled: false
```
:::

Cache-only never reads a bypass entry, so Storebot gets `EC-Source: bypass` on every request, and the entry other bots receive keeps its content and its creation time.

A fresh render in the slot is still served. On a bypass host a render exists only when the URL is precached with `"mode": "render"` through the [Cache Daemon API](../cache-daemon/api-reference.md). Stop render-mode precaching on those paths if the bot must always see the origin.

Scope the rule to the paths the bot checks rather than `*`: a site-wide rule overrides every path rule for that bot, see [Rule order](#rule-order).

### Validation

The gateway refuses to start when a rule's `match_ua` has one of these problems. `-t` reports every row except an unknown alias, which it reports only when you pass a URL (`-t <url>`): without a URL it does not expand aliases, so it prints `syntax is ok` and the gateway then fails to start.

| Problem | Error |
|---------|-------|
| Empty list (`match_ua: []`) | `match_ua must list at least one pattern; omit it to match every client` |
| Empty string in the list | `match_ua[0] must not be empty` |
| Pattern made only of `*` (`"*"`, `"**"`) | `match_ua[0] '*' matches every client; omit match_ua instead` |
| Invalid syntax (`**` inside a pattern, a regexp that does not compile) | `match_ua[0]: ` followed by the syntax error, for example `match_ua[0]: invalid user-agent case-sensitive regexp '~(x': ...` |
| `tracking_params` on the same rule | `tracking_params is not allowed on a rule with match_ua; set it at host level` |
| Unknown alias | `url_rule[1] match_ua: failed to expand bot aliases: unknown bot alias "$Nope" ...` |

A rule that matches every client is a rule without `match_ua` that sorts first, so the gateway rejects the wildcard form. A regexp that matches everything, such as `~.*`, is not detected and is accepted. Do not write one.

`tracking_params` decides the cache key, and cache operations compute keys without a User-Agent, so a `match_ua` rule with its own `tracking_params` would store entries that the cache list, purge and recache cannot find. Any `tracking_params` block is rejected on such a rule, `strip: false` included.

To make a rule apply to every client, delete the `match_ua` key. A YAML null (`match_ua: ~` or `match_ua: null`) reads as an omitted key, so the rule then applies to every client.

Errors name the rule by its position in the host file:

```
Configuration validation FAILED:
- 01-example.yaml: host[0] (example.com): url_rules[1]: match_ua[0] '*' matches every client; omit match_ua instead
```

## Rule order

Edge Gateway sorts URL rules once, when it loads the configuration. For each request the first matching rule wins, and only one rule applies. Rules with an array of patterns are expanded into one rule per pattern before sorting. Sort priority:

1. Rules with `match_ua` before rules without it
2. Pattern type: exact, then wildcard, then regexp
3. Rules with `match_query` before rules without it
4. More slashes (deeper paths) first
5. Declaration order

`match_ua` comes first because the main use is a site-wide bot rule on `*`. Sorted by path alone, that rule would lose to every exact or deeper path rule, and a rule that blocks ClaudeBot would not cover `/`.

::: warning A site-wide match_ua rule overrides path rules for its bot
For a matching bot, a `match_ua` rule on `*` replaces every path rule meant for everyone, and the bot gets host-level values for everything its own rule does not set:

- A path rule's status action no longer applies: a `status_404` on `/admin/*` becomes origin HTML under a `bypass` UA rule
- A redirect rule (`status` with a 301) stops redirecting
- A path rule's `headers.request_headers_set` is not sent, so an origin that checks such a header serves its fallback page
- A path rule's `tracking_params` does not apply, so the bot's URLs hash to a different cache key from everyone else's
- A path rule's `cache_sharding`, `bothit_recache` and render overrides fall back to host values
:::

Scope the `match_ua` rule (`/product/*` rather than `*`), or give the bot its own copy of the path rule. Rules with `match_ua` sort among themselves by the remaining priorities, so the deeper path wins:

::: code-group
```yaml [Host - example.com.yaml]
url_rules:
  - match: "/admin/*"
    action: "status_404"

  # Keeps the 404 for ClaudeBot: sorts before the site-wide rule below
  - match: "/admin/*"
    match_ua: ["$AnthropicBot"]
    action: "status_404"

  - match: "*"
    match_ua: ["$AnthropicBot"]
    action: "bypass"
    bypass:
      cache:
        enabled: false
```
:::

On a host with site-wide `match_ua` rules, set `tracking_params` at host level rather than on path rules.

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
- Matched rule ID, the same value as the `EC-Matched-Rule` response header
- Action and resolved configuration

Example output:

```
=== Host: example.com (host_id: 1) ===
URL: https://example.com/blog/post
Normalized URL: https://example.com/blog/post
URL Hash: 177635683940360438

Matched Pattern: /blog/*
Matched Rule: rule_4:/blog/*
Action: render

Cache TTL: 7200s (2h)

Rendering:
  - Timeout: 30s
  - Wait Until: networkIdle
```

The rule ID has the form `rule_<N>:<pattern>`. `N` is the rule's position after [sorting](#rule-order), not its position in the file. The ID ends in `?...` when the rule has `match_query` and in `+ua` when it has `match_ua`, for example `rule_0:*?...+ua`.

### Test as a specific client

Without `-ua`, the test runs without a client User-Agent, so rules with `match_ua` never match - the view precache and cache operations have. Pass `-ua` to test as a specific client. It works before or after the URL:

```bash
./edge-gateway -c configs/edge-gateway.yaml -t https://example.com/admin/users \
  -ua "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0; +claudebot@anthropic.com)"
```

The output adds the User-Agent and, when a `match_ua` rule matched, that rule's pattern list after alias expansion:

```
=== Host: example.com (host_id: 1) ===
URL: https://example.com/admin/users
Normalized URL: https://example.com/admin/users
URL Hash: 7701484195564188689
User-Agent: Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0; +claudebot@anthropic.com)

Matched Pattern: *
Matched User-Agent Patterns: ~ClaudeBot\/\d+\.\d+; \+claudebot@anthropic\.com
Matched Rule: rule_2:*+ua
Action: status_403
Response: 403 Forbidden
```

Exit code is 0 for success, 1 for errors. Flags may follow the URL; any other argument after it is rejected with exit code 2.
