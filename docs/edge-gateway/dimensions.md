---
title: Dimensions
description: How Edge Gateway handles different device types and bot User-Agents
---

# Dimensions

## Overview

Every render requires a viewport size and User-Agent string. Most websites use responsive layouts and serve the same content for mobile and desktop visitors.

Use dimensions to configure how Edge Gateway handles incoming requests based on User-Agent. Each dimension defines an action (render, bypass, or block) and patterns to match incoming User-Agents. Render dimensions also specify viewport size and the User-Agent string sent to Chrome during rendering. When a bot accesses EG, the system matches its User-Agent against dimension patterns to determine which dimension applies.

Dimensions are configured at the host level (not inside `render:`). Global dimensions serve as defaults for hosts that don't define their own.

## Dimension components

::: code-group
```yaml [Host - example.com.yaml]
dimensions:
  mobile_bots:
    id: 1                             # Unique identifier for cache keys
    action: "render"                  # Action: render, bypass, or block
    width: 375                        # Viewport width in pixels
    height: 812                       # Viewport height in pixels
    render_ua: "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 Chrome/120.0.0.0 Mobile Safari/537.36"  # User-Agent sent to Chrome
    match_ua:                         # Patterns to match incoming User-Agents
      # Google bots
      - $GooglebotSearchDesktop
      - $GooglebotSearchMobile
      # AI bots
      - $ChatGPTUserBot
      - $ChatGPTTrainingBot
      - $PerplexityBot
      - $AnthropicBot
      # Custom patterns
      - "*WhatsApp*"                  # Wildcard pattern for WhatsApp bot
```
:::

