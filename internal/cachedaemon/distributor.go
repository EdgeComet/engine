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
	"github.com/edgecomet/engine/pkg/types"
)

// Recache outcome status labels for edgecomet_cd_recache_requests_total.
const (
	recacheStatusSuccess = "success"
	recacheStatusSkipped = "skipped"
	recacheStatusTimeout = "timeout"
	recacheStatusError   = "error"
)

// recacheOutcome classifies what one recache request achieved at the edge gateway.
type recacheOutcome int

const (
	// outcomeCached: the edge gateway refreshed the cache entry.
	outcomeCached recacheOutcome = iota
	// outcomeSkipped: the resolved configuration declines to cache this URL. Terminal, and
	// nothing a retry would change.
	outcomeSkipped
	// outcomeFailed: the attempt failed; permanent decides whether retrying is worth anything.
	outcomeFailed
)

// recacheAttempt is the classified result of one recache request to an edge gateway.
type recacheAttempt struct {
	outcome recacheOutcome
	// permanent marks a failure no retry can resolve. The edge gateway has already recorded
	// its own event row for it, so the daemon neither retries nor emits a drop.
	permanent bool
	// errorType is the edge gateway's classification, empty when the answer carried none.
	errorType string
	// err is non-nil exactly when outcome is outcomeFailed.
	err error
}

// stampOn records this attempt's classification on the entry so a later terminal discard names
// the edge gateway's real diagnosis rather than whichever dispatch-level error came last.
func (a recacheAttempt) stampOn(entry InternalQueueEntry) InternalQueueEntry {
	// Only a classified attempt overwrites the stamp. A dispatch-level failure - timeout, panic,
	// no answer at all - carries no classification, and letting it blank the field would leave the
	// terminal discard reporting "HTTP request failed" for an entry an edge gateway had already
	// diagnosed. The immediate cause still reaches the log through markEntryFailed's zap.Error.
	if a.errorType == "" {
		return entry
	}
	entry.LastErrorType = a.errorType
	if a.err != nil {
		entry.LastErrorMessage = a.err.Error()
	}
	return entry
}

// failedAttempt classifies a failure the edge gateway never got to describe - no answer reached
// us at all - so another attempt is the only reasonable response.
func failedAttempt(err error) recacheAttempt {
	return recacheAttempt{outcome: outcomeFailed, err: err}
}

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

// RecacheResult pairs a queue entry with the classified result of dispatching it.
type RecacheResult struct {
	Entry   InternalQueueEntry
	Attempt recacheAttempt
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
			d.recordQueueFullDrop(entry, queueFullReasonEGDispatch)
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
		lastCause := lastFailureCause(entry, cause)
		d.logger.Error("Recache failed after max retries, discarding",
			zap.Int("host_id", entry.HostID),
			zap.String("url", entry.URL),
			zap.Int("dimension_id", entry.DimensionID),
			zap.Int("retry_count", entry.RetryCount),
			zap.String("last_error_type", entry.LastErrorType),
			zap.String("last_error", lastCause),
			zap.Error(cause))
		d.emitPrecacheDrop(entry, dropErrorTypeMaxRetries, lastCause)
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
		zap.String("last_error_type", entry.LastErrorType),
		zap.Error(cause))
	return entry, true
}

// lastFailureCause renders the most informative account of why an entry is being given up on:
// the edge gateway's own classification when one ever reached us, and the dispatch-level error
// otherwise (no EG answered, so no classification exists to prefer).
func lastFailureCause(entry InternalQueueEntry, cause error) string {
	if entry.LastErrorMessage == "" {
		if cause == nil {
			return ""
		}
		return cause.Error()
	}
	if entry.LastErrorType == "" {
		return entry.LastErrorMessage
	}
	return entry.LastErrorType + ": " + entry.LastErrorMessage
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
					results <- RecacheResult{Entry: it.entry,
						Attempt: failedAttempt(fmt.Errorf("dispatch panic: %v", r))}
				}
			}()

			start := time.Now()
			attempt := d.SendRecacheRequest(egAddress, it.entry)
			d.recordRecacheOutcome(it.entry, attempt, time.Since(start))

			results <- RecacheResult{Entry: it.entry, Attempt: attempt}
		}(item)
	}

	// Wait for all requests in this EG batch to complete
	batchWG.Wait()
}

// SendRecacheRequest sends a single recache request to an EG and classifies the answer.
func (d *CacheDaemon) SendRecacheRequest(egAddress string, entry InternalQueueEntry) recacheAttempt {
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
		return failedAttempt(fmt.Errorf("failed to marshal request body: %w", err))
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
		return failedAttempt(fmt.Errorf("HTTP request failed: %w", err))
	}

	attempt := classifyRecacheResponse(resp.StatusCode(), resp.Body())
	if attempt.outcome != outcomeFailed {
		d.logger.Debug("Recache request completed",
			zap.String("eg_address", egAddress),
			zap.Int("host_id", entry.HostID),
			zap.String("url", entry.URL),
			zap.Int("dimension_id", entry.DimensionID),
			zap.Bool("skipped", attempt.outcome == outcomeSkipped))
	}

	return attempt
}

