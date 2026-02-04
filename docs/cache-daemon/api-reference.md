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
  "priority": "high"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `host_id` | integer | Yes | Host identifier from hosts configuration |
| `urls` | array of strings | Yes | URLs to recache (1-10000 entries) |
| `dimension_ids` | array of integers | No | Dimension IDs to recache (empty = all dimensions) |
| `priority` | string | Yes | Queue priority: `"high"` or `"normal"` |

#### Response

**Success (200):**

```json
{
  "status": "ok",
  "data": {
    "host_id": 1,
    "urls_count": 2,
    "dimension_ids_count": 2,
    "entries_enqueued": 4,
    "priority": "high"
  }
}
```

**Error responses:**
- `400` - Invalid JSON, missing required fields, invalid priority, host not found, dimension not configured
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
  "status": "ok",
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
      "autorecache": {"total": 100, "due_now": 30}
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
  "status": "ok",
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
  "status": "ok",
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
