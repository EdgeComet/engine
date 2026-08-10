package cachedaemon

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

// collectDrops installs a recording sink and returns an accessor for what it received.
func collectDrops(d *CacheDaemon) func() []PrecacheDrop {
	var drops []PrecacheDrop
	d.SetPrecacheDropSink(func(drop PrecacheDrop) { drops = append(drops, drop) })
	return func() []PrecacheDrop { return drops }
}

// TestPrecacheDropSink_MaxRetriesCarriesLastEGError: an exhausted entry is reported with the
// edge gateway's own diagnosis, not with whichever dispatch-level error came last. Covering
// markEntryFailed covers both of its callers.
func TestPrecacheDropSink_MaxRetriesCarriesLastEGError(t *testing.T) {
	const hostID, dimID = 1, 1
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	drops := collectDrops(env.daemon)

	entry := InternalQueueEntry{
		HostID:           hostID,
		URL:              "https://h.test/exhausted",
		DimensionID:      dimID,
		RetryCount:       env.daemon.daemonConfig.InternalQueue.MaxRetries - 1,
		LastErrorType:    types.ErrorTypeOrigin5xx,
		LastErrorMessage: "origin returned uncacheable status 503",
	}

	_, retry := env.daemon.markEntryFailed(entry, fmt.Errorf("no healthy edge gateways available"))

	require.False(t, retry)
	require.Len(t, drops(), 1)
	assert.Equal(t, PrecacheDrop{
		URL:          "https://h.test/exhausted",
		HostID:       hostID,
		DimensionID:  dimID,
		ErrorType:    dropErrorTypeMaxRetries,
		ErrorMessage: "origin_5xx: origin returned uncacheable status 503",
	}, drops()[0])
}

// TestPrecacheDropSink_MaxRetriesWithoutEGDiagnosis: when no edge gateway ever answered there is
// no classification to prefer, so the dispatch-level cause is reported instead.
func TestPrecacheDropSink_MaxRetriesWithoutEGDiagnosis(t *testing.T) {
	const hostID, dimID = 1, 1
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	drops := collectDrops(env.daemon)

	entry := InternalQueueEntry{
		HostID:      hostID,
		URL:         "https://h.test/never-answered",
		DimensionID: dimID,
		RetryCount:  env.daemon.daemonConfig.InternalQueue.MaxRetries - 1,
	}

	_, retry := env.daemon.markEntryFailed(entry, fmt.Errorf("no healthy edge gateways available"))

	require.False(t, retry)
	require.Len(t, drops(), 1)
	assert.Equal(t, dropErrorTypeMaxRetries, drops()[0].ErrorType)
	assert.Equal(t, "no healthy edge gateways available", drops()[0].ErrorMessage)
}

// TestPrecacheDropSink_UnresolvedHost: the cluster-move case. The entry names a host this daemon
// no longer knows, so it is discarded at the gate and reported as such.
func TestPrecacheDropSink_UnresolvedHost(t *testing.T) {
	const knownHost, unknownHost, dimID = 1, 99, 1
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: knownHost, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	drops := collectDrops(env.daemon)

	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      unknownHost,
		URL:         "https://gone.test/p",
		DimensionID: dimID,
		QueuedAt:    time.Now().UTC(),
	}))

	env.daemon.ProcessInternalQueue()

	require.Len(t, drops(), 1)
	assert.Equal(t, PrecacheDrop{
		URL:          "https://gone.test/p",
		HostID:       unknownHost,
		DimensionID:  dimID,
		ErrorType:    dropErrorTypeUnresolvedHost,
		ErrorMessage: unresolvedHostMessage,
	}, drops()[0])
	assert.Equal(t, 0, env.daemon.internalQueue.Size())
}

// TestPrecacheDropSink_FiresUnderReloadReadLock proves the documented sink contract is a real
// constraint rather than defensive prose: the unresolved-host drop is emitted while the gate
// loop holds reloadMu for read, so a sink that re-entered the daemon's locking would deadlock
// the whole tick as soon as a reload queued for the write lock.
func TestPrecacheDropSink_FiresUnderReloadReadLock(t *testing.T) {
	const knownHost, unknownHost, dimID = 1, 99, 1
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: knownHost, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})

	// Buffered so the send completes without another goroutine, which is what the contract
	// requires of every implementation.
	dropped := make(chan PrecacheDrop, 1)
	readLockHeld := make(chan bool, 1)
	env.daemon.SetPrecacheDropSink(func(drop PrecacheDrop) {
		if env.daemon.reloadMu.TryLock() {
			env.daemon.reloadMu.Unlock()
			readLockHeld <- false
		} else {
			readLockHeld <- true
		}
		dropped <- drop
	})

	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      unknownHost,
		URL:         "https://gone.test/p",
		DimensionID: dimID,
		QueuedAt:    time.Now().UTC(),
	}))

	env.daemon.ProcessInternalQueue()

	assert.True(t, <-readLockHeld, "the write lock must be unavailable, proving the gate loop's read lock is held")
	assert.Equal(t, dropErrorTypeUnresolvedHost, (<-dropped).ErrorType)
}

