# Edge Gateway



## Purpose

Edge Gateway (EG) is the starting point of request processing. It works as a middle layer of a website's HTTP server or CDN. By intercepting requests from Googlebots, AI bots, and rendering page content, it returns Server Side Rendered HTML back to the bot.

It manages all the logic from validating the payload to rendering a page, caching it, and pushing through a sharding system.

EG uses Redis to discover Rendeding Services (RS)  and other EGs for replication. HTML cache itself is stored in the filesystem with metadata in Redis.



## Reliability

EG is designed for graceful degradation. When Redis fails, it falls back to bypass mode, fetching content directly from the origin. When Render Services are unavailable, it serves stale cache (if configured) or bypasses to the origin. When the filesystem is corrupted, it pulls cache from other EG nodes or bypasses.



## Resources consumption

Working is a high-traffic environment, the EG is built to consume as few system resources as possible. Usually, it runs under 50MB RAM and doesn’t require much CPU. Cache files are served directly from the filesystem and can easily withstand thousands of RPS.





## Render and Bypass modes

Render mode is the default action. In this mode, EG sends the URL to a Render Service instance, which opens it in headless Chrome and captures the fully rendered HTML.

Bypass serves two purposes. First, as explicit configuration for parts of a website that don't need rendering: for eg, /api/* endpoints, or already server-rendered pages. Second, as an automatic fallback when rendering fails: no available render services, render timeout, Chrome errors, or unmatched User-Agent with `unmatched_dimension: "bypass"`.

Render configuration includes event waiting behavior. The `wait_for` field specifies when Chrome considers the page ready: `DOMContentLoaded` (DOM ready, resources may still load), `load` (all resources loaded), `networkIdle` (recommended), or `networkAlmostIdle`. The `additional_wait` field adds extra delay after the event fires, useful for JavaScript that executes after network activity stops. Both settings are configurable at all three levels: global, host, and URL pattern.



## Device dimension

When a website needs to be rendered differently for mobile and desktop user agents, dimensions determine how EG renders pages. Each dimension specifies viewport size (width and height), User-Agent string sent to Chrome during rendering, and patterns to match incoming request User-Agents. When a bot accesses EG, the system matches its User-Agent against dimension patterns to determine which viewport to use.

Also, it’s a way to manage a rendering behaviour, for example, Googlebot and AI bots' requests will be rendered, while any other bots will be bypassed or simply blocked.



## Caching

EG stores rendered HTML in the filesystem with metadata in Redis. Cache configuration operates on three levels (global, host, pattern) with deep merge semantics. You configure TTL, cacheable status codes (default: 200, 301, 302, 307, 308, 404), and expiration strategy per URL pattern.

When the cache expires, two strategies are available:

* `delete` strategy removes expired cache metadata immediately, forcing fresh renders.
* `serve_stale` strategy keeps serving expired content for a configurable period while attempting fresh renders. If rendering fails due to service unavailability, timeouts, or errors, the system serves stale cache as a fallback rather than returning errors to bots.

Bot hit recache automatically schedules re-renders when search engine bots access cached content. You configure bot User-Agent patterns and recache interval. When a bot hits the cache, EG queues the URL for automatic re-rendering.

Cache invalidation works through the `/internal/cache/invalidate` API endpoint. You provide host ID, URLs, and optionally dimension IDs. The system deletes Redis metadata immediately while filesystem cleanup runs as a background worker, removing files after TTL plus a configurable safety margin.



## Sharding

Cache sharding distributes rendered HTML across multiple EG instances for storage scalability and high availability. Each EG registers itself in Redis with a unique identifier and heartbeat. Instances discover each other automatically through Redis keys.

The system supports three distribution strategies:

* `hash_modulo` (default) uses XXHash64 to map URLs deterministically to primary EG instances. The same URL always routes to the same primary node, making cache location predictable.
* `random` shuffles healthy EGs for load balancing but sacrifices determinism.
* `primary_only` disables distribution entirely, storing cache only on the rendering EG.

Main configuration includes `replication_factor` (number of copies per cache entry, default 2), `push_on_render` (proactively replicate to target EGs after rendering), and `replicate_on_pull` (store pulled cache locally or serve from memory only). 
All inter-EG communication uses a shared `internal_auth_key` for authentication.

When an EG renders a page, it saves locally and pushes to replica EGs in parallel via HTTP POST. When a cache miss occurs locally but Redis metadata shows other EGs have it, the system pulls from a remote peer. This provides redundancy while distributing storage load across the cluster.



## Configuration

EG uses a three-level configuration hierarchy: _global_ (edge-gateway.yaml), _host_ (hosts.d/*.yaml), and _URL pattern_ (`url_rules` within hosts). Settings merge from global to host to pattern, with each level overriding specific fields from its parent. 
Scalar values like `timeout` and `ttl` override directly. Array values like `blocked_resource_types` and `safe_headers` replace the parent entirely rather than appending.

Each host defines a domain, render key for authentication, and optional overrides. Hosts inherit global dimensions and settings when not explicitly defined. Within hosts, URL rules specify behavior for specific paths using pattern matching.

URL patterns support three matching types. Exact patterns (no special characters) match paths case-insensitively. Wildcard patterns use * to match any characters recursively (`/blog/*` matches `/blog/post` and `/blog/2024/jan/post`). 
Regexp patterns use `~` prefix for case-sensitive matching or `~*` for case-insensitive (`~*\\.pdf$` matches any PDF file).

Query parameter matching extends this with the match_query field. 
Multiple parameters use AND logic between keys and OR logic within array values. 
The same pattern types apply: exact values, wildcards (`*` means non-empty), and regexp patterns.

User-Agent aliases provide predefined bot patterns. Prefix with `$` to expand: `$GooglebotSearchDesktop` expands to multiple exact and regexp patterns covering known Googlebot variants. 
This simplifies dimension configuration and ensures comprehensive bot coverage without manual pattern maintenance.



## Monitoring

EG exposes Prometheus metrics on a dedicated port for monitoring request rates, cache hit ratios, render latencies, 
and service health. 
Each response includes diagnostic headers: `X-Request-ID` for tracing, `X-Render-Source` indicating content origin (`rendered`, `cache`, `bypass`, `bypass_cache`),
and `X-Cache-Age` showing time since caching.

These headers enable debugging without log access. Operators can trace request flow, identify cache behavior, and diagnose rendering issues directly from HTTP responses.