| Parameter | Description |
|-----------|-------------|
| `id` | Unique integer identifier used in cache keys. Must be stable to avoid cache invalidation. ID `0` is reserved for the built-in bypass dimension. |
| `action` | Dimension action: `"render"` (default), `"bypass"`, or `"block"`. See [dimension actions](#dimension-actions). |
| `width` | Viewport width in pixels for Chrome rendering. Required for render dimensions. |
| `height` | Viewport height in pixels for Chrome rendering. Required for render dimensions. |
| `render_ua` | User-Agent string sent to the target website during Chrome rendering. Required for render dimensions. |
| `match_ua` | Patterns to match incoming request User-Agents. Supports exact strings, wildcards, regexp, and aliases. Use `"*"` to match all User-Agents. Required for block dimensions. |

## Dimension actions

Each dimension has an `action` field that determines how matching requests are handled.

| Action | Behavior | Required fields |
|--------|----------|----------------|
| `render` | Render the page with Chrome and cache the result. This is the default when `action` is omitted. | `id`, `width`, `height`, `render_ua`, `match_ua` |
| `block` | Return 403 Forbidden. Checked before URL rules and status actions. | `id`, `match_ua` |
| `bypass` | Fetch directly from origin without rendering. Reserved for the built-in bypass dimension. | auto-injected |

### Render dimensions

Render dimensions open pages in headless Chrome with the specified viewport and User-Agent, then cache the rendered HTML.

::: code-group
```yaml [Host - example.com.yaml]
dimensions:
  desktop:
    id: 1
    action: "render"
    width: 1920
    height: 1080
    render_ua: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36"
    match_ua:
      - $GooglebotSearchDesktop
      - $BingbotDesktop
      - $AIBots
```
:::

When no URL rule matches a request, the dimension action applies as the default. URL rules override the dimension action for specific paths.

### Block dimensions

Block dimensions reject matching requests with 403 Forbidden before any URL rule or status action processing. This makes block dimensions absolute - no URL rule can override them.

::: code-group
```yaml [Host - example.com.yaml]
dimensions:
  scrapers:
    id: 3
    action: "block"
    match_ua:
      - "*SemrushBot*"
      - "*AhrefsBot*"
      - "*MJ12bot*"
```
:::

Block dimensions require at least one `match_ua` pattern. They do not need `width`, `height`, or `render_ua` since no rendering occurs.

### Built-in bypass dimension

Every host automatically receives a bypass dimension with name `bypass` and ID `0`. This dimension handles requests that don't match any other dimension or that are explicitly routed to bypass through `unmatched_dimension: "bypass"`.

You don't need to declare it. If you want to assign specific User-Agent patterns to the bypass dimension, declare it explicitly:

::: code-group
```yaml [Host - example.com.yaml]
dimensions:
  desktop:
    id: 1
    action: "render"
    width: 1920
    height: 1080
    render_ua: "Mozilla/5.0 Chrome/120.0.0.0 Safari/537.36"
    match_ua:
      - $SearchBots
      - $AIBots

  bypass:
    match_ua:
      - "*Chrome*"
```
:::

The bypass dimension ID (`0`) and action (`"bypass"`) are set automatically. User-defined dimensions cannot use `action: "bypass"` - all bypass traffic routes through the single built-in dimension to prevent duplicate cache entries.

Bypass responses are cached with dimension ID `0` in the standard cache key format: `cache:{host_id}:0:{url_hash}`.

## Dimension IDs

Pages are cached separately for each dimension. If you configure three dimensions, each URL can have up to three cached versions (plus a bypass cache entry with dimension ID `0`).

The dimension ID is part of both the Redis cache key and the filename:

- Redis key: `cache:{host_id}:{dimension_id}:{url_hash}`
- Filename: `{hash}_{dim}.html`

Use explicit, stable IDs for each dimension. Changing an ID invalidates all existing cache entries for that dimension, forcing re-renders. ID `0` is reserved for the built-in bypass dimension.

## Precedence

When a request matches a dimension, the following precedence determines the response:

1. **Block dimension** - absolute 403, no further processing
2. **Status actions** - URL rules with status actions (403, 404, 410, custom)
3. **URL rule action** - render or bypass action from a matching URL rule
4. **Dimension default action** - the dimension's `action` field applies when no URL rule matches

A URL rule with `match_ua` takes part in steps 2 and 3 like any other URL rule.

## Block dimension or match_ua rule

A block dimension and a [URL rule with `match_ua`](./url-rules.md#user-agent-matching) can both single out a bot by User-Agent:

| | Block dimension | URL rule with `match_ua` |
|---|---|---|
| Response | 403 Forbidden | Any URL rule action: a status (403, 404, 410, 429 with `Retry-After`, a redirect), bypass, cache-only bypass, or render with overrides |
| Scope | Whole site | Any path, query parameters, or both |
| Precedence | Runs before URL rules; nothing overrides it | A URL rule; cannot unblock a block dimension or `unmatched_dimension: "block"` |
| Cache | Not used | The bot stays in its dimension and shares that dimension's cache |

Use a block dimension when a bot must receive 403 everywhere and no URL rule may change that. Use a `match_ua` rule for everything else.

Moving a bot into its own render dimension to treat it differently has a cost: the dimension ID is part of the cache key, so the bot no longer sees any page cached for other bots. A `match_ua` rule leaves the bot in the dimension it was detected in.

A `match_ua` rule also applies to a User-Agent that matches no dimension, unless `unmatched_dimension` is `"block"`: the request is handled in the fallback dimension and URL rules apply as usual.

## User-Agent aliases

Aliases simplify configuration by grouping common bot User-Agent patterns under memorable names. Each alias expands to multiple exact strings and regexp patterns that cover known bot variants. When bots update their User-Agents, aliases are updated accordingly.

### Pattern matching syntax

You can use four pattern types in `match_ua`:

| Type | Syntax | Example |
|------|--------|---------|
| Exact | No prefix | `"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"` |
| Wildcard | `*` | `"*Googlebot*"` or `"*"` (match all) |
| Regexp (case-sensitive) | `~` prefix | `"~^Mozilla.*Googlebot/2\\.1"` |
| Regexp (case-insensitive) | `~*` prefix | `"~*googlebot"` |

Examples:

::: code-group
```yaml [edge-gateway.yaml]
match_ua:
  # Exact match - must match entire User-Agent string
  - "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"

  # Wildcard - matches any string containing "WhatsApp"
  - "*WhatsApp*"

  # Regexp - matches Slurp with any version number
  - "~Slurp/[0-9]+\\.[0-9]+"

  # Case-insensitive regexp
  - "~*facebookexternalhit"
```
:::

### Using aliases

Prefix alias names with `$` to expand them:

::: code-group
```yaml [edge-gateway.yaml]
match_ua:
  - $GooglebotSearchDesktop    # Expands to multiple patterns
  - $BingbotMobile
  - "*CustomBot*"              # Mix with custom patterns
```
:::

### Available aliases

**Google**

| Alias | Description |
|-------|-------------|
| `$GooglebotSearchDesktop` | Googlebot desktop search crawler and Google-InspectionTool desktop |
| `$GooglebotSearchMobile` | Googlebot mobile search crawler and Google-InspectionTool mobile |
| `$GoogleBotAds` | Google Ads bot |
| `$GoogleBotAdsMobileWeb` | Google Ads mobile bot |
| `$StorebotGoogle` | Google Storebot, desktop and mobile (`*Storebot-Google/*`) |

**Bing**

| Alias | Description |
|-------|-------------|
| `$BingbotDesktop` | Bing desktop crawler |
| `$BingbotMobile` | Bing mobile crawler |

**AI bots**

| Alias | Description |
|-------|-------------|
| `$ChatGPTUserBot` | ChatGPT user browsing |
| `$ChatGPTTrainingBot` | GPTBot for training |
| `$OpenAISearchBot` | OpenAI search bot |
| `$OpenAIAdsBot` | OpenAI ads crawler |
| `$GoogleAgentDesktop` | Google-Agent desktop crawler |
| `$GoogleAgentMobile` | Google-Agent mobile crawler |
| `$PerplexityBot` | Perplexity search bot |
| `$PerplexityUserBot` | Perplexity user queries |
| `$AnthropicBot` | Claude indexing bot |
| `$AnthropicUserBot` | Claude user browsing |
| `$AnthropicSearchBot` | Claude search bot |
| `$Amazonbot` | Amazon crawler (`*Amazonbot/*`) |
| `$AmazonUser` | Amazon user browsing (`*AMZN-User/*`) |

**Meta**

| Alias | Description |
|-------|-------------|
| `$MetaExternalAgent` | Meta external agent crawler (`*meta-externalagent/*`) |
| `$MetaWebIndexer` | Meta web indexer (`*meta-webindexer/*`) |

**Messaging apps and social networks**

| Alias | Description |
|-------|-------------|
| `$Messengers` | WhatsApp, Viber, Telegram, Snapchat, Discord, and Slack link preview bots. |
| `$Socials` | Facebook link previews (`facebookexternalhit`), Twitterbot, Pinterestbot, and Applebot. |

**Composite aliases**

Composite aliases combine multiple individual aliases for convenience:

| Alias | Includes |
|-------|----------|
| `$SearchBots` | GooglebotSearchDesktop, GooglebotSearchMobile, BingbotDesktop, BingbotMobile. |
| `$AIBots` | All AI bot aliases (ChatGPT, OpenAI, Google-Agent, Perplexity, Anthropic, Amazon). |

`$AIBots` and `$Socials` do not include `$MetaExternalAgent`, `$MetaWebIndexer` or `$StorebotGoogle`. Name those aliases explicitly where you need them.

## Fallback behavior

When a request User-Agent doesn't match any dimension pattern, Edge Gateway uses the `unmatched_dimension` setting to determine behavior. This setting is configured at the host level.

| Value | Behavior |
|-------|----------|
| `"bypass"` | Route through the built-in bypass dimension (default). |
| `"block"` | Return 403 Forbidden. |
| dimension name | Use specified dimension as fallback (e.g., `"desktop"`). |

### Protecting resources

Configure dimensions for important bots only and block or bypass everything else. This serves rendered content to search engines and AI bots while rejecting scrapers and unknown crawlers.

::: code-group
```yaml [Host - example.com.yaml]
unmatched_dimension: "block"
dimensions:
  search_bots:
    id: 1
    action: "render"
    width: 1920
    height: 1080
    render_ua: "Mozilla/5.0 Chrome/120.0.0.0 Safari/537.36"
    match_ua:
      - $GooglebotSearchDesktop
      - $GooglebotSearchMobile
      - $BingbotDesktop
      - $BingbotMobile
      - $ChatGPTUserBot
      - $PerplexityBot
      - $AnthropicBot
```
:::

This configuration renders pages only for Googlebot, Bingbot, and AI bots. All other requests receive 403 Forbidden, saving Chrome resources and preventing unauthorized scraping.

Use `"bypass"` instead of `"block"` if you want unmatched bots to receive content from origin without rendering.

You can also use block dimensions to reject specific bots while keeping `unmatched_dimension: "bypass"` for everything else:

::: code-group
```yaml [Host - example.com.yaml]
unmatched_dimension: "bypass"
dimensions:
  search_bots:
    id: 1
    action: "render"
    width: 1920
    height: 1080
    render_ua: "Mozilla/5.0 Chrome/120.0.0.0 Safari/537.36"
    match_ua:
      - $SearchBots
      - $AIBots

  scrapers:
    id: 2
    action: "block"
    match_ua:
      - "*SemrushBot*"
      - "*AhrefsBot*"
```
:::

## Configuration example

Host configuration with desktop and mobile render dimensions, a block dimension for scrapers, and bypass as fallback for everything else:

::: code-group
```yaml [hosts.d/example.com.yaml]
hosts:
  - domain: "example.com"
    unmatched_dimension: "bypass"

    dimensions:
      desktop:
        id: 1
        action: "render"
        width: 1920
        height: 1080
        render_ua: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36"
        match_ua:
          - $GooglebotSearchDesktop
          - $BingbotDesktop
          - $AIBots

      mobile:
        id: 2
        action: "render"
        width: 375
        height: 812
        render_ua: "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 Chrome/120.0.0.0 Mobile Safari/537.36"
        match_ua:
          - $GooglebotSearchMobile
          - $BingbotMobile
          - $Messengers

      scrapers:
        id: 3
        action: "block"
        match_ua:
          - "*SemrushBot*"
          - "*AhrefsBot*"

    render:
      timeout: "30s"
      events:
        wait_for: "networkIdle"
        additional_wait: "1s"
```
:::
