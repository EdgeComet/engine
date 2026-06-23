package cachedaemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/recache"
)

// Recache outcome status labels for edgecomet_cd_recache_requests_total.
const (
	recacheStatusSuccess = "success"
	recacheStatusTimeout = "timeout"
	recacheStatusError   = "error"
)

// readyItem pairs a queue entry with the per-host concurrency slot reserved
// for it. The slot is acquired by the scheduler before dispatch and released
// by whichever distributor path finishes the entry's work — either the
// per-URL goroutine on success/failure, or the early-return loops in
// DistributeToEGs when no EGs are available.
type readyItem struct {
	entry InternalQueueEntry
	slot  Slot
	// isRender is captured at gate time (ProcessInternalQueue) so the per-URL
	// goroutine can pair the in-flight render counter without re-reading config
	// (which a concurrent reload could have flipped).
	isRender bool
}

// RecacheResult represents the result of a single recache attempt
type RecacheResult struct {
	Entry   InternalQueueEntry
	Success bool
	Error   error
}

// DistributeToEGs distributes a batch of recache requests across healthy EG instances.
// Every successful TryAcquire upstream is paired with exactly one Release here,
// either in an early-return loop or in the per-URL goroutine's deferred release.
func (d *CacheDaemon) DistributeToEGs(batch []readyItem) {
	ctx := context.Background()

	// Get healthy EGs from registry
	egs, err := d.egRegistry.GetHealthyEGs(ctx)
	if err != nil {
		d.logger.Error("Failed to query EG registry",
			zap.Error(err),
			zap.Int("batch_size", len(batch)))
		d.releaseAndReenqueue(batch, fmt.Errorf("eg registry query failed: %w", err))
		return
	}

	if len(egs) == 0 {
		d.logger.Warn("No healthy EGs available, re-enqueueing batch",
			zap.Int("batch_size", len(batch)))
		d.releaseAndReenqueue(batch, fmt.Errorf("no healthy edge gateways available"))
		return
	}

	// Calculate distribution across EGs
	numEGs := len(egs)
	urlsPerEG := len(batch) / numEGs
	remainder := len(batch) % numEGs

	d.logger.Info("Distributing recache batch to EGs",
		zap.Int("batch_size", len(batch)),
		zap.Int("num_egs", numEGs),
		zap.Int("urls_per_eg", urlsPerEG))

	var wg sync.WaitGroup
	resultsChan := make(chan RecacheResult, len(batch))

	startIdx := 0
	for i, eg := range egs {
		count := urlsPerEG
		if i < remainder {
			count++ // Distribute remainder URLs to first N EGs
		}

		if count == 0 {
			continue
		}

		egBatch := batch[startIdx : startIdx+count]
		startIdx += count

		wg.Add(1)
		go d.SendBatchToEG(eg.Address, egBatch, resultsChan, &wg)
	}

	// Wait for all EG batches to complete
	wg.Wait()
	close(resultsChan)

	// Process results (retry logic)
	d.HandleRecacheResults(resultsChan)
}

// releaseAndReenqueue releases every concurrency slot in batch, applies retry
// backoff (so the next dispatch attempt is delayed), and re-enqueues the
// entries. Used by DistributeToEGs early-return paths so persistent EG-side
// failures do not cause the daemon to spin re-dispatching the same batch on
// every tick. Entries that have exceeded MaxRetries are discarded.
func (d *CacheDaemon) releaseAndReenqueue(batch []readyItem, cause error) {
	retried := 0
	discarded := 0
	for _, item := range batch {
		d.concurrencyLimiter.Release(item.slot)
		entry, retry := d.markEntryFailed(item.entry, cause)
		if !retry {
			discarded++
			continue
		}
		if !d.internalQueue.Enqueue(entry) {
			d.logger.Error("Internal queue full while re-enqueueing after EG dispatch failure; entry dropped",
				zap.Int("host_id", entry.HostID),
				zap.String("url", entry.URL),
				zap.Int("dimension_id", entry.DimensionID),
				zap.Int("retry_count", entry.RetryCount))
			discarded++
			continue
		}
		retried++
	}
	d.logger.Info("Recache batch deferred after EG dispatch failure",
		zap.Int("retry", retried),
		zap.Int("discard", discarded),
		zap.Error(cause))
}

// markEntryFailed updates an entry's retry bookkeeping after a failed
// dispatch attempt. Returns the updated entry and whether the caller should
// re-enqueue (false = MaxRetries exceeded, caller must discard).
func (d *CacheDaemon) markEntryFailed(entry InternalQueueEntry, cause error) (InternalQueueEntry, bool) {
	entry.RetryCount++
	now := time.Now().UTC()
	entry.LastAttempt = now
	if entry.RetryCount >= d.daemonConfig.InternalQueue.MaxRetries {
		d.logger.Error("Recache failed after max retries, discarding",
			zap.Int("host_id", entry.HostID),
			zap.String("url", entry.URL),
			zap.Int("dimension_id", entry.DimensionID),
			zap.Int("retry_count", entry.RetryCount),
			zap.Error(cause))
		return entry, false
	}
	delay := d.retryBaseDelay * (1 << (entry.RetryCount - 1))
	entry.NextRetryAfter = now.Add(delay)
	d.logger.Debug("Recache failed, will retry with backoff",
		zap.Int("host_id", entry.HostID),
		zap.String("url", entry.URL),
		zap.Int("dimension_id", entry.DimensionID),
		zap.Int("retry_count", entry.RetryCount),
		zap.Duration("retry_after", delay),
		zap.Error(cause))
	return entry, true
}

