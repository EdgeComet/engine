package cachedaemon

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// pauseTTL bounds an operator pause so a forgotten one cannot silently stop a host's
// recache indefinitely. Not configurable: there is no per-deploy reason to differ.
const pauseTTL = 3 * time.Hour

// PauseHost stops the scheduler from pulling new recache work for hostID and returns the
// unix time the pause lifts by itself. Pausing an already-paused host overwrites the stored
// expiry with a fresh now+pauseTTL. Enqueueing stays available while paused: the operator's
// goal is to stop hitting the origin, not to lose the work list.
func (d *CacheDaemon) PauseHost(ctx context.Context, hostID int) (int64, error) {
	expiresAt := time.Now().UTC().Add(pauseTTL).Unix()

	// Formatted explicitly rather than passed as an int64: the sweep compares the stored
	// string byte for byte, so its encoding must not depend on the Redis client's.
	value := strconv.FormatInt(expiresAt, 10)
	if err := d.redis.HSet(ctx, d.keyGenerator.RecachePausedKey(), strconv.Itoa(hostID), value); err != nil {
		return 0, fmt.Errorf("failed to store recache pause for host %d: %w", hostID, err)
	}
	return expiresAt, nil
}

// ResumeHost clears any pause for hostID; draining restarts on the next tick. Deliberately
// not value-guarded like the expiry sweep: an explicit resume must win over whatever expiry
// is stored. Resuming a host that is not paused succeeds and changes nothing.
func (d *CacheDaemon) ResumeHost(ctx context.Context, hostID int) error {
	if err := d.redis.HDel(ctx, d.keyGenerator.RecachePausedKey(), strconv.Itoa(hostID)); err != nil {
		return fmt.Errorf("failed to clear recache pause for host %d: %w", hostID, err)
	}
	return nil
}

// PausedHosts returns the still-paused host IDs mapped to their unix expiry. One HGETALL
// serves the whole cluster, and expiry is decided in Go against the caller's nowUnix so
// every host within a tick shares one reference point. Redis cannot expire a hash field, so
// fields that have run out are swept here.
//
// The walk is over the stored fields, not over the configured host set. That is what lets
// the sweep reach fields left behind by a host that has since moved to another cluster and
// is no longer configured here.
func (d *CacheDaemon) PausedHosts(ctx context.Context, nowUnix int64) (map[int]int64, error) {
	key := d.keyGenerator.RecachePausedKey()

	fields, err := d.redis.HGetAll(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to read recache pause state: %w", err)
	}
	if len(fields) == 0 {
		return nil, nil
	}

	paused := make(map[int]int64, len(fields))
	for field, value := range fields {
		hostID, hostErr := strconv.Atoi(field)
		expiresAt, expiryErr := strconv.ParseInt(value, 10, 64)
		if hostErr != nil || expiryErr != nil {
			// A field nobody can interpret pauses nothing and can never expire, so
			// leaving it in place would leak it forever.
			d.logger.Warn("Removing unusable recache pause field",
				zap.String("field", field),
				zap.String("value", value))
			d.sweepPauseField(ctx, key, field, value)
			continue
		}

		if expiresAt > nowUnix {
			paused[hostID] = expiresAt
			continue
		}

		// Every daemon sharing this Redis observes the same expiry and logs its own
		// copy of this line; only one of them wins the sweep.
		swept := d.sweepPauseField(ctx, key, field, value)
		d.logger.Info("Recache pause expired, host resumes draining",
			zap.Int("host_id", hostID),
			zap.Int64("expired_at", expiresAt),
			zap.Bool("swept_by_this_daemon", swept))
	}
	return paused, nil
}

// PauseExpiry returns the unix expiry of hostID's pause, or 0 when it is not paused. Reads
// the single field instead of the whole hash: the API responses that carry pause state
// alongside a queue answer only ever care about one host. An expired or unusable value
// reads as not paused and is left for the scheduler tick to sweep.
func (d *CacheDaemon) PauseExpiry(ctx context.Context, hostID int, nowUnix int64) (int64, error) {
	value, err := d.redis.HGet(ctx, d.keyGenerator.RecachePausedKey(), strconv.Itoa(hostID))
	if err != nil {
		return 0, fmt.Errorf("failed to read recache pause for host %d: %w", hostID, err)
	}
	if value == "" {
		return 0, nil
	}

	expiresAt, err := strconv.ParseInt(value, 10, 64)
	if err != nil || expiresAt <= nowUnix {
		return 0, nil
	}
	return expiresAt, nil
}

// sweepPauseField removes a pause field only while it still holds value, so an operator who
// re-pauses the host between the read and this call keeps the fresh pause. Best-effort: a
// failed sweep leaves the field for the next tick, and the in-Go expiry check has already
// treated the host as resumed.
func (d *CacheDaemon) sweepPauseField(ctx context.Context, key, field, value string) bool {
	removed, err := d.redis.HDelIfValue(ctx, key, field, value)
	if err != nil {
		d.logger.Error("Failed to sweep expired recache pause field",
			zap.String("field", field),
			zap.Error(err))
		return false
	}
	return removed
}

// pauseExpiryForResponse resolves a host's pause expiry for an API response, reporting a
// read failure as "not paused". A queue or enqueue answer must not fail because the pause
// hash was briefly unreadable.
func (d *CacheDaemon) pauseExpiryForResponse(ctx context.Context, hostID int) int64 {
	expiresAt, err := d.PauseExpiry(ctx, hostID, time.Now().UTC().Unix())
	if err != nil {
		d.logger.Error("Failed to read recache pause state for response",
			zap.Int("host_id", hostID),
			zap.Error(err))
		return 0
	}
	return expiresAt
}
