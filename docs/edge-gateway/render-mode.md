---
title: Render mode
description: How Edge Gateway renders JavaScript-heavy pages using headless Chrome
---

# Render mode

## Overview

Render mode is the default processing for HTML pages. Edge Gateway sends the URL to a Render Service instance, which opens it in headless Chrome, executes JavaScript and AJAX requests, then captures the fully rendered HTML for caching and serving to bots.

Serving rendered pages improves Googlebot crawl budget, leading to better indexation and organic traffic. AI bots like ChatGPT, Claude, and Perplexity do not execute JavaScript, making rendered content essential for their access.

## Chrome lifecycle events

The biggest challenge in rendering modern JavaScript websites is determining when a page is ready and HTML can be captured. Depending on the technology and implementation, this timing varies significantly.

Most issues with JavaScript rendering occur because HTML was captured before the page finished loading.

### wait_for options

Chrome provides several lifecycle events that fire during page loading:

- `DOMContentLoaded`: Fires when initial HTML is parsed
- `load`: Fires when all resources (images, stylesheets) finish loading
- `networkAlmostIdle`: Fires when no more than 2 network connections are active for 500ms
- `networkIdle`: Fires when no network connections are active for 500ms (recommended)

For modern websites, `DOMContentLoaded` and `load` rarely provide useful timing. They fire early in the page lifecycle when JavaScript has just started executing.

The choice is typically between `networkAlmostIdle` and `networkIdle`:

- `networkAlmostIdle` allows some network requests to remain in flight. It works for most websites but may fire too early in some cases.
- `networkIdle` is the recommended event that works for most cases.

### additional_wait

For some websites, even `networkIdle` is not sufficient and HTML is not fully ready. The `additional_wait` setting specifies how long to wait after the `wait_for` event before capturing HTML content. Use Go duration format (e.g., `"500ms"`, `"2s"`).

### Configuration example

::: code-group
```yaml [Global - edge-gateway.yaml]
render:
  events:
    wait_for: "networkIdle"
    additional_wait: "2s"
```
```yaml [Host - example.com.yaml]
hosts:
  - id: 1
    render:
      events:
        wait_for: "networkIdle"
        additional_wait: "2s"
```
```yaml [URL pattern]
url_rules:
  - match: "/spa-pages/*"
    action: "render"
    render:
      events:
        wait_for: "networkIdle"
        additional_wait: "3s"
```
:::

## Render timeout

Pages can sometimes take tens of seconds to render due to JavaScript errors, AJAX requests without timeouts, or slow response times. This significantly degrades overall render performance, and search engine bots will not wait that long. In other cases, Chrome lifecycle events may not fire at all, causing the render process to hang.

The render timeout addresses these issues. It works as a soft timeout: when a page does not fire the `wait_for` event within the timeout period, Chrome stops rendering and captures the page content as-is. Use Go duration format (e.g., `"10s"`, `"1m"`).

### Configuration

::: code-group
```yaml [Global - edge-gateway.yaml]
render:
  timeout: "30s"
```
```yaml [Host - example.com.yaml]
hosts:
  - id: 1
    render:
      timeout: "30s"
```
```yaml [URL pattern]
url_rules:
  - match: "/slow-pages/*"
    action: "render"
    render:
      timeout: "60s"
```
:::

## Hard timeout

The hard timeout is a safety mechanism configured on the Render Service that forcefully cancels renders that exceed the maximum allowed time. Unlike the soft timeout (which captures partial content), the hard timeout completely aborts the render and returns a 504 Gateway Timeout error.

Set this value higher than your Edge Gateway render timeout to allow soft timeout to work first. The hard timeout should only trigger when Chrome becomes stuck due to browser hangs, infinite loops, or other unrecoverable issues.

### Configuration

Configure `max_timeout` in the Render Service configuration:

::: code-group
```yaml [render-service.yaml]
chrome:
  render:
    max_timeout: "60s"  # Maximum time before force-cancelling render
```
:::

## Resource blocking

Pages load many additional resources including JavaScript, CSS, fonts, images, and videos. While important for end-user experience, these resources are not necessary for rendering purposes.

Blocking resources significantly reduces traffic to the origin website and increases rendering speed. However, in rare cases, certain resources may be required for proper JavaScript functionality.

### Blocked resource types

You can block resources by type. The recommended defaults are:

- `Image`: Block all images
- `Media`: Block audio and video
- `Font`: Block web fonts

All available resource types from Chrome DevTools Protocol:

- `Document`, `Stylesheet`, `Image`, `Media`, `Font`, `Script`
- `TextTrack`, `XHR`, `Fetch`, `Prefetch`, `EventSource`, `WebSocket`
- `Manifest`, `SignedExchange`, `Ping`, `CSPViolationReport`, `Other`

### Blocked URL patterns

Pages typically contain Google Tag Manager, Analytics, and other tracking scripts. There is no need to execute these during rendering. The default blocking list contains common trackers and analytics systems. You can add custom scripts, domains, and URLs to block. The fewer resources a page loads, the faster it renders.

