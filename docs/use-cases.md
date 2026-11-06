---
title: When to use the engine
description: Who the open-source EdgeComet engine is for, what it solves on your own servers, and when it is not the right fit.
---

# When to use the EdgeComet engine

Use the EdgeComet engine when your team wants to own its stack: bot caching,
pre-caching, and JavaScript rendering on your own servers, without a cloud
subscription. It is the same engine EdgeComet cloud runs in production. See
[The engine and EdgeComet cloud](./index.md#the-engine-and-edgecomet-cloud).

## What the engine solves

- **Fast bot responses on any stack**: on a cache hit, Edge Gateway serves
  prepared HTML from the filesystem in under 15ms, however your origin performs.
  This works for JavaScript apps, CMS sites, and static sites alike.
- **Crawl load on large sites**: bots get cached pages instead of each request
  reaching your origin, which lets them cover more pages in the same window.
  With the bypass cache turned on, [bypass mode](./edge-gateway/bypass-mode.md)
  caches your server's HTML without rendering for paths that do not need it.
- **Fresh cache without full re-renders**: the [Cache
  Daemon](./cache-daemon/overview.md) recaches frequently crawled pages on idle
  capacity, without re-rendering the whole site.
- **JavaScript rendering**: Render Service runs single-page apps built with
  React, Vue, or Angular in headless Chrome. Bots that do not run JavaScript get
  the complete page, with its metadata and structured data.
- **Separate mobile and desktop copies**:
  [dimensions](./edge-gateway/dimensions.md) keep one cache entry per device
  profile and choose it by the bot's user agent.
- **Several servers**: [sharding](./edge-gateway/sharding.md) distributes cached
  pages across Edge Gateway instances, and the others keep serving when one
  fails.

## What you run yourself

You operate every part of a self-hosted deployment: Redis, the Chrome pool on
Render Service machines, the Linux servers, the routing rule in your CDN or
proxy, and monitoring through Prometheus metrics. See [System
requirements](./index.md#system-requirements) and [Deployment
topology](./getting-started/topology.md).

## When the engine is not the right fit

- **Real-time or per-user content**: a shared bot cache cannot serve pages that
  change on every request or for every user.
- **No need for caching or rendering**: if your origin already serves bots
  complete HTML quickly and crawl load is not a concern, the engine adds little.
  Pages with no JavaScript-dependent content do not need rendering, though
  caching can still reduce crawl load on large sites.
- **Bot analytics, a site audit, or Edge SEO**: the engine caches and renders,
  and these features are part of EdgeComet cloud. See [The engine and EdgeComet
  cloud](./index.md#the-engine-and-edgecomet-cloud).
