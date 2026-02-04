---
title: Dimensions
description: How Edge Gateway handles different device types and bot User-Agents
---

# Dimensions

## Overview

Every render requires a viewport size and User-Agent string. Most websites use responsive layouts and serve the same content for mobile and desktop visitors.

Use dimensions to configure rendering based on the incoming request User-Agent. Each dimension specifies viewport size (width and height), User-Agent string sent to Chrome during rendering, and patterns to match incoming request User-Agents. When a bot accesses EG, the system matches its User-Agent against dimension patterns to determine which viewport to use.

Dimensions also control rendering behavior. For example, you can render Googlebot and AI bot requests while bypassing or blocking other bots.

## Dimension components

::: code-group
```yaml [edge-gateway.yaml]
dimensions:
  mobile_bots:
    id: 1                             # Unique identifier for cache keys
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
| `id` | Unique integer identifier used in cache keys. Must be stable to avoid cache invalidation. |
| `width` | Viewport width in pixels for Chrome rendering. |
| `height` | Viewport height in pixels for Chrome rendering. |
| `render_ua` | User-Agent string sent to the target website during Chrome rendering. |
| `match_ua` | Patterns to match incoming request User-Agents. Supports exact strings, wildcards, regexp, and aliases. Use `"*"` to match all User-Agents. |

## Dimension IDs

Pages are cached separately for each dimension. If you configure three dimensions, each URL can have up to three cached versions.

The dimension ID is part of both the Redis cache key and the filename:

- Redis key: `cache:{host_id}:{dimension_id}:{url_hash}`
- Filename: `{hash}_{dim}.html`

Use explicit, stable IDs for each dimension. Changing an ID invalidates all existing cache entries for that dimension, forcing re-renders.

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
| `$GooglebotSearchDesktop` | Googlebot desktop search crawler |
| `$GooglebotSearchMobile` | Googlebot mobile search crawler |
| `$GoogleBotAds` | Google Ads bot |
| `$GoogleBotAdsMobileWeb` | Google Ads mobile bot |

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
| `$PerplexityBot` | Perplexity search bot |
| `$PerplexityUserBot` | Perplexity user queries |
| `$AnthropicBot` | Claude indexing bot |
| `$AnthropicUserBot` | Claude user browsing |
| `$AnthropicSearchBot` | Claude search bot |

**Messaging apps**

| Alias | Description |
|-------|-------------|
| `$Messengers` | WhatsApp, Viber, Telegram, Snapchat, Discord, and Slack link preview bots. |

**Composite aliases**

Composite aliases combine multiple individual aliases for convenience:

| Alias | Includes |
|-------|----------|
| `$SearchBots` | GooglebotSearchDesktop, GooglebotSearchMobile, BingbotDesktop, BingbotMobile. |
| `$AIBots` | All AI bot aliases (ChatGPT, OpenAI, Perplexity, Anthropic). |

## Fallback behavior

When a request User-Agent doesn't match any dimension pattern, Edge Gateway uses the `unmatched_dimension` setting to determine behavior.

| Value | Behavior |
|-------|----------|
| `"bypass"` | Fetch content from origin without rendering (default). |
| `"block"` | Return 403 Forbidden. |
| dimension name | Use specified dimension as fallback (e.g., `"desktop"`). |

### Protecting resources

Configure dimensions for important bots only and block or bypass everything else. This serves rendered content to search engines and AI bots while rejecting scrapers and unknown crawlers.

::: code-group
```yaml [edge-gateway.yaml]
render:
  unmatched_dimension: "block"         # Block unmatched User-Agents
  dimensions:
    search_bots:
      id: 1
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

## Configuration example

Minimal host configuration with a single mobile dimension for all bots:

::: code-group
```yaml [hosts.d/example.com.yaml]
hosts:
  - domain: "example.com"
    render:
      unmatched_dimension: "bypass"
      timeout: "30s"
      events:
        wait_for: "networkIdle"
        additional_wait: "1s"
      dimensions:
        mobile:
          id: 1
          width: 375
          height: 812
          render_ua: "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 Chrome/120.0.0.0 Mobile Safari/537.36"
          match_ua:
            - $SearchBots
            - $AIBots
            - $Messengers
```
:::
