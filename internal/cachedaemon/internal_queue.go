package cachedaemon

import (
	"sync"
	"time"

	"github.com/edgecomet/engine/internal/common/redis"
)

// Dispatch ranks of the recache priorities: a lower rank dispatches first.
const (
	rankHigh = iota
	rankNormal
	rankAutorecache
	rankCount
)

// priorityRank maps an entry's source priority to its dispatch rank. An entry without a
// priority ranks as normal, the same fallback flushInternalQueueToRedis applies.
func priorityRank(priority string) int {
	switch priority {
	case redis.PriorityHigh:
		return rankHigh
	case redis.PriorityAutorecache:
		return rankAutorecache
	default:
		return rankNormal
	}
}

// waitingCounts holds one host's entries that are ready to dispatch, indexed by dispatch rank.
type waitingCounts [rankCount]int

// atOrAbove returns how many waiting entries rank the same as priority or ahead of it.
func (w waitingCounts) atOrAbove(priority string) int {
	n := 0
	for rank := 0; rank <= priorityRank(priority); rank++ {
		n += w[rank]
	}
	return n
}

// InternalQueueEntry represents a recache task in the daemon's internal queue
type InternalQueueEntry struct {
	HostID         int
	URL            string
	DimensionID    int
	Mode           string // Optional action override: render | bypass (empty = respect config)
	Priority       string // Source Redis priority (high | normal | autorecache); used for shutdown flush and metrics
	RetryCount     int
	QueuedAt       time.Time
	LastAttempt    time.Time
	NextRetryAfter time.Time

	// LastErrorType and LastErrorMessage retain how the edge gateway classified the most
	// recent failed attempt. A dispatch-level failure (no healthy EG, panic) carries no
	// classification, so without this the terminal discard would report whichever error
	// happened last rather than the diagnosis an EG actually made.
	LastErrorType    string
	LastErrorMessage string
}

// inBackoff reports whether the entry is still waiting out a retry delay at now.
func (e InternalQueueEntry) inBackoff(now time.Time) bool {
	return !e.NextRetryAfter.IsZero() && now.Before(e.NextRetryAfter)
}

// InternalQueue is a thread-safe in-memory queue for recache tasks
type InternalQueue struct {
	mu      sync.RWMutex
	entries []InternalQueueEntry
	maxSize int
}

// NewInternalQueue creates a new internal queue with the specified maximum size
func NewInternalQueue(maxSize int) *InternalQueue {
	return &InternalQueue{
		entries: make([]InternalQueueEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Enqueue adds an entry to the queue, respecting maxSize limit
// Returns true if entry was added, false if queue is full
func (q *InternalQueue) Enqueue(entry InternalQueueEntry) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) >= q.maxSize {
		return false
	}

	q.entries = append(q.entries, entry)
	return true
}

// Dequeue removes and returns up to count entries from the queue
func (q *InternalQueue) Dequeue(count int) []InternalQueueEntry {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) == 0 {
		return nil
	}

	if count > len(q.entries) {
		count = len(q.entries)
	}

	result := make([]InternalQueueEntry, count)
	copy(result, q.entries[:count])
	q.entries = q.entries[count:]

	return result
}

// Size returns the current number of entries in the queue
func (q *InternalQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return len(q.entries)
}

// CountByHostID returns the number of entries in the queue for a specific host
func (q *InternalQueue) CountByHostID(hostID int) int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	count := 0
	for _, entry := range q.entries {
		if entry.HostID == hostID {
			count++
		}
	}
	return count
}

// CountsByHostID returns the per-host entry counts in a single pass.
// Used by the scheduler's per-host skip-on-defer logic so it doesn't have to
// call CountByHostID once per host (O(N hosts × queue size) vs O(queue size)).
func (q *InternalQueue) CountsByHostID() map[int]int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	counts := make(map[int]int, 8)
	for _, entry := range q.entries {
		counts[entry.HostID]++
	}
	return counts
}

// waitingCountsByHostID returns, per host, the entries ready to dispatch at now, split by
// dispatch rank. Entries still in a retry backoff are left out: they make no claim on a
// slot until the backoff ends.
func (q *InternalQueue) waitingCountsByHostID(now time.Time) map[int]waitingCounts {
	q.mu.RLock()
	defer q.mu.RUnlock()

	counts := make(map[int]waitingCounts, 8)
	for _, entry := range q.entries {
		if entry.inBackoff(now) {
			continue
		}
		c := counts[entry.HostID]
		c[priorityRank(entry.Priority)]++
		counts[entry.HostID] = c
	}
	return counts
}
