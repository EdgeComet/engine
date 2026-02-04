package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/edge/recache"
)

// RecacheResult represents the result of a single recache attempt
type RecacheResult struct {
	Entry   InternalQueueEntry
	Success bool
	Error   error
}

// DistributeToEGs distributes a batch of recache requests across healthy EG instances
func (d *CacheDaemon) DistributeToEGs(batch []InternalQueueEntry) {
	ctx := context.Background()

	// Get healthy EGs from registry
	egs, err := d.egRegistry.GetHealthyEGs(ctx)
	if err != nil {
		d.logger.Error("Failed to query EG registry",
			zap.Error(err),
			zap.Int("batch_size", len(batch)))
		// Re-enqueue entire batch
		for _, entry := range batch {
			d.internalQueue.Enqueue(entry)
		}
		return
	}

	if len(egs) == 0 {
		d.logger.Warn("No healthy EGs available, re-enqueueing batch",
			zap.Int("batch_size", len(batch)))
		// Re-enqueue entire batch
		for _, entry := range batch {
			d.internalQueue.Enqueue(entry)
		}
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

// SendBatchToEG sends a batch of recache requests to a single EG concurrently
func (d *CacheDaemon) SendBatchToEG(egAddress string, batch []InternalQueueEntry, results chan<- RecacheResult, wg *sync.WaitGroup) {
	defer wg.Done()

	d.logger.Debug("Sending batch to EG",
		zap.String("eg_address", egAddress),
		zap.Int("batch_size", len(batch)))

	var batchWG sync.WaitGroup

	for _, entry := range batch {
		batchWG.Add(1)

		go func(e InternalQueueEntry) {
			defer batchWG.Done()

			err := d.SendRecacheRequest(egAddress, e)

			results <- RecacheResult{
				Entry:   e,
				Success: err == nil,
				Error:   err,
			}
		}(entry)
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

// HandleRecacheResults processes results and implements retry logic
func (d *CacheDaemon) HandleRecacheResults(resultsChan chan RecacheResult) {
	successCount := 0
	retryCount := 0
	discardCount := 0
	failedEntries := []InternalQueueEntry{}

	for result := range resultsChan {
		if result.Success {
			successCount++
		} else {
			// Increment retry count
			result.Entry.RetryCount++
			result.Entry.LastAttempt = time.Now().UTC()

			if result.Entry.RetryCount < d.daemonConfig.InternalQueue.MaxRetries {
				// Calculate exponential backoff delay: 5s, 10s, 20s, etc.
				delay := d.retryBaseDelay * (1 << (result.Entry.RetryCount - 1))
				result.Entry.NextRetryAfter = time.Now().UTC().Add(delay)

				// Re-enqueue for retry
				failedEntries = append(failedEntries, result.Entry)
				retryCount++

				d.logger.Debug("Recache failed, will retry with backoff",
					zap.Int("host_id", result.Entry.HostID),
					zap.String("url", result.Entry.URL),
					zap.Int("dimension_id", result.Entry.DimensionID),
					zap.Int("retry_count", result.Entry.RetryCount),
					zap.Duration("retry_after", delay),
					zap.Error(result.Error))
			} else {
				// Discard after max retries
				discardCount++

				d.logger.Error("Recache failed after max retries, discarding",
					zap.Int("host_id", result.Entry.HostID),
					zap.String("url", result.Entry.URL),
					zap.Int("dimension_id", result.Entry.DimensionID),
					zap.Int("retry_count", result.Entry.RetryCount),
					zap.Error(result.Error))
			}
		}
	}

	// Re-enqueue failed entries
	for _, entry := range failedEntries {
		d.internalQueue.Enqueue(entry)
	}

	d.logger.Info("Recache batch results",
		zap.Int("success", successCount),
		zap.Int("retry", retryCount),
		zap.Int("discard", discardCount))
}