// TestPrecacheDropSink_QueueOverflowAtGate: the internal queue is full when a deferred entry is
// put back, so the entry is lost. The reason it was being deferred is the only thing separating
// the overflow drops from one another, so it must survive into the event.
func TestPrecacheDropSink_QueueOverflowAtGate(t *testing.T) {
	const hostID, dimID = 1, 1
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	drops := collectDrops(env.daemon)

	// Two entries in backoff against a queue with room for one. Hand-built because Enqueue
	// cannot overfill the queue that ProcessInternalQueue then drains.
	backoff := time.Now().UTC().Add(time.Hour)
	env.daemon.internalQueue = &InternalQueue{
		entries: []InternalQueueEntry{
			{HostID: hostID, URL: "https://h.test/first", DimensionID: dimID, NextRetryAfter: backoff},
			{HostID: hostID, URL: "https://h.test/second", DimensionID: dimID, NextRetryAfter: backoff},
		},
		maxSize: 1,
	}

	env.daemon.ProcessInternalQueue()

	require.Len(t, drops(), 1)
	assert.Equal(t, PrecacheDrop{
		URL:          "https://h.test/second",
		HostID:       hostID,
		DimensionID:  dimID,
		ErrorType:    dropErrorTypeQueueOverflow,
		ErrorMessage: queueOverflowMessage(queueFullReasonBackoff),
	}, drops()[0])
}

// TestPrecacheDropSink_QueueOverflowAfterFailedRecache covers the distributor's re-enqueue of a
// retryable failure against a full queue.
func TestPrecacheDropSink_QueueOverflowAfterFailedRecache(t *testing.T) {
	const hostID, dimID = 1, 1
	env := newSchedulerEnv(t, 0, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	drops := collectDrops(env.daemon)

	results := make(chan RecacheResult, 1)
	results <- RecacheResult{
		Entry: InternalQueueEntry{HostID: hostID, URL: "https://h.test/p", DimensionID: dimID},
		Attempt: recacheAttempt{
			outcome:   outcomeFailed,
			errorType: types.ErrorTypeOrigin5xx,
			err:       fmt.Errorf("origin returned uncacheable status 503"),
		},
	}
	close(results)
	env.daemon.HandleRecacheResults(results)

	require.Len(t, drops(), 1)
	assert.Equal(t, dropErrorTypeQueueOverflow, drops()[0].ErrorType)
	assert.Equal(t, queueOverflowMessage(queueFullReasonRecacheFailed), drops()[0].ErrorMessage)
}

// TestPrecacheDropSink_QueueOverflowAfterEGDispatchFailure covers the last of the five sites:
// a batch that never reached an edge gateway and cannot go back on the queue either.
func TestPrecacheDropSink_QueueOverflowAfterEGDispatchFailure(t *testing.T) {
	const hostID, dimID = 1, 1
	env := newSchedulerEnv(t, 0, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	drops := collectDrops(env.daemon)

	slot, ok := env.daemon.concurrencyLimiter.TryAcquire(hostID)
	require.True(t, ok)

	env.daemon.releaseAndReenqueue([]readyItem{{
		entry: InternalQueueEntry{HostID: hostID, URL: "https://h.test/p", DimensionID: dimID},
		slot:  slot,
	}}, fmt.Errorf("no healthy edge gateways available"))

	require.Len(t, drops(), 1)
	assert.Equal(t, dropErrorTypeQueueOverflow, drops()[0].ErrorType)
	assert.Equal(t, queueOverflowMessage(queueFullReasonEGDispatch), drops()[0].ErrorMessage)
}

// TestPrecacheDropSink_NilSinkIsNoOp: the open-source build installs no sink, so every drop site
// must stay a plain log. Exercising them with the sink unset is the whole test.
func TestPrecacheDropSink_NilSinkIsNoOp(t *testing.T) {
	const hostID, unknownHost, dimID = 1, 99, 1
	env := newSchedulerEnv(t, 0, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	require.Nil(t, env.daemon.precacheDropSink, "no sink is installed by default")

	entry := InternalQueueEntry{HostID: hostID, URL: "https://h.test/p", DimensionID: dimID}

	assert.NotPanics(t, func() {
		env.daemon.emitPrecacheDrop(entry, dropErrorTypeMaxRetries, "no sink installed")

		exhausted := entry
		exhausted.RetryCount = env.daemon.daemonConfig.InternalQueue.MaxRetries - 1
		env.daemon.markEntryFailed(exhausted, fmt.Errorf("boom"))

		env.daemon.recordQueueFullDrop(entry, queueFullReasonBackoff)

		unresolved := entry
		unresolved.HostID = unknownHost
		env.daemon.internalQueue = NewInternalQueue(10)
		require.True(t, env.daemon.internalQueue.Enqueue(unresolved))
		env.daemon.ProcessInternalQueue()
	})
}

// TestPrecacheDropSink_SetNilClears: the setter accepts nil the way SetReloadFunc does, so an
// installed sink can be removed.
func TestPrecacheDropSink_SetNilClears(t *testing.T) {
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: 1, domain: "h.test", maxConcurrent: 5, dimensionID: 1},
	})

	collectDrops(env.daemon)
	require.NotNil(t, env.daemon.precacheDropSink)

	env.daemon.SetPrecacheDropSink(nil)
	assert.Nil(t, env.daemon.precacheDropSink)
}
