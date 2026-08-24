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

Two further `wait_for` values are not lifecycle events at all. `prerenderContentReady` and `prerenderReady` wait for a JavaScript property the page sets on itself once its content is in the DOM. They apply only to applications built around that contract, and they change how redirects and not-found URLs render. See [Readiness property wait](#readiness-property-wait).

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

**A URL pattern timeout does not apply to debug or preview renders.** The HAR debug endpoint renders with the host timeout, or with the explicit `timeout` parameter when one is given, and Edge SEO preview renders with the host timeout. Every other render setting a pattern carries - `wait_for`, `additional_wait`, blocked patterns, blocked resource types, `scroll.enabled` - is resolved for the URL exactly as a production render resolves it. So a pattern that raises the timeout for a slow section keeps rendering that section longer in production than a preview does, and the preview can capture an earlier stage of the page. Pass `timeout` explicitly on the debug endpoint to reproduce the pattern value.

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

## Readiness property wait

Some single-page applications resolve their content lazily and never reach a quiet network at a moment when the page is actually complete. Frameworks built to be captured by a bot renderer solve this with a contract: the application announces that it has finished by setting a property on `window`, and the renderer waits for that property instead of inferring readiness from network activity.

Two `wait_for` values select that wait:

- `prerenderContentReady` (preferred)
- `prerenderReady`

Neither is a Chrome lifecycle event. The renderer polls the page for the named property and captures HTML as soon as the property is truthy.

### window.isPrerender

Selecting either value also marks the page as being captured rather than browsed: the renderer sets `window.isPrerender = true` before any of the page's own scripts run. This is not separately configurable, and it is the half of the contract that carries the feature.

An application that implements this contract reads the flag during bootstrap and never reads it again. A flag that arrives after the page's first script changes nothing, and without the flag the application stays in its ordinary mode: its lazily resolved sections never resolve and it never sets a readiness property at all.

The flag is set for the whole page, same-origin iframes included, and only for the render that asked for it. It does not carry into the next render on the same browser.

### Which value to prefer

Prefer `prerenderContentReady`. Where an application sets both, the two are not equivalent.

`prerenderReady` is usually wired into application state and is not monotonic: a route change sets it back to `false` after it has been true. Nothing within a single render recovers from that. The value can be true when the renderer looks and false a moment later, and which of the two a render sees is a race.

`prerenderContentReady` is normally guarded so that it is set once and never reset, and it is set from the event that fires when lazily resolved components have finished resolving. It also tends to fire slightly later, which is the point: it describes content that is in the DOM rather than a state machine reaching a step.

Confirm which properties an application actually sets before configuring either one.

### Parked redirects

The wait also ends as soon as `window.prerenderRedirectUrl` holds a non-empty value, whichever comes first.

An application that has seen the flag does not navigate. When it decides that a URL belongs somewhere else - a redirect, or a not-found - it parks the destination in that property and stops. Without this exit, such a URL would sit on its loading shell until the render timeout expired, on every crawl. Applications set the parked URL early, well ahead of any readiness signal, so leaving on it costs a render nothing.

The parked URL wins over readiness within the same check. An application can park a redirect and then go on to build a page anyway; when it does, the content it builds belongs to the destination rather than to the URL being rendered.

`additional_wait` is not paid on this exit. It is settling time for a page that reported its content was in the DOM, which a parked page never does, and the only thing such a page can build while the render sleeps is the destination's content - the content the exit exists to keep out of the capture.

The parked URL is recorded on the render metrics, and appears as `prerenderRedirectUrl` in a debug HAR. It is what separates a captured loading shell from a page that genuinely had nothing to render.

### Redirects and not-found URLs change behaviour

This is the constraint to resolve before switching a host.

Because the application no longer navigates, a URL that would have redirected and a URL that would have served a not-found page both render a near-empty loading shell instead of the destination. Nothing about that shell marks it as a failure, so it is served as a 200 and cached as a 200 for the configured TTL.

Have origin status handling in place for those URLs before moving a host to a readiness `wait_for` value. On a host where every crawled URL renders content, the wait is a straight improvement; on a host with redirects or soft not-found pages, it is not.

### Timeout budget

A readiness wait is far more likely to run its full length than a lifecycle wait. Lifecycle events fire on almost every page, while a property an application has stopped setting - after a bundle upgrade, a refactor, a renamed flag - never fires at all.

Keep the host `render.timeout` below the Render Service `chrome.render.max_timeout`, with room to spare. The render timeout is soft and still yields partial HTML; the hard timeout is not, and cancels the render outright with no HTML at all. When the two are equal, a wait that runs its full length leaves nothing for HTML extraction and the render ends as a 504 instead of as a usable partial capture. The Render Service logs a warning when it receives a request in that state.

### Configuration example

::: code-group
```yaml [Host - example.com.yaml]
hosts:
  - id: 1
    render:
      # Below the Render Service max_timeout, so an application that stops
      # signalling still yields a partial capture rather than a 504.
      timeout: 30s
      events:
        wait_for: "prerenderContentReady"
        # Paid only after a readiness signal - never after a timeout, never
        # after a parked redirect - and the signal is the point.
        additional_wait: 0s
```
```yaml [URL pattern]
url_rules:
  - match: "/catalog/*"
    action: "render"
    render:
      events:
        wait_for: "prerenderContentReady"
```
:::

### What to watch after switching

- The readiness property appears in `lifecycleEvents` in a debug HAR, alongside `load` and `networkIdle`. It is recorded like an event, so an application that stopped setting it shows up as a missing entry rather than only as a rise in timeouts.
- Timeouts should not rise. A rise means URLs are reaching the render timeout, which on such a host usually means either that the property is no longer set or that those URLs are parking a redirect the render is not leaving on.

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

## Scroll to load lazy content

Many sites mount sections only after the visitor scrolls, using an IntersectionObserver or a scroll listener. A renderer that never scrolls captures the page without those sections, and no amount of extra waiting recovers them: the content is gated on a scroll event, not on elapsed time. The symptom is a cached page that is missing a footer, a table, or a large part of the internal link graph, while the same page looks complete in a browser.

### scroll.enabled

Scrolls the page to the bottom before HTML capture, pausing between steps so lazily mounted sections have time to load, and returns to the top before capturing.

- **Type**: boolean
- **Default**: `false`
- **Levels**: Global, Host, URL Pattern

The pass walks the page itself first, following it as it grows, and only turns to an inner container once the page has nothing left to give.

It never guesses which element is the page scroller. Both `document.scrollingElement` and `body` are scrolled on every step: on any given layout exactly one of them is the real scroll container, and scrolling the other is a no-op. This matters because sites that set `html { overflow: hidden }` and scroll `body` are common, and on those `window.scrollTo` does nothing at all - while on an ordinary page writing to `body.scrollTop` does nothing either.

Two behaviours follow from the page growing while the pass runs:

- the pass does not stop the first time it reaches the bottom. It stops after the page has stayed at its bottom with no new height and no new links for several steps, so a section that arrives a beat late is still captured
- steps travel one viewport by default, and grow larger when the document is getting taller faster than the remaining budget can walk. Without that, a page that keeps expanding is never walked to its end and anything anchored to its bottom - typically the SEO footer - never mounts

An inner container gets whatever budget is left after the page settles, scrolled back into view first, and bounded so that a virtualised list cannot consume the whole pass. Pages whose document does not scroll at all go straight to that container.

### Cost

Scrolling adds seconds to every render of a matching URL:

- roughly 2.4s on a page with nothing to load, since the pass confirms the page is at its bottom and stable before it stops
- a few seconds more on a page that also has an inner container worth scrolling
- up to 12s on a page that keeps producing content, which is the point at which the pass gives up and captures what it has

That is why it is off by default and why it belongs on the hosts that need it rather than globally. Two further consequences to expect on a scroll-enabled host:

- cached HTML and cache files grow, sometimes substantially on feed-like pages
- request counts, transferred bytes, and per-domain statistics rise, because scrolling triggers the requests the lazy sections make. Those numbers stop being comparable with hosts that do not scroll.

### Timeout budget

The scroll spends wall clock inside the Render Service hard timeout. Keep `chrome.render.max_timeout` above the host `render.timeout` plus 12s, otherwise a scrolling render can be cancelled outright and return 504 with no HTML. The Render Service logs a warning when it receives a request whose scroll budget does not fit.

### Configuration example

::: code-group
```yaml [Global - edge-gateway.yaml]
render:
  scroll:
    enabled: false  # Default
```
```yaml [Host - example.com.yaml]
hosts:
  - id: 1
    render:
      # Raise the render timeout together with the flag: the scroll runs after the
      # lifecycle wait, and max_timeout has to cover both.
      timeout: 25s
      scroll:
        enabled: true
```
```yaml [URL pattern]
url_rules:
  - match: "/catalog/*"
    action: "render"
    render:
      scroll:
        enabled: true  # Only the pages that lazy-load pay the cost
```
:::

### When it does not help

Two outcomes are worth watching for, both logged by the Render Service as warnings naming the URL.

**Nothing was scrollable.** The pass re-checks for a couple of seconds before concluding this, since an early start only means the page has not laid out yet. A warning means the page genuinely fits in the viewport, or its layout is one the pass does not recognise. There is no automatic fallback.

**The bottom was never reached.** The pass ran out of budget partway down. The capture is usable but anything anchored to the bottom of the page is missing from it, which is exactly the symptom this feature exists to fix. Raise the host `render.timeout` if the page is merely slow; a page that grows without end will always end this way.

Both are counted in the Render Service metrics: `edgecomet_rs_scroll_outcomes_total` carries the stop reason and whether the bottom was reached, and `edgecomet_rs_scroll_no_scroller_total` counts the first case.
