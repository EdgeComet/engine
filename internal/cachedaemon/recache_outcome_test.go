package cachedaemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/common/httputil"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

// outcomeEG is an Edge Gateway stand-in that answers every recache request with one canned
// status and body, so tests can pin how the daemon reads each response shape.
type outcomeEG struct {
	srv     *httptest.Server
	address string

	mu    sync.Mutex
	count int
}

func newOutcomeEG(t *testing.T, statusCode int, body []byte) *outcomeEG {
	t.Helper()
	f := &outcomeEG{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		f.mu.Lock()
		f.count++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write(body)
	}))
	f.address = strings.TrimPrefix(f.srv.URL, "http://")
	t.Cleanup(f.srv.Close)
	return f
}

func (f *outcomeEG) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

// outcomeBody renders the response the edge gateway's recache handler produces.
func outcomeBody(t *testing.T, success bool, message string, data types.RecacheOutcomeData) []byte {
	t.Helper()
	raw, err := json.Marshal(httputil.APIResponse{Success: success, Message: message, Data: data})
	require.NoError(t, err)
	return raw
}

// TestClassifyRecacheResponse_OutcomeMatrix pins how each response shape maps onto a retry
// decision. The fallback rows are the load-bearing ones: an edge gateway that predates the
// outcome protocol, an error envelope with no data, and a body that is not JSON at all must all
// read exactly as they did before the protocol existed, or a mixed-version deploy changes
// behaviour on every request.
func TestClassifyRecacheResponse_OutcomeMatrix(t *testing.T) {
	cases := []struct {
		name          string
		statusCode    int
		body          []byte
		wantOutcome   recacheOutcome
		wantPermanent bool
		wantErrorType string
		wantMessage   string
	}{
		{
			name:        "cached outcome",
			statusCode:  http.StatusOK,
			body:        outcomeBody(t, true, "", types.RecacheOutcomeData{Outcome: types.RecacheOutcomeCached}),
			wantOutcome: outcomeCached,
		},
		{
			name:       "skipped outcome",
			statusCode: http.StatusOK,
			body: outcomeBody(t, true, "", types.RecacheOutcomeData{
				Outcome: types.RecacheOutcomeSkipped,
				Reason:  "bypass cache disabled",
			}),
			wantOutcome: outcomeSkipped,
		},
		{
			name:       "permanent failure",
			statusCode: http.StatusUnprocessableEntity,
			body: outcomeBody(t, false, "origin returned uncacheable status 404", types.RecacheOutcomeData{
				Outcome:   types.RecacheOutcomeFailed,
				ErrorType: types.ErrorTypeOrigin4xx,
				Permanent: true,
			}),
			wantOutcome:   outcomeFailed,
			wantPermanent: true,
			wantErrorType: types.ErrorTypeOrigin4xx,
			wantMessage:   "origin returned uncacheable status 404",
		},
		{
			name:       "retryable failure",
			statusCode: http.StatusInternalServerError,
			body: outcomeBody(t, false, "origin returned uncacheable status 503", types.RecacheOutcomeData{
				Outcome:   types.RecacheOutcomeFailed,
				ErrorType: types.ErrorTypeOrigin5xx,
			}),
			wantOutcome:   outcomeFailed,
			wantErrorType: types.ErrorTypeOrigin5xx,
			wantMessage:   "origin returned uncacheable status 503",
		},
		{
			name:       "422 without the permanent flag is still permanent",
			statusCode: http.StatusUnprocessableEntity,
			body: outcomeBody(t, false, "dimension 7 not found for host 1", types.RecacheOutcomeData{
				Outcome:   types.RecacheOutcomeFailed,
				ErrorType: types.ErrorTypeInvalidRequest,
			}),
			wantOutcome:   outcomeFailed,
			wantPermanent: true,
			wantErrorType: types.ErrorTypeInvalidRequest,
			wantMessage:   "dimension 7 not found for host 1",
		},
		{
			name:        "fallback: envelope carrying a different data shape",
			statusCode:  http.StatusOK,
			body:        outcomeBody(t, true, "", types.RecacheOutcomeData{}),
			wantOutcome: outcomeCached,
		},
		{
			name:        "fallback: old edge gateway, no data at all",
			statusCode:  http.StatusOK,
			body:        []byte(`{"success":true,"message":""}`),
			wantOutcome: outcomeCached,
		},
		{
			name:        "fallback: error envelope with no data",
			statusCode:  http.StatusBadRequest,
			body:        []byte(`{"success":false,"message":"missing required fields"}`),
			wantOutcome: outcomeFailed,
			wantMessage: "unexpected status code: 400",
		},
		{
			name:        "fallback: non-JSON body",
			statusCode:  http.StatusInternalServerError,
			body:        []byte("Internal server error"),
			wantOutcome: outcomeFailed,
			wantMessage: "unexpected status code: 500",
		},
		{
			name:        "fallback: empty body",
			statusCode:  http.StatusOK,
			body:        nil,
			wantOutcome: outcomeCached,
		},
		{
			name:        "fallback: unrecognised outcome value",
			statusCode:  http.StatusOK,
			body:        outcomeBody(t, true, "", types.RecacheOutcomeData{Outcome: "reticulated"}),
			wantOutcome: outcomeCached,
		},
		{
			name:        "fallback: unrecognised outcome value on a failing status",
			statusCode:  http.StatusServiceUnavailable,
			body:        outcomeBody(t, false, "", types.RecacheOutcomeData{Outcome: "reticulated"}),
			wantOutcome: outcomeFailed,
			wantMessage: "unexpected status code: 503",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attempt := classifyRecacheResponse(tc.statusCode, tc.body)

			assert.Equal(t, tc.wantOutcome, attempt.outcome)
			assert.Equal(t, tc.wantPermanent, attempt.permanent)
			assert.Equal(t, tc.wantErrorType, attempt.errorType)
			if tc.wantOutcome == outcomeFailed {
				require.Error(t, attempt.err)
				assert.Equal(t, tc.wantMessage, attempt.err.Error())
			} else {
				assert.NoError(t, attempt.err)
			}
		})
	}
}

