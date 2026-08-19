package cachedaemon

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
)

// luaPurgeQueue counts and removes one recache queue in a single step, so the returned count
// is exactly what was dropped rather than a ZCARD that a concurrent pull can invalidate.
//
// UNLINK, not DEL: a bulk submit enqueues one member per URL x dimension and autorecache is
// bounded only by distinct URL x dimension, so these ZSETs reach millions of members on a
// large host. DEL is O(N) and synchronous while Lua holds the whole Redis event loop, and the
// edge gateways read cache metadata from that same Redis.
const luaPurgeQueue = `
local n = redis.call('ZCARD', KEYS[1])
redis.call('UNLINK', KEYS[1])
return n
`

// recachePriorities lists every recache queue a host has, in priority order. The single
// source of truth for the daemon's own priority allow-lists, which - unlike the cluster
// manager's - accept autorecache.
var recachePriorities = []string{redis.PriorityHigh, redis.PriorityNormal, redis.PriorityAutorecache}

// recachePrioritySet is recachePriorities in the lookup shape the request filters need.
var recachePrioritySet = buildPrioritySet(recachePriorities)

func buildPrioritySet(priorities []string) map[string]bool {
	set := make(map[string]bool, len(priorities))
	for _, priority := range priorities {
		set[priority] = true
	}
	return set
}

// defaultPurgePriorities is what an omitted or empty priorities list resolves to. Autorecache
// is left out because every entry there is a real bot hit with a real refresh time: wiping it
// means those URLs do not refresh until the next bot hit, possibly days later. Naming it is
// the opt-in.
func defaultPurgePriorities() []string {
	return []string{redis.PriorityHigh, redis.PriorityNormal}
}

// resolvePurgePriorities validates a requested priority list and substitutes the default set
// when nothing was requested. Matching is exact and lowercase, like the other priority
// filters. Duplicates need no handling: purging the same key twice counts zero the second time.
func resolvePurgePriorities(requested []string) ([]string, error) {
	if len(requested) == 0 {
		return defaultPurgePriorities(), nil
	}

	for _, priority := range requested {
		if !recachePrioritySet[priority] {
			return nil, fmt.Errorf("invalid priority %q, must be one of: %s",
				priority, strings.Join(recachePriorities, ", "))
		}
	}
	return requested, nil
}

// PurgeRecacheQueues drops every queued entry for hostID on the named priorities and returns
// how many were removed. Only the durable Redis queues are touched: entries the scheduler has
// already pulled into the internal queue, or already dispatched, still complete. On failure
// the count of what was purged before the error is returned alongside it.
func (d *CacheDaemon) PurgeRecacheQueues(ctx context.Context, hostID int, priorities []string) (int, error) {
	total := 0

	for _, priority := range priorities {
		key := d.keyGenerator.RecacheQueueKey(hostID, priority)

		result, err := d.redis.Eval(ctx, luaPurgeQueue, []string{key})
		if err != nil {
			return total, fmt.Errorf("failed to purge recache queue %s: %w", key, err)
		}
		purged, ok := result.(int64)
		if !ok {
			return total, fmt.Errorf("unexpected purge result type %T for recache queue %s", result, key)
		}

		total += int(purged)
		d.logger.Info("Recache queue purged",
			zap.Int("host_id", hostID),
			zap.String("priority", priority),
			zap.String("key", key),
			zap.Int64("entries_purged", purged))
	}

	return total, nil
}
