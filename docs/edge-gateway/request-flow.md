---
title: Request flow
description: How Edge Gateway processes incoming requests from receipt to response
---

# Request flow

When a request arrives at Edge Gateway, it first extracts or generates a unique request ID for tracing. 
The target URL is extracted from the query parameter and validated for proper format and scheme. 
Authentication follows by checking the `X-Render-Key` header against configured hosts.

EG then analyzes the `User-Agent` header to detect device dimension (desktop, mobile, or custom patterns) 
and resolves the final configuration by matching the URL path against patterns and merging settings from global, host, and pattern levels. 
The action determines the next step: status codes return immediately, bypass fetches from origin directly, and render continues to cache logic.

For render actions, EG checks Redis for fresh cache. On cache hit, it serves the content immediately. 
On cache miss, it acquires a distributed lock to prevent duplicate renders across the cluster. 
If another request holds the lock, EG waits and polls for cache. 
Once the lock is acquired, EG selects a render service instance, reserves a Chrome tab, and sends the render request.

After rendering completes, EG stores the HTML in the filesystem, updates Redis metadata, 
and optionally replicates to other EG nodes. If rendering fails, it falls back to serving stale cache 
or bypassing directly to the origin.

![Request flow overview](/images/request-flow.svg)

### Common fallback triggers

| Trigger | Fallback Path |
|---------|---------------|
| **No render services available** | Stale cache → Bypass |
| **Render timeout** | Stale cache → Bypass |
| **Render 5xx error** | Stale cache → Serve 5xx |
| **All EG replicas down** | Fresh render → Stale cache → Bypass |
| **Redis unavailable** | Bypass |
| **Lock wait timeout** | Stale cache → Bypass |


## Cache sharding architecture

Edge Gateway supports distributed cache sharding across multiple instances for high availability and load distribution.


### Replication strategies

**Configuration: `sharding.push_on_render`**

When a cache entry is created (after rendering):

![Cache push](/images/replication-push.svg)


**Configuration: `sharding.replicate_on_pull`**

When a cache entry is requested on an EG that doesn't have the file:

![Cache pull](/images/replication-pull.svg)