// SendBatchToEG sends a batch of recache requests to a single EG concurrently.
// Each per-URL goroutine releases its concurrency slot via defer once the
// request completes (success, EG error, or timeout).
func (d *CacheDaemon) SendBatchToEG(egAddress string, batch []readyItem, results chan<- RecacheResult, wg *sync.WaitGroup) {
	defer wg.Done()

	d.logger.Debug("Sending batch to EG",
		zap.String("eg_address", egAddress),
		zap.Int("batch_size", len(batch)))

	var batchWG sync.WaitGroup

	for _, item := range batch {
		batchWG.Add(1)

		go func(it readyItem) {
			// Pair the in-flight render counter 1:1. Both the increment and the
			// deferred decrement live in this goroutine, so every early-return
			// path that releases a slot WITHOUT spawning this goroutine
			// (no-healthy-EG, registry error, coordinator panic) never
			// increments and therefore needs no decrement.
			if it.isRender {
				atomic.AddInt64(&d.inFlightRenders, 1)
			}
			defer batchWG.Done()
			defer d.concurrencyLimiter.Release(it.slot)
			if it.isRender {
				defer atomic.AddInt64(&d.inFlightRenders, -1)
			}
			defer func() {
				if r := recover(); r != nil {
					// Contain a panic in SendRecacheRequest here so it cannot
					// crash the process. The deferred Release above still runs
					// during unwind, so the slot is freed. Emit a result so the
					// coordinator's accounting stays balanced.
					d.logger.Error("recovered panic in recache URL dispatch",
						zap.String("url", it.entry.URL), zap.Any("panic", r))
					results <- RecacheResult{Entry: it.entry, Success: false,
						Error: fmt.Errorf("dispatch panic: %v", r)}
				}
			}()

			start := time.Now()
			err := d.SendRecacheRequest(egAddress, it.entry)
			d.recordRecacheOutcome(it.entry, err, time.Since(start))

			results <- RecacheResult{
				Entry:   it.entry,
				Success: err == nil,
				Error:   err,
			}
		}(item)
	}

	// Wait for all requests in this EG batch to complete
	batchWG.Wait()
}

// SendRecacheRequest sends a single recache request to an EG
func (d *CacheDaemon) SendRecacheRequest(egAddress string, entry InternalQueueEntry) error {
	url := fmt.Sprintf("http://%s/internal/cache/recache", egAddress)

	// Build request body
	body := recache.RecacheRequest{
		URL:         entry.URL,
		HostID:      entry.HostID,
		DimensionID: entry.DimensionID,
		Mode:        entry.Mode,
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Acquire request/response from pool
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	// Set request
	req.SetRequestURI(url)
	req.Header.SetMethod("POST")
	req.Header.Set("X-Internal-Auth", d.internalAuthKey)
	req.Header.Set("Content-Type", "application/json")
	req.SetBody(bodyJSON)

	// Execute request with timeout
	err = d.httpClient.DoTimeout(req, resp, time.Duration(d.daemonConfig.Recache.TimeoutPerURL))
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}

	// Check status code
	if resp.StatusCode() != 200 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
	}

	d.logger.Debug("Recache request successful",
		zap.String("eg_address", egAddress),
		zap.Int("host_id", entry.HostID),
		zap.String("url", entry.URL),
		zap.Int("dimension_id", entry.DimensionID))

	return nil
}

// recordRecacheOutcome records per-URL dispatch timing and outcome to
// Prometheus. RecordRecacheDuration was previously never wired, which is why
// recache_duration_seconds read all-zero in production. The status label lets
// operators see the straggler (timeout) rate directly.
func (d *CacheDaemon) recordRecacheOutcome(entry InternalQueueEntry, err error, elapsed time.Duration) {
	if d.metricsCollector == nil {
		return
	}
	d.metricsCollector.RecordRecacheDuration(elapsed)

	status := recacheStatusSuccess
	if err != nil {
		if isTimeoutErr(err) {
			status = recacheStatusTimeout
		} else {
			status = recacheStatusError
		}
	}
	queueType := entry.Priority
	if queueType == "" {
		queueType = redis.PriorityNormal
	}
	d.metricsCollector.RecordRecacheRequest(status, queueType)
}

// isTimeoutErr reports whether err is (or wraps) a request deadline/timeout, so
// a URL that hit timeout_per_url is classified as a timeout rather than a
// generic error. DoTimeout surfaces fasthttp.ErrTimeout; dial-level deadlines
// surface as a net.Error with Timeout() true.
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fasthttp.ErrTimeout) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
}

// HandleRecacheResults processes results and implements retry logic.
// Failed entries are marked with backoff via markEntryFailed and re-enqueued;
// entries past MaxRetries are discarded. If the internal queue is full when
// re-enqueueing, the entry is dropped with an error log instead of disappearing
// silently.
func (d *CacheDaemon) HandleRecacheResults(resultsChan chan RecacheResult) {
	successCount := 0
	retryCount := 0
	discardCount := 0

	for result := range resultsChan {
		if result.Success {
			successCount++
			continue
		}

		entry, retry := d.markEntryFailed(result.Entry, result.Error)
		if !retry {
			discardCount++
			continue
		}
		if !d.internalQueue.Enqueue(entry) {
			d.logger.Error("Internal queue full while re-enqueueing failed recache; entry dropped",
				zap.Int("host_id", entry.HostID),
				zap.String("url", entry.URL),
				zap.Int("dimension_id", entry.DimensionID),
				zap.Int("retry_count", entry.RetryCount))
			discardCount++
			continue
		}
		retryCount++
	}

	d.logger.Info("Recache batch results",
		zap.Int("success", successCount),
		zap.Int("retry", retryCount),
		zap.Int("discard", discardCount))
}
