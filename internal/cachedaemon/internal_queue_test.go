package cachedaemon

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInternalQueue_BasicOperations(t *testing.T) {
	queue := NewInternalQueue(10)

	t.Run("new queue is empty", func(t *testing.T) {
		assert.Equal(t, 0, queue.Size())
	})

	t.Run("enqueue and dequeue single entry", func(t *testing.T) {
		entry := InternalQueueEntry{
			HostID:      1,
			URL:         "https://example.com",
			DimensionID: 1,
			RetryCount:  0,
			QueuedAt:    time.Now(),
		}

		ok := queue.Enqueue(entry)
		assert.True(t, ok)
		assert.Equal(t, 1, queue.Size())

		dequeued := queue.Dequeue(1)
		require.Len(t, dequeued, 1)
		assert.Equal(t, entry.URL, dequeued[0].URL)
		assert.Equal(t, 0, queue.Size())
	})

	t.Run("dequeue from empty queue returns nil", func(t *testing.T) {
		emptyQueue := NewInternalQueue(5)
		result := emptyQueue.Dequeue(10)
		assert.Nil(t, result)
	})

	t.Run("dequeue more than available returns all", func(t *testing.T) {
		testQueue := NewInternalQueue(10)
		for i := 0; i < 3; i++ {
			testQueue.Enqueue(InternalQueueEntry{
				HostID: i + 1,
				URL:    "https://example.com",
			})
		}

		result := testQueue.Dequeue(10)
		assert.Len(t, result, 3)
	})

	t.Run("respects max size", func(t *testing.T) {
		smallQueue := NewInternalQueue(2)

		ok1 := smallQueue.Enqueue(InternalQueueEntry{URL: "url1"})
		assert.True(t, ok1)

		ok2 := smallQueue.Enqueue(InternalQueueEntry{URL: "url2"})
		assert.True(t, ok2)

		// Queue is full
		ok3 := smallQueue.Enqueue(InternalQueueEntry{URL: "url3"})
		assert.False(t, ok3)
		assert.Equal(t, 2, smallQueue.Size())
	})
}

func TestInternalQueue_ConcurrentEnqueue(t *testing.T) {
	queue := NewInternalQueue(1000)
	numGoroutines := 100
	entriesPerGoroutine := 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < entriesPerGoroutine; j++ {
				entry := InternalQueueEntry{
					HostID:      goroutineID,
					URL:         "https://example.com",
					DimensionID: j,
					QueuedAt:    time.Now(),
				}
				queue.Enqueue(entry)
			}
		}(i)
	}

	wg.Wait()

	// All entries should have been enqueued
	expectedSize := numGoroutines * entriesPerGoroutine
	assert.Equal(t, expectedSize, queue.Size())
}

func TestInternalQueue_ConcurrentDequeue(t *testing.T) {
	queue := NewInternalQueue(1000)

	// Pre-populate queue
	totalEntries := 500
	for i := 0; i < totalEntries; i++ {
		queue.Enqueue(InternalQueueEntry{
			HostID: i,
			URL:    "https://example.com",
		})
	}

	numGoroutines := 50
	entriesPerDequeue := 5

	var wg sync.WaitGroup
	var mu sync.Mutex
	totalDequeued := 0

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			dequeued := queue.Dequeue(entriesPerDequeue)
			mu.Lock()
			totalDequeued += len(dequeued)
			mu.Unlock()
		}()
	}

	wg.Wait()

	// All dequeued entries + remaining entries should equal original total
	remaining := queue.Size()
	assert.Equal(t, totalEntries, totalDequeued+remaining)
}

func TestInternalQueue_MixedConcurrentOperations(t *testing.T) {
	queue := NewInternalQueue(500)
	duration := 100 * time.Millisecond

	var wg sync.WaitGroup

	// Enqueuers
	numEnqueuers := 20
	wg.Add(numEnqueuers)
	for i := 0; i < numEnqueuers; i++ {
		go func(id int) {
			defer wg.Done()
			deadline := time.Now().Add(duration)
			for time.Now().Before(deadline) {
				queue.Enqueue(InternalQueueEntry{
					HostID: id,
					URL:    "https://example.com",
				})
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Dequeuers
	numDequeuers := 10
	wg.Add(numDequeuers)
	for i := 0; i < numDequeuers; i++ {
		go func() {
			defer wg.Done()
			deadline := time.Now().Add(duration)
			for time.Now().Before(deadline) {
				queue.Dequeue(5)
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	// Size readers
	numReaders := 10
	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			deadline := time.Now().Add(duration)
			for time.Now().Before(deadline) {
				_ = queue.Size()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Verify queue is still functional
	finalSize := queue.Size()
	assert.GreaterOrEqual(t, finalSize, 0)
	assert.LessOrEqual(t, finalSize, 500) // Should not exceed max size
}

func TestInternalQueue_MaxSizeEnforcement(t *testing.T) {
	maxSize := 100
	queue := NewInternalQueue(maxSize)

	var wg sync.WaitGroup
	numGoroutines := 50
	attemptsPerGoroutine := 10

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < attemptsPerGoroutine; j++ {
				queue.Enqueue(InternalQueueEntry{
					HostID: id,
					URL:    "https://example.com",
				})
			}
		}(i)
	}

	wg.Wait()

	// Queue should never exceed max size
	finalSize := queue.Size()
	assert.LessOrEqual(t, finalSize, maxSize)
}

func TestInternalQueue_FIFO_Order(t *testing.T) {
	queue := NewInternalQueue(10)

	// Enqueue entries in order
	for i := 0; i < 5; i++ {
		queue.Enqueue(InternalQueueEntry{
			HostID: i,
			URL:    "https://example.com",
		})
	}

	// Dequeue and verify FIFO order
	result := queue.Dequeue(5)
	require.Len(t, result, 5)

	for i := 0; i < 5; i++ {
		assert.Equal(t, i, result[i].HostID, "Expected FIFO order")
	}
}
