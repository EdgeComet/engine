package cachedaemon

import (
	"sync"
	"sync/atomic"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/pkg/types"
)

// DefaultMaxConcurrent is the compiled-in fallback for per-host recache concurrency
// when neither global EG config nor per-host override specifies a limit.
const DefaultMaxConcurrent = 5

// hostCounters tracks per-host atomic stats. Counters survive reload so that
// monotonic totals remain monotonic across configuration changes.
type hostCounters struct {
	inFlight      int64
	acquiredTotal uint64
	deniedTotal   uint64
	maxCapacity   int64
}

// Slot is the token returned by TryAcquire. It carries the channel reference
// the slot was acquired from so Release operates on that exact channel even
// if the host's channel was replaced by a concurrent Reload. Release is
// idempotent: extra Release calls on the same Slot are no-ops, guaranteed by
// a per-Slot sync.Once that is shared across copies via the pointer field.
type Slot struct {
	ch       chan struct{}
	counters *hostCounters
	once     *sync.Once
}

// HostConcurrencyLimiter enforces a per-host cap on concurrent recache requests
// using a buffered channel as a semaphore. One channel per host; capacity is
// the maximum in-flight recache requests for that host.
type HostConcurrencyLimiter struct {
	mu                   sync.RWMutex
	defaultMaxConcurrent int
	overrides            map[int]int
	slots                map[int]chan struct{}
	counters             map[int]*hostCounters
}

// HostConcurrencyStats is a point-in-time snapshot of a host's limiter state.
type HostConcurrencyStats struct {
	MaxConcurrent int    `json:"max_concurrent"`
	InFlight      int64  `json:"in_flight"`
	AcquiredTotal uint64 `json:"acquired_total"`
	DeniedTotal   uint64 `json:"denied_total"`
}

// NewHostConcurrencyLimiter constructs a limiter from EG config and host list.
// Channels are created lazily on first TryAcquire to avoid allocating capacity
// for hosts that never recache.
func NewHostConcurrencyLimiter(eg *configtypes.EgConfig, hosts []types.Host) *HostConcurrencyLimiter {
	l := &HostConcurrencyLimiter{
		defaultMaxConcurrent: resolveDefaultMaxConcurrent(eg),
		overrides:            buildOverrides(hosts),
		slots:                make(map[int]chan struct{}),
		counters:             make(map[int]*hostCounters),
	}
	return l
}

// TryAcquire attempts to take a slot for hostID without blocking.
// Returns (slot, true) on success; (zero, false) if no slot available.
// The caller MUST pass the returned slot to Release. Multiple Release calls
// on the same Slot are safe — the second and subsequent calls are no-ops.
//
// The channel send runs under a read lock so a concurrent Reload (write lock)
// cannot swap the host's channel between lookup and send. Without the lock,
// inFlight could transiently exceed the new max_concurrent until pre-reload
// slots drained on the old channel.
func (l *HostConcurrencyLimiter) TryAcquire(hostID int) (Slot, bool) {
	// Fast path: entry already exists.
	l.mu.RLock()
	if ch, ok := l.slots[hostID]; ok {
		if counters, ok := l.counters[hostID]; ok {
			slot, acquired := trySend(ch, counters)
			l.mu.RUnlock()
			return slot, acquired
		}
	}
	l.mu.RUnlock()

	// Slow path: lazily create the entry.
	l.mu.Lock()
	ch, counters := l.createLocked(hostID)
	slot, acquired := trySend(ch, counters)
	l.mu.Unlock()
	return slot, acquired
}

// trySend performs the non-blocking semaphore send and updates counters.
// Both branches work without the limiter lock as long as the caller serialises
// channel-replacement (Reload) against this call.
func trySend(ch chan struct{}, counters *hostCounters) (Slot, bool) {
	select {
	case ch <- struct{}{}:
		atomic.AddInt64(&counters.inFlight, 1)
		atomic.AddUint64(&counters.acquiredTotal, 1)
		return Slot{ch: ch, counters: counters, once: &sync.Once{}}, true
	default:
		atomic.AddUint64(&counters.deniedTotal, 1)
		return Slot{}, false
	}
}

// Release returns a previously acquired slot. Idempotent: the first call
// drains the captured channel and decrements the counter; subsequent calls
// are no-ops. Safe to call with a zero Slot.
func (l *HostConcurrencyLimiter) Release(s Slot) {
	if s.once == nil {
		return
	}
	s.once.Do(func() {
		<-s.ch
		atomic.AddInt64(&s.counters.inFlight, -1)
	})
}

