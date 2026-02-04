---
title: Sharding
description: How Edge Gateway distributes cache across multiple instances
---

# Sharding

## Overview

Sharding distributes cached pages across multiple Edge Gateway instances. When one instance fails, others continue serving content without interruption.

For high-traffic websites, this prevents crawl budget and indexation losses during hardware or software failures.

## Service discovery

Edge Gateways register with a shared Redis instance and track each other's health through heartbeats. Instances communicate over a dedicated HTTP port for cache operations like push and pull.

Secure internal communication by:
- Blocking the internal port in your firewall
- Configuring an internal authentication key

## Distribution strategies

EdgeComet supports three strategies for selecting which instances store each cache entry. Configure with `cache_sharding.distribution_strategy`.

### hash_modulo (default)

Uses consistent hashing to distribute cache entries deterministically. The same URL always maps to the same set of instances.

**How it works:**
1. Computes XXHash64 of the cache key
2. Maps result to a primary instance index
3. Selects consecutive instances for replication (wraps around)

Use hash_modulo when you need predictable cache placement and even load distribution across the cluster.

### random

Randomly selects target instances for each cache entry. Different renders of the same URL may store on different instances.

Use random when cache churn is high and you want to spread load without deterministic placement. Trade-off: the same content may be stored on different instances over time.

### primary_only

Stores cache only on the instance that performed the render. No replication to other instances.

Use primary_only for:
- Single-instance deployments
- Development and testing
- Scenarios where replication overhead is unnecessary

All strategies ensure the rendering instance always stores the cache entry.

## Replication

### Replication factor

The `replication_factor` setting controls how many instances store each cache entry. Set to the total number of instances in your cluster to replicate everywhere.

If you set the factor higher than your actual instance count, EdgeComet automatically caps it to the cluster size. This allows you to configure once and scale your cluster without updating the factor.

Default: 2

### Push on render

When `push_on_render` is enabled (default), the rendering instance immediately pushes the cache entry to all target instances after completing the render.

**Behavior:**
- Pushes execute in parallel to all replication targets
- Runs after render completes but before sending the response to the client
- A high replication factor increases response time since all pushes must complete

Use push on render when you need immediate cache availability across the cluster. Avoid setting a high replication factor to keep response times low.

### Replicate on pull

When an instance receives a request for content it doesn't have locally, it pulls from another instance that has the cache entry.

**replicate_on_pull: true (default)**
- Stores the pulled content to local disk
- Subsequent requests served from local cache
- Gradually distributes cache as content is accessed

**replicate_on_pull: false**
- Serves pulled content from memory without storing
- Each request re-pulls from the remote instance
- Use for read-only edge nodes that shouldn't store cache locally

This lazy replication approach has no impact on render performance since replication happens only when content is requested.

## Failure handling

Sharding operations are designed to fail gracefully. Failures in cache distribution never block requests or expose errors to clients.

### Push failures

When `push_on_render` fails to reach target instances, the system makes a single attempt per target without retries. The request completes successfully with the cache stored locally, and partial failures are logged as warnings. Redis metadata is updated with only the instances that received the cache successfully.

### Pull failures

When pulling cache from remote instances fails, the system tries each healthy instance in sequence with a 3-second timeout per attempt. After exhausting all sources, it falls back to re-rendering the page. If rendering also fails, the system serves stale cache if available, or bypasses directly to the origin server.

### Missing files

When cache metadata exists in Redis but the file is missing on disk, the pull returns a 404 and is treated as a standard pull failure. The system falls back to re-rendering, and stale metadata is cleaned during the next cache daemon cycle.

### Fallback chain

The complete fallback sequence for cache requests proceeds through fresh local cache, fresh remote cache via pull, re-render, stale local cache, stale remote cache via pull, and finally bypass to origin. Each step only executes if the previous one fails.

## Inter-instance communication

Instances communicate over HTTP for push and pull operations. These settings can only be configured at the global level:

::: code-group

```yaml [edge-gateway.yaml]
# Unique identifier for this instance
eg_id: "eg-1"

# Internal server for inter-instance communication
# Block this port in your firewall from external access
internal:
  listen: "10.0.0.1:10071"
  auth_key: "your-secure-key-here"
```

:::

## Configuration example

All sharding settings can be overridden at three levels: global, host, and URL pattern.

::: code-group

```yaml [Global - edge-gateway.yaml]
cache_sharding:
  enabled: true
  replication_factor: 2
  distribution_strategy: hash_modulo
  push_on_render: true
  replicate_on_pull: true
```

```yaml [Host - hosts.yaml]
- id: 1
  domain: "example.com"
  cache_sharding:
    replication_factor: 3
    push_on_render: false
```

```yaml [URL pattern]
url_rules:
  - match: "/api/*"
    action: "render"
    cache_sharding:
      replicate_on_pull: false
```

:::

Pattern-level settings override host-level, which override global-level. Omit any setting to inherit from the parent level.
