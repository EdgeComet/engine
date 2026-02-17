package cachedaemon

import (
	"sync"
	"time"
)

// InternalQueueEntry represents a recache task in the daemon's internal queue
type InternalQueueEntry struct {
	HostID         int
	URL            string
	DimensionID    int
	RetryCount     int
	QueuedAt       time.Time
	LastAttempt    time.Time
	NextRetryAfter time.Time
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
