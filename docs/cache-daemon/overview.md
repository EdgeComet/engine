# Cache Daemon - Overview

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

CD processes queues in priority order: priority first, then normal, then autorecache. Only URLs with a scheduled time in the past are picked for processing. This ensures urgent updates happen first while background recaching continues at a lower priority.



## Rate Limiting

CD monitors Render Service capacity before scheduling recache tasks. It queries Redis for the current RS load and available Chrome tabs. The rs_capacity_reserved setting (e.g., 0.30) reserves a percentage of total capacity for real-time rendering. CD only uses the remaining capacity for background recaching. If capacity is insufficient, URLs remain in the queue for later processing. This prevents background recaching from degrading production traffic performance.


## Cache Invalidation

Cache invalidation is available through POST /internal/cache/invalidate API endpoint. You provide host ID, URLs to invalidate, and optionally dimension IDs. The system removes cache metadata from Redis immediately, forcing Edge Gateway to render fresh content on the next bot request.

Filesystem cleanup is a separate automatic operation performed by EG's cleanup worker. It removes orphaned HTML files after their TTL plus a configured safety margin expires.

For proactive cache updates, use POST /internal/cache/recache endpoint to add URLs to priority or normal queues. Unlike invalidation, this schedules re-rendering without waiting for the next bot visit.


## Autorecache Integration

Large websites can have hundreds of thousands or even millions of pages. Keeping all rendered versions up to date would consume significant resources and money. However, Googlebot and AI bots don't crawl all pages. Monthly crawl ratios vary from 10-20% up to 60-70% depending on site size and structure.

Autorecache leverages this pattern. When a bot hits cached content, EG checks if the User-Agent matches configured bot patterns (bothit_recache.match_ua). If matched, EG adds the URL to the autorecache queue in Redis with a scheduled time based on the configured interval (e.g., 24h from now). CD periodically scans this queue, picks URLs that are due, and sends them for rendering.

This approach keeps fresh cached versions only for pages that bots actually visit, saving resources by ignoring pages that aren't being crawled.


## Configuration

CD maintains its own configuration file separate from EG. The eg_config setting points to EG's configuration file, allowing CD to load host definitions and understand available hosts and their dimension settings.

All communication with EG instances uses the shared internal_auth_key for authentication. CD and EG must use the same Redis instance for service discovery and queue coordination.