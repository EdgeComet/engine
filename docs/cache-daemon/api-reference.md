---
title: API reference
description: Cache Daemon HTTP API endpoints
---

# API reference

## Authentication

All endpoints require the `X-Internal-Auth` header with the configured internal authentication key.

```
X-Internal-Auth: your-internal-auth-key
```

Unauthorized requests return 401 status code.

## Endpoints

### Recache URLs

Queue URLs for rendering. Use this to manually trigger cache refresh for specific URLs.

#### Request

**Method:** `POST`
**Path:** `/internal/cache/recache`
**Headers:** `X-Internal-Auth`, `Content-Type: application/json`

**Body parameters:**

```json
{
  "host_id": 1,
  "urls": ["https://example.com/page1", "https://example.com/page2"],
  "dimension_ids": [1, 2],
  "priority": "high",
  "mode": "render"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `host_id` | integer | Yes | Host identifier from hosts configuration |
| `urls` | array of strings | Yes | URLs to recache (1-10000 entries) |
| `dimension_ids` | array of integers | No | Dimension IDs to recache (empty = all dimensions) |
| `priority` | string | Yes | Queue priority: `"high"` or `"normal"` |
| `mode` | string | No | Action override: `"render"` forces a Chrome render (stored as render cache), `"bypass"` forces an origin fetch (stored as bypass cache). Omit to respect the configured dimension/url-rule action. |

The `mode` override lets you precache against the configured action. For example, a bypass-mode
host can render selected URLs with `"mode": "render"` so bots are served the rendered HTML, while
a render-mode host can warm already-server-rendered URLs cheaply with `"mode": "bypass"`. A
`"bypass"` precache never overwrites a fresh render record for the same URL (render-wins
precedence).

#### Response

**Success (200):**

```json
{
  "success": true,
  "data": {
    "host_id": 1,
    "urls_count": 2,
    "dimension_ids_count": 2,
    "entries_enqueued": 4,
    "priority": "high",
    "paused": false
  }
}
```

`paused` is `true` when recache draining is paused for the host. Enqueueing still succeeds
while a host is paused - the URLs are accepted and stored, and the daemon starts working
through them when the pause is lifted or expires. Treat `entries_enqueued` together with
`paused`: entries were queued, but nothing is about to be fetched.

**Error responses:**
- `400` - Invalid JSON, missing required fields, invalid priority, invalid mode, host not found, dimension not configured
- `401` - Unauthorized (invalid X-Internal-Auth)

#### Example

```bash
curl -X POST http://localhost:10090/internal/cache/recache \
  -H "X-Internal-Auth: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "host_id": 1,
    "urls": ["https://example.com/"],
    "dimension_ids": [1],
    "priority": "high"
  }'