// recacheEnvelope is httputil.APIResponse with the data payload left opaque, so a body whose
// data is not a recache outcome decodes far enough to be recognised as such instead of
// aborting the whole parse.
type recacheEnvelope struct {
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// classifyRecacheResponse turns an edge gateway's answer into a retry decision. The outcome
// payload is authoritative when present; anything else - an edge gateway predating the outcome
// protocol, a 400/401 error envelope, a non-JSON body - degrades to the status code alone,
// which is all the daemon ever used before. Mixed-version deploys therefore behave exactly as
// they did, rather than misreading silence as a permanent failure.
func classifyRecacheResponse(statusCode int, body []byte) recacheAttempt {
	envelope, data, ok := decodeRecacheOutcome(body)
	if !ok {
		return classifyRecacheStatus(statusCode)
	}

	switch data.Outcome {
	case types.RecacheOutcomeCached:
		return recacheAttempt{outcome: outcomeCached}
	case types.RecacheOutcomeSkipped:
		return recacheAttempt{outcome: outcomeSkipped}
	case types.RecacheOutcomeFailed:
		message := envelope.Message
		if message == "" {
			message = fmt.Sprintf("recache failed with status %d", statusCode)
		}
		return recacheAttempt{
			outcome: outcomeFailed,
			// The status is the retry instruction and the flag restates it; honour either,
			// so a disagreement never turns a permanent failure into three more attempts.
			permanent: data.Permanent || statusCode == fasthttp.StatusUnprocessableEntity,
			errorType: data.ErrorType,
			err:       errors.New(message),
		}
	default:
		return classifyRecacheStatus(statusCode)
	}
}

// decodeRecacheOutcome extracts the outcome payload, reporting whether one was actually there.
func decodeRecacheOutcome(body []byte) (recacheEnvelope, types.RecacheOutcomeData, bool) {
	var envelope recacheEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Data) == 0 {
		return envelope, types.RecacheOutcomeData{}, false
	}

	var data types.RecacheOutcomeData
	if err := json.Unmarshal(envelope.Data, &data); err != nil || data.Outcome == "" {
		return envelope, types.RecacheOutcomeData{}, false
	}
	return envelope, data, true
}

// classifyRecacheStatus is the pre-outcome-protocol reading of a response: 200 succeeded,
// everything else is worth another attempt.
func classifyRecacheStatus(statusCode int) recacheAttempt {
	if statusCode == fasthttp.StatusOK {
		return recacheAttempt{outcome: outcomeCached}
	}
	return failedAttempt(fmt.Errorf("unexpected status code: %d", statusCode))
}

// recordRecacheOutcome records per-URL dispatch timing and outcome to
// Prometheus. RecordRecacheDuration was previously never wired, which is why
// recache_duration_seconds read all-zero in production. The status label lets
// operators see the straggler (timeout) rate directly.
func (d *CacheDaemon) recordRecacheOutcome(entry InternalQueueEntry, attempt recacheAttempt, elapsed time.Duration) {
	if d.metricsCollector == nil {
		return
	}
	d.metricsCollector.RecordRecacheDuration(elapsed)

	status := recacheStatusSuccess
	switch {
	case attempt.outcome == outcomeSkipped:
		status = recacheStatusSkipped
	case attempt.outcome == outcomeFailed && isTimeoutErr(attempt.err):
		status = recacheStatusTimeout
	case attempt.outcome == outcomeFailed:
		status = recacheStatusError
	}

	queueType := entry.Priority
	if queueType == "" {
		queueType = redis.PriorityNormal
	}
	d.metricsCollector.RecordRecacheRequest(status, queueType, entry.HostID)
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
// Retryable failures are marked with backoff via markEntryFailed and re-enqueued;
// entries past MaxRetries are discarded. If the internal queue is full when
// re-enqueueing, the entry is dropped with an error log instead of disappearing
// silently. Outcomes the edge gateway called terminal - cached, declined by
// configuration, or a failure it marked permanent - end here without another attempt.
func (d *CacheDaemon) HandleRecacheResults(resultsChan chan RecacheResult) {
	successCount := 0
	skippedCount := 0
	permanentCount := 0
	retryCount := 0
	discardCount := 0

	for result := range resultsChan {
		if result.Attempt.outcome == outcomeCached {
			successCount++
			continue
		}
		if result.Attempt.outcome == outcomeSkipped {
			skippedCount++
			continue
		}
		if result.Attempt.permanent {
			// Retrying cannot change the edge gateway's answer, and it already recorded
			// the failure itself, so the daemon adds neither an attempt nor a drop.
			permanentCount++
			continue
		}

		entry, retry := d.markEntryFailed(result.Attempt.stampOn(result.Entry), result.Attempt.err)
		if !retry {
			discardCount++
			continue
		}
		if !d.internalQueue.Enqueue(entry) {
			d.recordQueueFullDrop(entry, queueFullReasonRecacheFailed)
			discardCount++
			continue
		}
		retryCount++
	}

	d.logger.Info("Recache batch results",
		zap.Int("success", successCount),
		zap.Int("skipped", skippedCount),
		zap.Int("permanent_failure", permanentCount),
		zap.Int("retry", retryCount),
		zap.Int("discard", discardCount))
}