// Reload updates the limiter's configuration from new EG config and hosts.
// Channels are replaced only when the effective capacity for a host changes.
// Counters are preserved so monotonic totals remain monotonic across reloads.
// Goroutines holding pre-reload Slots release on the old channel (no-op on
// the new one); the old channel is GC'd after all references drop.
func (l *HostConcurrencyLimiter) Reload(eg *configtypes.EgConfig, hosts []types.Host) {
	newDefault := resolveDefaultMaxConcurrent(eg)
	newOverrides := buildOverrides(hosts)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.defaultMaxConcurrent = newDefault
	l.overrides = newOverrides

	for hostID, ch := range l.slots {
		newCap := l.effectiveLocked(hostID)
		if cap(ch) == newCap {
			continue
		}
		l.slots[hostID] = make(chan struct{}, newCap)
		if c, ok := l.counters[hostID]; ok {
			atomic.StoreInt64(&c.maxCapacity, int64(newCap))
		}
	}
}

// Stats returns a point-in-time snapshot for hostID. Returns a zero-value
// snapshot if the host has no activity recorded yet.
func (l *HostConcurrencyLimiter) Stats(hostID int) HostConcurrencyStats {
	l.mu.RLock()
	c, ok := l.counters[hostID]
	maxCap := l.effectiveLocked(hostID)
	l.mu.RUnlock()

	if !ok {
		return HostConcurrencyStats{MaxConcurrent: maxCap}
	}
	return HostConcurrencyStats{
		MaxConcurrent: int(atomic.LoadInt64(&c.maxCapacity)),
		InFlight:      atomic.LoadInt64(&c.inFlight),
		AcquiredTotal: atomic.LoadUint64(&c.acquiredTotal),
		DeniedTotal:   atomic.LoadUint64(&c.deniedTotal),
	}
}

// MaxConcurrent returns the effective max-concurrent limit for hostID
// (per-host override -> global config value -> DefaultMaxConcurrent).
// Hot-path version of Stats(hostID).MaxConcurrent that avoids allocating
// a HostConcurrencyStats struct.
func (l *HostConcurrencyLimiter) MaxConcurrent(hostID int) int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.effectiveLocked(hostID)
}

// AllStats returns a snapshot keyed by host_id for every host the limiter
// has tracked since startup (including hosts removed from config).
func (l *HostConcurrencyLimiter) AllStats() map[int]HostConcurrencyStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := make(map[int]HostConcurrencyStats, len(l.counters))
	for hostID, c := range l.counters {
		out[hostID] = HostConcurrencyStats{
			MaxConcurrent: int(atomic.LoadInt64(&c.maxCapacity)),
			InFlight:      atomic.LoadInt64(&c.inFlight),
			AcquiredTotal: atomic.LoadUint64(&c.acquiredTotal),
			DeniedTotal:   atomic.LoadUint64(&c.deniedTotal),
		}
	}
	return out
}

// createLocked returns the channel and counters for hostID, creating them
// if they don't exist. Caller must hold l.mu (write lock).
// The channel capacity is the effective max_concurrent
// (override → global → DefaultMaxConcurrent).
func (l *HostConcurrencyLimiter) createLocked(hostID int) (chan struct{}, *hostCounters) {
	if ch, ok := l.slots[hostID]; ok {
		if c, ok := l.counters[hostID]; ok {
			return ch, c
		}
	}
	capN := l.effectiveLocked(hostID)
	ch := make(chan struct{}, capN)
	l.slots[hostID] = ch
	c, ok := l.counters[hostID]
	if ok {
		atomic.StoreInt64(&c.maxCapacity, int64(capN))
	} else {
		c = &hostCounters{maxCapacity: int64(capN)}
		l.counters[hostID] = c
	}
	return ch, c
}

// effectiveLocked returns the effective max_concurrent for hostID.
// Caller must hold l.mu (read or write).
func (l *HostConcurrencyLimiter) effectiveLocked(hostID int) int {
	if v, ok := l.overrides[hostID]; ok && v > 0 {
		return v
	}
	return l.defaultMaxConcurrent
}

// resolveDefaultMaxConcurrent picks the global default from EG config or
// falls back to the compiled-in DefaultMaxConcurrent.
func resolveDefaultMaxConcurrent(eg *configtypes.EgConfig) int {
	if eg != nil && eg.Recache != nil && eg.Recache.MaxConcurrent > 0 {
		return eg.Recache.MaxConcurrent
	}
	return DefaultMaxConcurrent
}

// buildOverrides extracts per-host max_concurrent overrides into a map.
func buildOverrides(hosts []types.Host) map[int]int {
	m := make(map[int]int, len(hosts))
	for _, h := range hosts {
		if h.Recache != nil && h.Recache.MaxConcurrent > 0 {
			m[h.ID] = h.Recache.MaxConcurrent
		}
	}
	return m
}