```

---

### Purge recache queue

Drop every queued entry for a host on the selected priorities. Use this to undo a bulk
submit, or to clear a backlog that is hammering an origin.

Purge removes queued work. Requests the daemon has already picked up will still complete:
the entries the scheduler pulled out of Redis before the purge landed are dispatched and
answered as normal, and no ordering of purge and pause prevents that. Expect a small tail
of origin requests after a purge returns.

#### Request

**Method:** `POST`
**Path:** `/internal/cache/queue/purge`
**Headers:** `X-Internal-Auth`, `Content-Type: application/json`

**Body parameters:**

```json
{
  "host_id": 1,
  "priorities": ["high", "normal"]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `host_id` | integer | Yes | Host identifier from hosts configuration |
| `priorities` | array of strings | No | Queues to purge: `"high"`, `"normal"`, `"autorecache"`. Omitted or `[]` means `["high", "normal"]` |

An explicit empty array means the default `["high", "normal"]`, not "purge nothing".

`autorecache` is never purged unless you name it. That queue is earned state - every entry
is a real bot hit with a real refresh time - so wiping it means those URLs will not refresh
until a bot visits them again, possibly days later. Name it only when you mean it.

Priority names are matched exactly and in lower case. `"HIGH"` is rejected. Duplicates are
harmless: the second pass over the same queue finds it already empty and counts nothing.

#### Response

**Success (200):**

```json
{
  "success": true,
  "data": {
    "entries_purged": 8412
  }
}
```

`entries_purged` counts the entries removed from the durable queues, summed across the
requested priorities.

**Error responses:**
- `400` - Invalid JSON, missing `host_id`, host not found, unknown or wrongly cased priority
- `401` - Unauthorized
- `500` - Redis error during the purge (the response body carries no partial count; the daemon logs how many entries were removed before the failure)

#### Example

```bash
curl -X POST http://localhost:10090/internal/cache/queue/purge \
  -H "X-Internal-Auth: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "host_id": 1,
    "priorities": ["high"]
  }'
```

---

### Pause recache for a host

Stop the daemon from pulling new recache work for one host. Use this when a host's origin
is struggling and you want background traffic to stop without losing the work list.

**Pause stops draining only.** New work is still accepted while a host is paused:
`POST /internal/cache/recache` succeeds and stores the URLs, and bot-hit autorecache
scheduling keeps adding entries to the autorecache queue. Nothing is pulled out of those
queues until the host is resumed or the pause expires.

**The pause auto-expires after 3 hours.** This is a compiled-in bound, not a configuration
setting, so a forgotten pause cannot silently stop a host's precache indefinitely. Pausing
an already-paused host overwrites the deadline with a fresh 3 hours - the `expires_at` in
the response is what is now stored.

#### How quickly the host actually goes quiet

Pausing stops new pulls immediately. It does not cancel work that is already past that
point, and that tail is measured in minutes, not seconds:

- roughly `2 x max_concurrent` entries are already past the gate when the pause lands
  (in flight plus pulled and waiting), which is about 10 entries at the default
  `recache.max_concurrent` of 5
- each of those entries gets up to `internal_queue.max_retries` attempts (default 3), with
  a backoff between attempts (default 5s, then 10s)
- each attempt can run for up to `recache.timeout_per_url` (default 60s)

A worst-case entry therefore occupies the origin for about 60 + 5 + 60 + 10 + 60 seconds,
roughly three minutes, before it is discarded. If you need the origin quiet right now,
pause is not sufficient on its own - take the origin out of rotation.

#### Known limitation: a render host starved of render-service capacity

Pause does not stop origin requests from a render host that has no render-service capacity
to dispatch into. Such a host accumulates entries that were already pulled but cannot be
sent, and those entries neither dispatch nor age out. Pausing the host stops further pulls,
but the accumulated entries remain. The moment render-service capacity returns they can
burst out to the origin - possibly while the host is still nominally paused.

This matters because it correlates badly: a host whose renders are backing up is exactly
the host an operator reaches for pause on. If you hit this, restart the daemon after
pausing, or take the origin out of rotation instead.

#### If the pause cannot be read

The daemon reads the pause state once per scheduler tick. If that read fails while Redis is
otherwise reachable, the daemon logs an error and keeps draining for that tick rather than
stalling every host. A pause can therefore be briefly ignored during a partial Redis
outage. It takes effect again on the next tick that reads successfully.

#### Pause state and cluster moves

The pause is stored in the Redis of the cluster whose daemon you called, so it applies only
to that cluster. Moving a host to another cluster leaves the pause behind: the host drains
immediately in its new cluster, and the old cluster's stale entry is cleaned up when it
expires. Pause the host again in the new cluster if you still want it held.

#### Request

**Method:** `POST`
**Path:** `/internal/cache/recache/pause`
**Headers:** `X-Internal-Auth`, `Content-Type: application/json`

**Body parameters:**

```json
{
  "host_id": 1
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `host_id` | integer | Yes | Host identifier from hosts configuration |

This endpoint is always available. Unlike `/internal/scheduler/pause`, which stops the whole
daemon and is gated behind `scheduler_control_api`, a per-host pause affects one host and
needs no configuration flag.

#### Response

**Success (200):**

```json
{
  "success": true,
  "data": {
    "paused": true,
    "expires_at": 1755630000
  }
}
```

`expires_at` is the unix timestamp at which the host starts draining again by itself.

**Error responses:**
- `400` - Invalid JSON, missing `host_id`, host not found
- `401` - Unauthorized
- `500` - Redis error while storing the pause

#### Example

```bash
curl -X POST http://localhost:10090/internal/cache/recache/pause \
  -H "X-Internal-Auth: your-key" \
  -H "Content-Type: application/json" \
  -d '{"host_id": 1}'
```

---

### Resume recache for a host

Clear a pause. Draining restarts on the next scheduler tick.

Resuming a host that is not paused succeeds and changes nothing, so it is safe to call
without checking first.

Nothing is ramped on resume: the whole queue becomes eligible again at once, throttled only
by the host's `recache.max_concurrent` and the render-service capacity reservation. After a
long pause on a large host, that is a burst at the origin.

#### Request

**Method:** `POST`
**Path:** `/internal/cache/recache/resume`
**Headers:** `X-Internal-Auth`, `Content-Type: application/json`

**Body parameters:**

```json
{
  "host_id": 1
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `host_id` | integer | Yes | Host identifier from hosts configuration |

#### Response

**Success (200):**

```json
{
  "success": true,
  "data": {
    "paused": false
  }
}
```

**Error responses:**
- `400` - Invalid JSON, missing `host_id`, host not found
- `401` - Unauthorized
- `500` - Redis error while clearing the pause

#### Example

```bash
curl -X POST http://localhost:10090/internal/cache/recache/resume \
  -H "X-Internal-Auth: your-key" \
  -H "Content-Type: application/json" \
  -d '{"host_id": 1}'
```
---

### Invalidate cache

Delete cache metadata for specific URLs. Filesystem cleanup happens in background.

#### Request

**Method:** `POST`
**Path:** `/internal/cache/invalidate`
**Headers:** `X-Internal-Auth`, `Content-Type: application/json`

**Body parameters:**

```json
{
  "host_id": 1,
  "urls": ["https://example.com/page1"],
  "dimension_ids": [1, 2]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `host_id` | integer | Yes | Host identifier from hosts configuration |
| `urls` | array of strings | Yes | URLs to invalidate |
| `dimension_ids` | array of integers | No | Dimension IDs to invalidate (empty = all dimensions) |

#### Response

**Success (200):**

```json
{
  "success": true,
  "data": {
    "host_id": 1,
    "urls_count": 1,
    "dimension_ids_count": 2,
    "entries_invalidated": 2
  }
}
```

**Error responses:**
- `400` - Invalid JSON, missing required fields, host not found, dimension not configured
- `401` - Unauthorized

#### Example

```bash
curl -X POST http://localhost:10090/internal/cache/invalidate \
  -H "X-Internal-Auth: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "host_id": 1,
    "urls": ["https://example.com/old-page"],
    "dimension_ids": []
  }'
```

---

### Status

Get daemon health, queue status, and render service capacity.

#### Request

**Method:** `GET`
**Path:** `/status`
**Headers:** `X-Internal-Auth`

#### Response

**Success (200):**

```json
{
  "daemon": {
    "daemon_id": "cache-daemon-01",
    "uptime_seconds": 3600,
    "last_tick": "2025-01-18T10:30:00Z"
  },
  "internal_queue": {
    "size": 150,
    "max_size": 1000,
    "capacity_used_percent": 15.0
  },
  "rs_capacity": {
    "total_free_tabs": 10,
    "reserved_for_online": 5,
    "available_for_recache": 5,
    "reservation_percent": 50.0
  },
  "queues": {
    "1": {
      "high": {"total": 10, "due_now": 5},
      "normal": {"total": 50, "due_now": 20},
      "autorecache": {"total": 100, "due_now": 30},
      "paused": false
    }
  }
}
```

**Fields:**
- `daemon.daemon_id` - Daemon identifier
- `daemon.uptime_seconds` - Uptime in seconds
- `daemon.last_tick` - Last scheduler tick timestamp
- `internal_queue.size` - Current internal queue size
- `internal_queue.max_size` - Maximum queue capacity
- `internal_queue.capacity_used_percent` - Queue usage percentage
- `rs_capacity.total_free_tabs` - Total available render service tabs
- `rs_capacity.reserved_for_online` - Tabs reserved for online traffic
- `rs_capacity.available_for_recache` - Tabs available for recaching
- `rs_capacity.reservation_percent` - Reservation percentage
- `queues` - Queue status per host (keyed by host_id)
- `queues[host_id].high` - High priority queue status
- `queues[host_id].normal` - Normal priority queue status
- `queues[host_id].autorecache` - Autorecache queue status
- `queues[host_id].paused` - Whether recache draining is paused for the host. The queues keep accepting work while this is true; the daemon just is not pulling from them
- `total` - Total entries in queue
- `due_now` - Entries ready to process

#### Example

```bash
curl -X GET http://localhost:10090/status \
  -H "X-Internal-Auth: your-key"
```

---

### Pause scheduler

Pause the recache scheduler. Requires `scheduler_control_api: true` in configuration.

#### Request

**Method:** `POST`
**Path:** `/internal/scheduler/pause`
**Headers:** `X-Internal-Auth`

#### Response

**Success (200):**

```json
{
  "success": true,
  "message": "Scheduler paused"
}
```

**Error responses:**
- `401` - Unauthorized
- `403` - Scheduler control API not enabled

#### Example

```bash
curl -X POST http://localhost:10090/internal/scheduler/pause \
  -H "X-Internal-Auth: your-key"
```

---

### Resume scheduler

Resume the recache scheduler. Requires `scheduler_control_api: true` in configuration.

#### Request

**Method:** `POST`
**Path:** `/internal/scheduler/resume`
**Headers:** `X-Internal-Auth`

#### Response

**Success (200):**

```json
{
  "success": true,
  "message": "Scheduler resumed"
}
```

**Error responses:**
- `401` - Unauthorized
- `403` - Scheduler control API not enabled

#### Example

```bash
curl -X POST http://localhost:10090/internal/scheduler/resume \
  -H "X-Internal-Auth: your-key"
```

---

## Error handling

All endpoints return JSON error responses with consistent format:

```json
{
  "status": "error",
  "message": "error description"
}
```

**Common HTTP status codes:**
- `200` - Success
- `400` - Bad request (validation errors)
- `401` - Unauthorized (missing or invalid X-Internal-Auth)
- `403` - Forbidden (feature not enabled)
- `404` - Not found (invalid endpoint)
