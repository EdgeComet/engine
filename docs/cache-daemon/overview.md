---
title: Cache Daemon - Overview
description: Pre-caching for the open-source EdgeComet engine - automatic, bot-triggered recaching that keeps frequently crawled pages fresh on idle capacity.
---

# Cache Daemon - Overview

Pre-caching is a first-class capability of the open-source EdgeComet engine, and the Cache Daemon provides it. Instead of rendering every page on every bot visit, it keeps frequently crawled pages fresh in the background, using only idle capacity so it never harms real-time performance.

Introduction to the Cache Daemon and automatic recaching functionality.

## What is Cache Daemon?
Cache daemon (CD) is an optional application in the system to manage automatic background cache updates, expose recache and invalidation API for an end user.
CD does not render pages itself. It observes the load of the Chrome pool and sends recache requests to EG instances. The goal is to use only free idle resources, so it does not harm the real-time rendering performance.



## Service Discovery

Cache Daemon discovers Edge Gateway instances through Redis registry, using the same mechanism that EGs use to find each other. CD queries registered EGs and distributes recache requests across them according to the configured sharding strategy. This automatic discovery means CD adapts to cluster changes without manual configuration updates.


## Queues
CD maintains three queue types as Redis sorted sets, each with URLs scored by scheduled execution time:

- Priority queue - For immediate rendering. Useful for tests and emergency updates for critical pages. Limited to 1000 URLs to prevent abuse.
- Normal queue - General cache updating queue for bulk recache operations.
- Autorecache queue - Populated automatically by EG when bots hit cached content. URLs are scheduled based on configured intervals.

Two operator controls act on these queues per host: purge drops what is queued, and pause stops the drain for up to three hours while still accepting new work. Both act on the Redis queues only, so requests the daemon has already picked up still complete. See the API reference for the tail this leaves and for the render-host case where pause does not stop origin requests.

## Drain loop

CD runs a unified drain on every `scheduler.tick_interval`. On each tick the scheduler walks all configured hosts and, per host, pulls at most one priority's worth of work in strict order: `high` first, then `normal`, then due `autorecache` (entries scheduled at or before "now"). Strict priority within a host is preserved by stopping at the first non-empty priority — a host's `normal` items wait only for that host's `high` to drain, never for an unrelated host's backlog.

The drain has three guardrails:

- **Rotating host cursor.** The host scan starts at a per-daemon cursor that advances by the number of hosts visited each iteration. When the internal queue fills before every host has been visited, the next iteration resumes at the host after the last one visited, so a heavy backlog at the front of the list can never starve later hosts.
- **Durability pre-check.** Before popping from Redis, CD checks the host's free concurrency slots. If none are free, no items are pulled — the backlog stays in durable Redis instead of piling up in the volatile in-memory queue. As soon as slots release, the next iter pulls.
- **Empty-host-list panic guard.** When the host list is empty (startup before the host loader populates, or every host removed), the drain loop is skipped but the tick-end `ProcessInternalQueue` still runs so retry/backoff items continue to dispatch.

Throughput is governed by the per-host `max_concurrent` cap and average render time: roughly `max_concurrent / avg_render_time` requests per second per host. `tick_interval` is an idle re-scan interval, not a throughput knob.

Within a host, `high` can still starve `normal`/`autorecache` if it is continuously fed — standard priority-queue behaviour. Operators size `max_concurrent` accordingly and route batch work that should not preempt other priorities through `normal` rather than `high`.

### Operator note: backoff backpressure during outages

`internal_queue.max_size` is load-bearing for the daemon's durability vs crash-loss tradeoff. Pulled items leave durable Redis and live in the internal queue until dispatch; on a daemon crash they are lost. During an EG or RS outage, dispatch failures re-enqueue entries onto the internal queue with exponential backoff. If the outage persists, deferred entries accumulate and the queue's free space shrinks, slowing fresh Redis pulls — by design, since pulling more would risk overflowing the volatile queue and dropping work via `logQueueFullDrop`. The expected operator observation in this state is: Redis ZSET counts grow while iq stays near capacity. This is durability backpressure, not a stuck drain — once the outage clears, the backoff entries drain (success or `max_retries` exceeded) and Redis catches up. If you regularly see drops, raise `internal_queue.max_size` for the outage envelope you want to absorb.



## Rate Limiting

CD monitors Render Service capacity before scheduling recache tasks. It queries Redis for the current RS load and available Chrome tabs. The rs_capacity_reserved setting (e.g., 0.30) reserves a percentage of total capacity for real-time rendering. CD only uses the remaining capacity for background recaching. If capacity is insufficient, URLs remain in the queue for later processing. This prevents background recaching from degrading production traffic performance.


## Cache Invalidation

Cache invalidation is available through POST /internal/cache/invalidate API endpoint. You provide host ID, URLs to invalidate, and optionally dimension IDs. The system removes cache metadata from Redis immediately, forcing Edge Gateway to render fresh content on the next bot request.

Filesystem cleanup is a separate automatic operation performed by EG's cleanup worker. It removes orphaned HTML files after their TTL plus a configured safety margin expires.

For proactive cache updates, use POST /internal/cache/recache endpoint to add URLs to priority or normal queues. Unlike invalidation, this schedules re-rendering without waiting for the next bot visit.


## Autorecache Integration

Large websites can have hundreds of thousands or even millions of pages. Keeping all rendered versions up to date would consume significant resources and money. However, Googlebot and AI bots don't crawl all pages. Monthly crawl ratios vary from 10-20% up to 60-70% depending on site size and structure.

Autorecache leverages this pattern. When a bot hits cached content, EG checks if the User-Agent matches configured bot patterns (bothit_recache.match_ua). If matched, EG adds the URL to the autorecache queue in Redis with a scheduled time based on the configured interval (e.g., 24h from now). CD scans this queue on every tick and picks URLs whose scheduled time is at or before now (the "due" filter is enforced by clamping `ZPOPMIN` to the count returned by `ZCOUNT(-inf, now)`).

This approach keeps fresh cached versions only for pages that bots actually visit, saving resources by ignoring pages that aren't being crawled.


## Configuration

CD maintains its own configuration file separate from EG. The eg_config setting points to EG's configuration file, allowing CD to load host definitions and understand available hosts and their dimension settings.

All communication with EG instances uses the shared internal_auth_key for authentication. CD and EG must use the same Redis instance for service discovery and queue coordination.