// TestHandleRecacheResults_TerminalOutcomesDoNotRetry: cached, skipped and permanent-failure
// results all end the entry's life. Only a retryable failure goes back on the queue.
func TestHandleRecacheResults_TerminalOutcomesDoNotRetry(t *testing.T) {
	const hostID, dimID = 1, 1

	cases := []struct {
		name      string
		attempt   recacheAttempt
		wantQueue int
	}{
		{name: "cached", attempt: recacheAttempt{outcome: outcomeCached}},
		{name: "skipped", attempt: recacheAttempt{outcome: outcomeSkipped}},
		{name: "permanent failure", attempt: recacheAttempt{
			outcome:   outcomeFailed,
			permanent: true,
			errorType: types.ErrorTypeInvalidRequest,
			err:       fmt.Errorf("SSRF protection: private ip"),
		}},
		{name: "retryable failure", attempt: recacheAttempt{
			outcome:   outcomeFailed,
			errorType: types.ErrorTypeOrigin5xx,
			err:       fmt.Errorf("origin returned uncacheable status 503"),
		}, wantQueue: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newSchedulerEnv(t, 10, []schedulerTestHost{
				{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
			})
			var drops []PrecacheDrop
			env.daemon.SetPrecacheDropSink(func(d PrecacheDrop) { drops = append(drops, d) })

			results := make(chan RecacheResult, 1)
			results <- RecacheResult{
				Entry:   InternalQueueEntry{HostID: hostID, URL: "https://h.test/p", DimensionID: dimID},
				Attempt: tc.attempt,
			}
			close(results)
			env.daemon.HandleRecacheResults(results)

			assert.Equal(t, tc.wantQueue, env.daemon.internalQueue.Size())
			assert.Empty(t, drops, "a single attempt never exhausts retries, so nothing is dropped")
		})
	}
}

// TestHandleRecacheResults_RetainsLastEGError: the edge gateway's classification survives on the
// entry, so the terminal discard reports the real cause rather than the last dispatch error.
func TestHandleRecacheResults_RetainsLastEGError(t *testing.T) {
	const hostID, dimID = 1, 1
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})

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

	queued := env.daemon.internalQueue.Dequeue(1)
	require.Len(t, queued, 1)
	assert.Equal(t, types.ErrorTypeOrigin5xx, queued[0].LastErrorType)
	assert.Equal(t, "origin returned uncacheable status 503", queued[0].LastErrorMessage)
}

// TestHandleRecacheResults_UnclassifiedRetryKeepsEGDiagnosis: a later dispatch-level failure has
// no classification of its own, so it must not blank the one an edge gateway already made. Without
// the guard the terminal discard would report "HTTP request failed" for an entry diagnosed as
// origin_5xx on its first attempt.
func TestHandleRecacheResults_UnclassifiedRetryKeepsEGDiagnosis(t *testing.T) {
	const hostID, dimID = 1, 1
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})

	classified := make(chan RecacheResult, 1)
	classified <- RecacheResult{
		Entry: InternalQueueEntry{HostID: hostID, URL: "https://h.test/p", DimensionID: dimID},
		Attempt: recacheAttempt{
			outcome:   outcomeFailed,
			errorType: types.ErrorTypeOrigin5xx,
			err:       fmt.Errorf("origin returned uncacheable status 503"),
		},
	}
	close(classified)
	env.daemon.HandleRecacheResults(classified)

	stamped := env.daemon.internalQueue.Dequeue(1)
	require.Len(t, stamped, 1)

	unclassified := make(chan RecacheResult, 1)
	unclassified <- RecacheResult{
		Entry:   stamped[0],
		Attempt: failedAttempt(fmt.Errorf("HTTP request failed: timeout")),
	}
	close(unclassified)
	env.daemon.HandleRecacheResults(unclassified)

	queued := env.daemon.internalQueue.Dequeue(1)
	require.Len(t, queued, 1)
	assert.Equal(t, types.ErrorTypeOrigin5xx, queued[0].LastErrorType)
	assert.Equal(t, "origin returned uncacheable status 503", queued[0].LastErrorMessage)
}