Default blocked patterns (always applied):

- `*google-analytics.com*`, `*analytics.google.com*`, `*googletagmanager.com*`
- `*googleadservices.com*`, `*googlesyndication.com*`, `*googletagservices.com*`
- `*googleapis.com*`, `*gstatic.com*`, `*googlevideo.com*`
- `*doubleclick.net*`, `*2mdn.net*`
- `*facebook.com*`, `*twitter.com*`, `*youtube.com*`
- `*hotjar.com*`, `*clarity.ms*`, `*static.cloudflareinsights.com*`
- `*paypal.com*`, `*paypalobjects.com*`, `*braintree-api.com*`, `*braintreegateway.com*`
- `*typekit.net*`, `*ampproject.org*`
- `*chatra.io*`, `*convertexperiments.com*`, `*affirm.com*`
- `*adobestats.com*`, `*adsappier.com*`, `*estorecontent.com*`
- `*lexx.me*`, `*pointandplace.com*`, `*listrakbi.com*`

Custom patterns you configure are added to the default blocked patterns above. However, when you specify blocked patterns at the host or URL pattern level, they **replace** the previous level entirely (not merge). If you override at host level, include all patterns you need - the global config patterns are replaced.

### Configuration example

::: code-group
```yaml [Global - edge-gateway.yaml]
render:
  blocked_resource_types:
    - Image
    - Media
    - Font
  blocked_patterns:
    - "*google-analytics.com*"
    - "*googletagmanager.com*"
    - "*facebook.net*"
```
```yaml [Host - example.com.yaml]
hosts:
  - id: 1
    render:
      blocked_resource_types:
        - Image
        - Media
      blocked_patterns:
        - "*custom-tracker.com*"
```
```yaml [URL pattern]
url_rules:
  - match: "/blog/*"
    action: "render"
    render:
      blocked_resource_types:
        - Image
        - Media
        - Font
        - Prefetch
      blocked_patterns:
        - "*social-widget.com*"
```
:::

## Error handling

When rendering fails due to service unavailability, timeout, or Chrome errors, Edge Gateway uses a fallback chain to ensure bots still receive content.

### Fallback behavior

1. **Stale cache**: If `serve_stale` strategy is configured and stale cache exists, serve the expired content
2. **Bypass mode**: If no stale cache is available, fetch content directly from origin without rendering

This graceful degradation ensures search engine bots always receive a response rather than errors.


## Response headers

### Safe headers

When Chrome renders a page, it captures response headers from the origin server. For security and cache efficiency, Edge Gateway filters these headers and only forwards specific "safe" headers to clients. Headers are filtered at storage time, so cached responses contain only the allowed headers.

Default safe headers:

- `Content-Type`
- `Cache-Control`
- `Expires`
- `Last-Modified`
- `ETag`
- `Location`

You can customize this list at global, host, or URL pattern level. Like other arrays, host/pattern level configurations **replace** the parent level entirely.

::: code-group
```yaml [Global - edge-gateway.yaml]
safe_headers:
  - "Content-Type"
  - "Cache-Control"
  - "X-Custom-Header"
```
```yaml [Host - example.com.yaml]
hosts:
  - id: 1
    safe_headers:
      - "Content-Type"
      - "X-App-Version"
```
```yaml [URL pattern]
url_rules:
  - match: "/api/*"
    action: "render"
    safe_headers:
      - "Content-Type"
      - "X-API-Version"
      - "X-RateLimit-Remaining"
```
:::


## Script cleaning

### strip_scripts

Controls whether executable JavaScript is removed from rendered HTML. This improves SEO by serving cleaner HTML to search engine bots while preserving structured data and non-executable content.

- **Type**: boolean
- **Default**: `true`
- **Levels**: Global, Host, URL Pattern

When enabled, removes:

- `<script>` tags with no type or executable types (`text/javascript`, `module`, `application/javascript`)
- `<link rel="import">` tags
- `<link rel="preload" as="script">` tags
- `<link rel="modulepreload">` tags

Preserves:

- `<script type="application/ld+json">` (SEO structured data)
- `<script type="application/json">` (data blocks)
- `<script type="text/template">` (templates)
- `<script type="importmap">` (ES module maps)
- Any other non-executable script type
- `<noscript>` elements (not targeted by script cleaning)

### When to disable

Set `strip_scripts: false` when you need to:

- Debug rendered pages with browser developer tools
- Capture HAR files with full script execution data
- Preserve inline scripts that use non-standard type attributes

### Configuration example

::: code-group
```yaml [Global - edge-gateway.yaml]
render:
  strip_scripts: true  # Remove executable scripts (default)
```
```yaml [Host - example.com.yaml]
hosts:
  - id: 1
    render:
      strip_scripts: true
```
```yaml [URL pattern]
url_rules:
  - match: "/app/*"
    action: "render"
    render:
      strip_scripts: false  # Keep scripts for this path
```
:::