// TestSendRecacheRequest_PermanentFailureIsNotRetried drives the real HTTP path: a 422 answer
// leaves the queue empty and the edge gateway is asked exactly once.
func TestSendRecacheRequest_PermanentFailureIsNotRetried(t *testing.T) {
	const hostID, dimID = 1, 1
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	env.enableAsyncDispatch()

	eg := newOutcomeEG(t, http.StatusUnprocessableEntity, outcomeBody(t, false, "dimension 9 not found for host 1",
		types.RecacheOutcomeData{
			Outcome:   types.RecacheOutcomeFailed,
			ErrorType: types.ErrorTypeInvalidRequest,
			Permanent: true,
		}))
	env.registerEG(t, "eg1", eg.address)

	var drops []PrecacheDrop
	env.daemon.SetPrecacheDropSink(func(d PrecacheDrop) { drops = append(drops, d) })

	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      hostID,
		URL:         "https://h.test/permanent",
		DimensionID: dimID,
		Priority:    redis.PriorityHigh,
		QueuedAt:    time.Now().UTC(),
	}))

	env.daemon.ProcessInternalQueue()
	env.daemon.dispatchWG.Wait()

	assert.Equal(t, 1, eg.requests(), "a permanent failure must be attempted exactly once")
	assert.Equal(t, 0, env.daemon.internalQueue.Size(), "a permanent failure must not be re-enqueued")
	assert.Empty(t, drops, "the edge gateway already recorded this failure; the daemon adds no drop")
}

// TestSendRecacheRequest_RetryableFailureIsRequeued is the counterpart: a 500 with the same
// envelope shape keeps the entry alive, so the 422 result above is the protocol at work rather
// than the daemon losing entries on any failure.
func TestSendRecacheRequest_RetryableFailureIsRequeued(t *testing.T) {
	const hostID, dimID = 1, 1
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	env.enableAsyncDispatch()

	eg := newOutcomeEG(t, http.StatusInternalServerError, outcomeBody(t, false, "origin returned uncacheable status 503",
		types.RecacheOutcomeData{
			Outcome:   types.RecacheOutcomeFailed,
			ErrorType: types.ErrorTypeOrigin5xx,
		}))
	env.registerEG(t, "eg1", eg.address)

	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      hostID,
		URL:         "https://h.test/retryable",
		DimensionID: dimID,
		Priority:    redis.PriorityHigh,
		QueuedAt:    time.Now().UTC(),
	}))

	env.daemon.ProcessInternalQueue()
	env.daemon.dispatchWG.Wait()

	queued := env.daemon.internalQueue.Dequeue(1)
	require.Len(t, queued, 1)
	assert.Equal(t, 1, queued[0].RetryCount)
	assert.Equal(t, types.ErrorTypeOrigin5xx, queued[0].LastErrorType)
}

// TestMetrics_SkippedStatusAndHostLabel: a configuration decline is counted as skipped rather
// than folded into success, and every recache request carries its host.
func TestMetrics_SkippedStatusAndHostLabel(t *testing.T) {
	const hostID, dimID = 7, 1
	env := newSchedulerEnv(t, 10, []schedulerTestHost{
		{id: hostID, domain: "h.test", maxConcurrent: 5, dimensionID: dimID},
	})
	env.enableAsyncDispatch()
	mc := env.attachMetrics()

	eg := newOutcomeEG(t, http.StatusOK, outcomeBody(t, true, "", types.RecacheOutcomeData{
		Outcome: types.RecacheOutcomeSkipped,
		Reason:  "bypass cache disabled",
	}))
	env.registerEG(t, "eg1", eg.address)

	require.True(t, env.daemon.internalQueue.Enqueue(InternalQueueEntry{
		HostID:      hostID,
		URL:         "https://h.test/skipped",
		DimensionID: dimID,
		Priority:    redis.PriorityHigh,
		QueuedAt:    time.Now().UTC(),
	}))

	env.daemon.ProcessInternalQueue()
	env.daemon.dispatchWG.Wait()

	out := scrapeMetrics(t, mc)
	assert.Contains(t, out, `status="skipped"`)
	assert.Contains(t, out, `host_id="7"`)
	assert.NotContains(t, out, `status="success"`, "a configuration decline must not be counted as a refresh")
}
