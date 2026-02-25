package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/css"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/htmlprocessor"
	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/internal/render/har"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	maxConsoleMessagesSize = 5120     // Maximum total size of console messages in bytes (5KB)
	maxHTMLResponseSize    = 20971520 // Maximum HTML response size in bytes (20MB)
)

// Render performs page rendering with the Chrome instance
// Context cancellation is supported - will abort rendering and return partial results if possible
func (ci *ChromeInstance) Render(ctx context.Context, req *types.RenderRequest) (*types.RenderResponse, error) {
	start := time.Now()

	// Create network metrics collector
	metricsCollector := NewNetworkMetricsCollector(req.URL, start)

	// Create blocklist for this request (combines global + custom patterns + resource types)
	blocklist := NewBlocklistWithResourceTypes(req.BlockedPatterns, req.BlockedResourceTypes)
	ci.logger.Debug("Created blocklist for render",
		zap.String("request_id", req.RequestID),
		zap.Strings("custom_patterns", req.BlockedPatterns),
		zap.Strings("blocked_resource_types", req.BlockedResourceTypes),
		zap.Int("global_patterns", len(globalBlockedPatterns)))

	// Create new tab context from browser context
	tabCtx, tabCancel := ci.GetContext()
	defer tabCancel()

	// Cancel tab when request context times out or is cancelled
	// This allows both soft timeout (in navigation) and hard timeout (via context) to work
	stop := context.AfterFunc(ctx, tabCancel)
	defer stop()

	// Build response
	resp := &types.RenderResponse{
		RequestID: req.RequestID,
		ChromeID:  fmt.Sprintf("chrome-%d", ci.ID),
		Timestamp: start,
	}

	// Mutex to protect status code access (prevents race condition between event listeners and main goroutine)
	var statusCodeMu sync.Mutex

	// Create HAR collector if requested
	var harCollector *har.HARCollector
	if req.IncludeHAR {
		harCollector = har.NewHARCollector(req.URL, req.RequestID)
		ci.logger.Debug("HAR collector created for request",
			zap.String("request_id", req.RequestID),
			zap.String("url", req.URL))
	}

	// Execute rendering tasks with merged context
	err := chromedp.Run(tabCtx, ci.buildTasks(req, resp, blocklist, &statusCodeMu, harCollector, metricsCollector))

	resp.RenderTime = time.Since(start)

	// Check hard timeout FIRST (highest priority - prevents shadowing by redirect cancellation)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
		resp.Success = false
		resp.Error = "hard timeout exceeded"
		resp.ErrorType = types.ErrorTypeHardTimeout
		resp.Metrics.TimedOut = true
		return resp, fmt.Errorf("hard timeout exceeded: %w", ctx.Err())
	}

	// Check redirect cancellation (intentional cancel with 3xx status code captured)
	statusCodeMu.Lock()
	statusCode := resp.Metrics.StatusCode
	statusCodeMu.Unlock()

	if errors.Is(err, context.Canceled) && statusCode >= 300 && statusCode < 400 {
		resp.Success = true
		return resp, nil
	}

	// Handle other errors
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		resp.ErrorType = categorizeRenderError(err)
		return resp, err
	}

	// Validate status code was captured
	statusCodeMu.Lock()
	finalStatusCode := resp.Metrics.StatusCode
	statusCodeMu.Unlock()

	if finalStatusCode == 0 {
		resp.Success = false
		resp.Error = "failed to capture status code"
		resp.ErrorType = types.ErrorTypeStatusCaptureFailed
		ci.logger.Error("Status code capture failed completely (event + fallback)",
			zap.String("request_id", req.RequestID),
			zap.Int("instance_id", ci.ID),
			zap.String("url", req.URL),
			zap.Duration("render_time", resp.RenderTime))
		return resp, ErrStatusCapture
	}

	// Success path
	resp.Success = true
	resp.HTMLSize = len(resp.HTML)

	// Check for response too large - HARD FAILURE (413 Payload Too Large)
	if resp.HTMLSize > maxHTMLResponseSize {
		originalSize := resp.HTMLSize
		resp.Success = false
		resp.Error = fmt.Sprintf("response size %d exceeds maximum %d bytes", originalSize, maxHTMLResponseSize)
		resp.ErrorType = types.ErrorTypeResponseTooLarge
		resp.HTML = ""
		resp.HTMLSize = 0
		resp.Metrics.StatusCode = 413
		ci.logger.Error("Response exceeds size limit - discarding",
			zap.String("request_id", req.RequestID),
			zap.Int("instance_id", ci.ID),
			zap.String("url", req.URL),
			zap.Int("original_size", originalSize),
			zap.Int("max_size", maxHTMLResponseSize))
		return resp, ErrResponseTooLarge
	}

	// Check for soft timeout (navigation wait exceeded but HTML was captured)
	if resp.Metrics.TimedOut {
		resp.ErrorType = types.ErrorTypeSoftTimeout
	}

	// Check for empty response (soft error - still successful but flagged)
	if resp.HTMLSize == 0 && resp.ErrorType == "" {
		resp.ErrorType = types.ErrorTypeEmptyResponse
		ci.logger.Warn("Empty HTML response from origin",
			zap.String("request_id", req.RequestID),
			zap.Int("instance_id", ci.ID),
			zap.String("url", req.URL),
			zap.Int("status_code", resp.Metrics.StatusCode))
	}

	// Parse HTML and extract SEO metadata
	doc, err := htmlprocessor.ParseWithDOM([]byte(resp.HTML))
	if err != nil {
		ci.logger.Warn("Failed to parse HTML for SEO extraction",
			zap.String("request_id", req.RequestID),
			zap.Int("instance_id", ci.ID),
			zap.String("url", req.URL),
			zap.Error(err))
		// Return minimal PageSEO with default IndexStatus
		resp.PageSEO = &types.PageSEO{
			IndexStatus: types.IndexStatusIndexable,
		}
	} else {
		resp.PageSEO = doc.ExtractPageSEO(resp.Metrics.StatusCode, resp.Metrics.FinalURL)

		if req.StripScripts {
			if doc.CleanScripts() {
				cleaned := doc.HTML()
				if len(cleaned) > 0 {
					resp.HTML = string(cleaned)
					resp.HTMLSize = len(resp.HTML)
					ci.logger.Debug("Scripts cleaned from HTML",
						zap.String("request_id", req.RequestID),
						zap.Int("instance_id", ci.ID),
						zap.String("url", req.URL))
				} else {
					ci.logger.Warn("Failed to regenerate HTML after script cleaning",
						zap.String("request_id", req.RequestID),
						zap.Int("instance_id", ci.ID),
						zap.String("url", req.URL))
				}
			}
		}
	}

	// Build and attach HAR if collector exists
	if harCollector != nil {
		// Convert lifecycle events from types to HAR format
		harLifecycleEvents := make([]har.LifecycleEvent, len(resp.Metrics.LifecycleEvents))
		for i, ev := range resp.Metrics.LifecycleEvents {
			harLifecycleEvents[i] = har.LifecycleEvent{
				Name:      ev.Name,
				Timestamp: int64(ev.Time * 1000),
			}
		}

		// Extract message strings for HAR (keeps simple format)
		consoleErrorStrings := make([]string, len(resp.Metrics.ConsoleMessages))
		for i, ce := range resp.Metrics.ConsoleMessages {
			consoleErrorStrings[i] = ce.Message
		}

		// Build HAR from collector
		harData := harCollector.Build(
			harLifecycleEvents,
			consoleErrorStrings,
			har.RenderMetrics{
				Duration:  resp.RenderTime.Milliseconds(),
				TimedOut:  resp.Metrics.TimedOut,
				ServiceID: resp.ChromeID,
			},
			har.RequestConfig{
				WaitFor:              req.WaitFor,
				BlockedPatterns:      req.BlockedPatterns,
				BlockedResourceTypes: req.BlockedResourceTypes,
				ViewportWidth:        req.ViewportWidth,
				ViewportHeight:       req.ViewportHeight,
				UserAgent:            req.UserAgent,
				Timeout:              req.Timeout.Milliseconds(),
				ExtraWait:            req.ExtraWait.Milliseconds(),
			},
			ci.GetBrowserVersion(),
		)

		// Marshal to JSON
		harJSON, err := json.Marshal(harData)
		if err != nil {
			ci.logger.Warn("Failed to marshal HAR to JSON",
				zap.String("request_id", req.RequestID),
				zap.Error(err))
		} else {
			resp.HAR = harJSON
			ci.logger.Debug("HAR generated",
				zap.String("request_id", req.RequestID),
				zap.Int("entries", len(harData.Log.Entries)),
				zap.Int("size_bytes", len(harJSON)))
		}
	}

	// Populate network metrics
	metricsCollector.PopulateMetrics(&resp.Metrics)

	// Set render configuration in metrics (for analytics)
	resp.Metrics.WaitForEvent = req.WaitFor
	resp.Metrics.ExtraWait = req.ExtraWait.Seconds()
	resp.Metrics.Timeout = req.Timeout.Seconds()
	resp.Metrics.ViewportWidth = req.ViewportWidth
	resp.Metrics.ViewportHeight = req.ViewportHeight

	return resp, nil
}

// buildTasks creates the chromedp task sequence for rendering
func (ci *ChromeInstance) buildTasks(req *types.RenderRequest, resp *types.RenderResponse, blocklist *Blocklist,
	statusCodeMu *sync.Mutex, harCollector *har.HARCollector, metricsCollector *NetworkMetricsCollector) chromedp.Tasks {
	timeOrigin := time.Now().UnixMilli()
	targetOrigin := extractOrigin(req.URL)

	// Track active fetch handler goroutines
	var fetchHandlerCount int64

	return chromedp.Tasks{
		// Set up event listeners FIRST - before any CDP commands
		chromedp.ActionFunc(func(ctx context.Context) error {
			chromedp.ListenTarget(ctx, func(event interface{}) {
				switch ev := event.(type) {
				case *fetch.EventRequestPaused:
					// Handle each fetch event in a goroutine to avoid blocking
					atomic.AddInt64(&fetchHandlerCount, 1)
					go func(event *fetch.EventRequestPaused) {
						// Ensure counter decrement happens even if panic occurs
						defer atomic.AddInt64(&fetchHandlerCount, -1)

						// Create timeout context for CDP commands to prevent goroutine leaks
						cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
						defer cancel()

						// Get executor context for CDP commands
						c := chromedp.FromContext(cmdCtx)
						ctxExecutor := cdp.WithExecutor(cmdCtx, c.Target)

						// Check if request should be blocked by URL pattern or resource type
						blockedByURL := blocklist.IsBlocked(event.Request.URL)
						blockedByResourceType := blocklist.IsResourceTypeBlocked(string(event.ResourceType))

						if blockedByURL || blockedByResourceType {
							// Track blocked request in HAR collector
							if harCollector != nil {
								var reason string
								if blockedByURL {
									reason = "Blocked by URL pattern"
								} else {
									reason = "Blocked resource type: " + string(event.ResourceType)
								}
								harCollector.OnRequestBlocked(
									string(event.RequestID),
									event.Request.URL,
									reason,
									string(event.ResourceType),
									float64(time.Now().UnixMilli())/1000,
								)
							}

							// Track blocked request in network metrics
							requestHost := urlutil.ExtractHost(event.Request.URL)
							metricsCollector.OnRequestBlocked(string(event.RequestID), requestHost)

							// Block the request - use ErrorReasonAborted (like production code)
							err := fetch.FailRequest(event.RequestID, network.ErrorReasonAborted).Do(ctxExecutor)
							if err != nil {
								ci.logger.Warn("Failed to block request",
									zap.String("request_id", req.RequestID),
									zap.Int("instance_id", ci.ID),
									zap.String("url", event.Request.URL),
									zap.Error(err))
							}
						} else if len(req.Headers) > 0 && isSameHost(event.Request.URL, targetOrigin) {
							// Same-origin request - inject client headers
							headers := mergeRequestHeaders(event.Request.Headers, req.Headers)
							err := fetch.ContinueRequest(event.RequestID).WithHeaders(headers).Do(ctxExecutor)
							if err != nil {
								ci.logger.Warn("Failed to continue request with headers, failing instead",
									zap.String("request_id", req.RequestID),
									zap.Int("instance_id", ci.ID),
									zap.String("url", event.Request.URL),
									zap.Error(err))
								fetch.FailRequest(event.RequestID, network.ErrorReasonAborted).Do(ctxExecutor)
							}
						} else {
							// Allow the request to continue without modification
							err := fetch.ContinueRequest(event.RequestID).Do(ctxExecutor)
							if err != nil {
								ci.logger.Warn("Failed to continue request, failing instead to prevent hang",
									zap.String("request_id", req.RequestID),
									zap.Int("instance_id", ci.ID),
									zap.String("url", event.Request.URL),
									zap.Error(err))
								// Fallback: fail the request to prevent it from hanging in paused state
								fetch.FailRequest(event.RequestID, network.ErrorReasonAborted).Do(ctxExecutor)
							}
						}
					}(ev)

				case *network.EventRequestWillBeSent:
					// Track first request timing
					metricsCollector.OnRequestSent()

					// Forward to HAR collector
					if harCollector != nil {
						headers := make(map[string]string)
						for k, v := range ev.Request.Headers {
							if str, ok := v.(string); ok {
								headers[k] = str
							}
						}
						harCollector.OnRequestWillBeSent(
							string(ev.RequestID),
							ev.Request.URL,
							ev.Request.Method,
							headers,
							string(ev.Type),
							float64(ev.Timestamp.Time().UnixMilli())/1000,
						)
					}

					if ev.RedirectResponse != nil &&
						urlsMatchIgnoringFragment(ev.RedirectResponse.URL, req.URL) &&
						ev.DocumentURL == ev.Request.URL &&
						ev.RedirectResponse.Status != 0 {
						statusCodeMu.Lock()
						resp.Metrics.StatusCode = int(ev.RedirectResponse.Status)
						resp.Metrics.FinalURL = ev.Request.URL
						statusCodeMu.Unlock()

						err := chromedp.Cancel(ctx)
						if err != nil {
							statusCodeMu.Lock()
							statusCode := resp.Metrics.StatusCode
							statusCodeMu.Unlock()

							ci.logger.Warn("Unable to cancel chrome instance",
								zap.String("request_id", req.RequestID),
								zap.Int("instance_id", ci.ID),
								zap.String("url", req.URL),
								zap.Int("status_code", statusCode))
						}
						return
					}

				case *network.EventResponseReceived:
					// Forward to HAR collector
					if harCollector != nil {
						headers := make(map[string]string)
						for k, v := range ev.Response.Headers {
							if str, ok := v.(string); ok {
								headers[k] = str
							}
						}

						// Convert Chrome timing to HAR timing
						var timing *har.TimingData
						if ev.Response.Timing != nil {
							timing = &har.TimingData{
								DNSStart:          ev.Response.Timing.DNSStart,
								DNSEnd:            ev.Response.Timing.DNSEnd,
								ConnectStart:      ev.Response.Timing.ConnectStart,
								ConnectEnd:        ev.Response.Timing.ConnectEnd,
								SSLStart:          ev.Response.Timing.SslStart,
								SSLEnd:            ev.Response.Timing.SslEnd,
								SendStart:         ev.Response.Timing.SendStart,
								SendEnd:           ev.Response.Timing.SendEnd,
								ReceiveHeadersEnd: ev.Response.Timing.ReceiveHeadersEnd,
							}
						}

						harCollector.OnResponseReceived(
							string(ev.RequestID),
							int(ev.Response.Status),
							ev.Response.StatusText,
							headers,
							ev.Response.MimeType,
							timing,
							ev.Response.Protocol,
						)
					}

					// Capture initial response status code and headers
					statusCodeMu.Lock()
					if urlsMatchIgnoringFragment(ev.Response.URL, req.URL) && resp.Metrics.StatusCode == 0 {
						resp.Metrics.StatusCode = int(ev.Response.Status)

						// Capture response headers (handles both single and multi-value headers)
						if ev.Response.Headers != nil {
							headers := make(map[string][]string)
							for key, value := range ev.Response.Headers {
								switch v := value.(type) {
								case string:
									// Chrome CDP returns multi-value headers as newline-separated string
									if strings.Contains(v, "\n") {
										for _, part := range strings.Split(v, "\n") {
											if trimmed := strings.TrimSpace(part); trimmed != "" {
												headers[key] = append(headers[key], trimmed)
											}
										}
									} else {
										headers[key] = []string{v}
									}
								case []interface{}:
									for _, item := range v {
										if str, ok := item.(string); ok {
											headers[key] = append(headers[key], str)
										}
									}
								}
							}
							resp.Headers = headers
						}
					}
					statusCodeMu.Unlock()

					// Store response metadata for correlation with loadingFinished
					var ttfbMs float64
					if ev.Response.Timing != nil {
						ttfbMs = ev.Response.Timing.ReceiveHeadersEnd // already in ms
					}
					metricsCollector.OnResponseReceived(
						string(ev.RequestID),
						string(ev.Type),
						int(ev.Response.Status),
						ev.Response.URL,
						ttfbMs,
					)

				case *cdpruntime.EventConsoleAPICalled:
					// Capture console errors and warnings
					var consoleType string
					switch ev.Type {
					case cdpruntime.APITypeError:
						consoleType = types.ConsoleTypeError
					case cdpruntime.APITypeWarning:
						consoleType = types.ConsoleTypeWarning
					default:
						return // Ignore other console types (log, info, debug, etc.)
					}

					// Collect message parts from all arguments
					var messageParts []string
					for _, arg := range ev.Args {
						if part := formatConsoleArg(arg); part != "" {
							messageParts = append(messageParts, part)
						}
					}

					if len(messageParts) == 0 {
						return
					}

					// Combine all parts into single message
					message := strings.Join(messageParts, " ")

					// Calculate current total size of messages (message field only)
					currentSize := 0
					for _, ce := range resp.Metrics.ConsoleMessages {
						currentSize += len(ce.Message)
					}

					// Skip if adding this message would exceed limit
					if currentSize+len(message) > maxConsoleMessagesSize {
						return
					}

					sourceURL, sourceLocation := extractSourceInfo(ev.StackTrace)
					resp.Metrics.ConsoleMessages = append(resp.Metrics.ConsoleMessages, types.ConsoleError{
						Type:           consoleType,
						SourceURL:      sourceURL,
						SourceLocation: sourceLocation,
						Message:        message,
					})

				case *network.EventLoadingFinished:
					// Forward to HAR collector
					if harCollector != nil {
						harCollector.OnLoadingFinished(
							string(ev.RequestID),
							int64(ev.EncodedDataLength),
							float64(ev.Timestamp.Time().UnixMilli())/1000,
						)
					}

					// Complete request with final byte count
					metricsCollector.OnLoadingFinished(string(ev.RequestID), int64(ev.EncodedDataLength))

				case *network.EventLoadingFailed:
					// Forward to HAR collector
					if harCollector != nil {
						harCollector.OnLoadingFailed(
							string(ev.RequestID),
							ev.ErrorText,
							ev.Canceled,
							float64(ev.Timestamp.Time().UnixMilli())/1000,
						)
					}

					// Record failed request
					metricsCollector.OnRequestFailed(string(ev.RequestID))
				}
			})
			return nil
		}),

		network.Enable(),

		// Enable fetch interception for request blocking
		fetch.Enable(),

		// Add X-Edge-Render header to prevent nginx loop
		network.SetExtraHTTPHeaders(network.Headers{
			headerEdgeRender: ci.serviceID,
		}),

		network.ClearBrowserCookies(),
		page.Enable(),
		css.Disable(),

		enableLifeCycle(),

		emulation.SetUserAgentOverride(req.UserAgent),
		emulation.SetDeviceMetricsOverride(
			int64(req.ViewportWidth),
			int64(req.ViewportHeight),
			1.0,                     // Default device scale
			req.ViewportWidth < 768, // Mobile if width < 768px
		),

		// Navigate and wait for page ready (with soft timeout)
		ci.navigateAndWait(req, timeOrigin, &resp.Metrics),

		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.WaitVisible("body", chromedp.ByQuery),

		ci.extractHTML(&resp.HTML),

		chromedp.Location(&resp.Metrics.FinalURL),

		// Fallback status code retrieval (if event listener missed it)
		chromedp.ActionFunc(func(ctx context.Context) error {
			statusCodeMu.Lock()
			currentStatusCode := resp.Metrics.StatusCode
			statusCodeMu.Unlock()

			if currentStatusCode == 0 {
				// Event listener missed the status code - use fallback via Performance API
				var statusCodeResult int64
				err := chromedp.Evaluate(`
					(function() {
						try {
							var navEntry = performance.getEntriesByType('navigation')[0];
							if (navEntry && navEntry.responseStatus) {
								return navEntry.responseStatus;
							}
							return 0;
						} catch (e) {
							return 0;
						}
					})()
				`, &statusCodeResult).Do(ctx)

				if err != nil {
					ci.logger.Warn("Failed to retrieve status code via Performance API fallback",
						zap.String("request_id", req.RequestID),
						zap.Int("instance_id", ci.ID),
						zap.String("url", req.URL),
						zap.Error(err))
					return nil // Don't fail the render, just log
				}

				if statusCodeResult > 0 {
					fallbackStatusCode := int(statusCodeResult)

					statusCodeMu.Lock()
					resp.Metrics.StatusCode = fallbackStatusCode
					statusCodeMu.Unlock()

					ci.logger.Info("Status code retrieved via Performance API fallback",
						zap.String("request_id", req.RequestID),
						zap.Int("instance_id", ci.ID),
						zap.String("url", req.URL),
						zap.Int("status_code", fallbackStatusCode))
				}
			}
			return nil
		}),

		// Wait for all fetch handlers to complete BEFORE closing page
		// This ensures all CDP commands finish before tab is destroyed
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Wait for all fetch handler goroutines to finish
			timeout := time.After(5 * time.Second)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			for {
				if atomic.LoadInt64(&fetchHandlerCount) <= 0 {
					return nil
				}

				select {
				case <-timeout:
					ci.logger.Warn("Timeout waiting for fetch handlers to complete",
						zap.String("request_id", req.RequestID),
						zap.Int("instance_id", ci.ID),
						zap.String("url", req.URL),
						zap.Int64("remaining", atomic.LoadInt64(&fetchHandlerCount)))
					return nil
				case <-ticker.C:
					// Continue waiting
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}),

		page.Close(),
	}
}

// navigateAndWait navigates to URL and waits for the specified event
// Supported events: "DOMContentLoaded", "load", "networkIdle", "networkAlmostIdle"
// Uses soft timeout - if wait exceeds timeout, it sets metrics.TimedOut=true but continues
func (ci *ChromeInstance) navigateAndWait(req *types.RenderRequest, timeOrigin int64, metrics *types.PageMetrics) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		// Navigate and capture frame/loader IDs
		frameId, loaderId, _, _, err := page.Navigate(req.URL).Do(ctx)
		if err != nil {
			return errors.Join(ErrNavigateFailed, err)
		}

		// Wait for lifecycle event with timeout (soft - we continue on timeout)
		err = waitForEvent(ctx, req.WaitFor, string(frameId), string(loaderId), req.Timeout, metrics, timeOrigin)

		// If timeout occurred, mark it but don't fail
		if errors.Is(err, ErrWaitTimeout) {
			metrics.TimedOut = true
			ci.logger.Debug("Navigation wait timed out, continuing with HTML extraction",
				zap.String("request_id", req.RequestID),
				zap.Int("instance_id", ci.ID),
				zap.String("url", req.URL),
				zap.Duration("timeout", req.Timeout),
				zap.Bool("timed_out", true))

			} else if err != nil {
			return err
		}

		// Extra wait if requested (skip if already timed out)
		if req.ExtraWait > 0 && !metrics.TimedOut {
			time.Sleep(req.ExtraWait)
		}

		return nil
	}
}

// waitForEvent waits for a specific page lifecycle event matching frameId and loaderId
// Tracks ALL lifecycle events for the page and signals completion when the target event arrives
func waitForEvent(ctx context.Context, eventName, frameId, loaderId string, timeout time.Duration, metrics *types.PageMetrics, timeOrigin int64) error {
	ch := make(chan struct{})

	listenerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	chromedp.ListenTarget(listenerCtx, func(ev interface{}) {
		if e, ok := ev.(*page.EventLifecycleEvent); ok {
			// Match both frameId AND loaderId to track correct navigation
			if string(e.FrameID) == frameId && string(e.LoaderID) == loaderId {
				now := time.Now().UnixMilli()
				delta := now - timeOrigin

				// Track ALL lifecycle events with timestamps
				metrics.LifecycleEvents = append(metrics.LifecycleEvents, types.LifecycleEvent{
					Name: string(e.Name),
					Time: float64(delta) / 1000.0,
				})

				// Signal completion when target event arrives
				if string(e.Name) == eventName {
					cancel()
					close(ch)
				}
			}
		}
	})

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		// Timeout - return specific error so caller can detect it
		return ErrWaitTimeout
	}
}

// extractHTML extracts the page HTML with retry logic
func (ci *ChromeInstance) extractHTML(output *string) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		var lastErr error

		for attempt := 0; attempt < 3; attempt++ {
			// Get document root node
			rootNode, err := dom.GetDocument().Do(ctx)
			if err != nil {
				lastErr = err
				time.Sleep(300 * time.Millisecond)
				continue
			}

			// Extract HTML
			html, err := dom.GetOuterHTML().WithNodeID(rootNode.NodeID).Do(ctx)
			if err != nil {
				lastErr = err
				time.Sleep(300 * time.Millisecond)
				continue
			}

			*output = html
			return nil
		}

		return fmt.Errorf("%w after 3 attempts: %v", ErrExtractHTML, lastErr)
	}
}

// enableLifeCycle enables page lifecycle events
func enableLifeCycle() chromedp.ActionFunc {
	return func(ctx context.Context) error {
		if err := page.Enable().Do(ctx); err != nil {
			return err
		}
		return page.SetLifecycleEventsEnabled(true).Do(ctx)
	}
}

// isSameHost checks if requestURL has the same scheme and host as targetOrigin.
// This is a strict check: scheme, host, and port must match exactly.
// Note: This differs from urlutil.IsSameOrigin which allows subdomains.
func isSameHost(requestURL, targetOrigin string) bool {
	if targetOrigin == "" {
		return false
	}

	reqParsed, err := url.Parse(requestURL)
	if err != nil {
		return false
	}

	targetParsed, err := url.Parse(targetOrigin)
	if err != nil {
		return false
	}

	return reqParsed.Scheme == targetParsed.Scheme &&
		reqParsed.Host == targetParsed.Host
}

// mergeRequestHeaders merges original Chrome headers with injected client headers.
// Injected headers override originals (case-insensitive match).
// Cookie values use "; " separator (RFC 6265), others use ", " (RFC 7230).
func mergeRequestHeaders(original map[string]interface{}, injected map[string][]string) []*fetch.HeaderEntry {
	headers := make([]*fetch.HeaderEntry, 0, len(original)+len(injected))

	// Build lowercase lookup of headers to inject (for deduplication)
	injectedLower := make(map[string]bool, len(injected))
	for name := range injected {
		injectedLower[strings.ToLower(name)] = true
	}

	// Keep original headers that are NOT being overridden
	for name, value := range original {
		if str, ok := value.(string); ok {
			if !injectedLower[strings.ToLower(name)] {
				headers = append(headers, &fetch.HeaderEntry{Name: name, Value: str})
			}
		}
	}

	// Add injected headers (these override originals)
	for name, values := range injected {
		if len(values) == 0 {
			continue
		}
		// Cookie uses "; " separator (RFC 6265), others use ", " (RFC 7230)
		value := values[0]
		if len(values) > 1 {
			separator := ", "
			if strings.EqualFold(name, "cookie") {
				separator = "; "
			}
			value = strings.Join(values, separator)
		}
		headers = append(headers, &fetch.HeaderEntry{Name: name, Value: value})
	}

	return headers
}

// extractOrigin extracts scheme+host from URL for same-origin checking.
func extractOrigin(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
}

// urlsMatchIgnoringFragment compares URLs while ignoring fragments and handling encoding differences
func urlsMatchIgnoringFragment(url1, url2 string) bool {
	// Strip fragments from both URLs
	base1 := url1
	if idx := strings.Index(url1, "#"); idx > -1 {
		base1 = url1[:idx]
	}

	base2 := url2
	if idx := strings.Index(url2, "#"); idx > -1 {
		base2 = url2[:idx]
	}

	// Fast path: exact match
	if base1 == base2 {
		return true
	}

	// Decode both URLs to handle encoding differences
	decoded1, err1 := url.QueryUnescape(base1)
	decoded2, err2 := url.QueryUnescape(base2)
	if err1 == nil && err2 == nil && decoded1 == decoded2 {
		return true
	}

	// Try parsing and comparing as proper URLs (handles more complex cases)
	parsed1, err1 := url.Parse(base1)
	parsed2, err2 := url.Parse(base2)
	if err1 != nil || err2 != nil {
		return false
	}

	// Compare scheme, host, and path (case-sensitive for path, case-insensitive for host)
	if !strings.EqualFold(parsed1.Host, parsed2.Host) {
		return false
	}
	if parsed1.Scheme != parsed2.Scheme {
		return false
	}
	if parsed1.Path != parsed2.Path {
		return false
	}

	// Compare query parameters (order-independent)
	return parsed1.RawQuery == parsed2.RawQuery
}

// formatConsoleArg converts a CDP RemoteObject to a string representation.
// Handles strings, numbers, booleans, null, undefined, and objects.
func formatConsoleArg(arg *cdpruntime.RemoteObject) string {
	// Handle primitive values from JSON
	if len(arg.Value) > 0 {
		raw := string(arg.Value)

		// Try to unquote JSON strings
		if unquoted, err := strconv.Unquote(raw); err == nil {
			return unquoted
		}

		// For numbers, booleans, null - use raw JSON value
		if raw != "null" && raw != "undefined" {
			return raw
		}
	}

	// For objects/functions, use description or type as fallback
	if arg.Description != "" {
		return arg.Description
	}
	if arg.ClassName != "" {
		return "[" + arg.ClassName + "]"
	}
	if string(arg.Type) != "" {
		return "[" + string(arg.Type) + "]"
	}

	return ""
}

// extractSourceInfo extracts source URL and location from a CDP stack trace.
// Returns the source URL and formatted "line:column" location.
// Uses placeholder values when stack trace is unavailable or empty.
func extractSourceInfo(stackTrace *cdpruntime.StackTrace) (sourceURL, sourceLocation string) {
	if stackTrace == nil || len(stackTrace.CallFrames) == 0 {
		return types.AnonymousSourceURL, types.UnknownSourceLocation
	}

	frame := stackTrace.CallFrames[0]

	// Use placeholder for empty URL
	sourceURL = frame.URL
	if sourceURL == "" {
		sourceURL = types.AnonymousSourceURL
	}

	// Convert 0-based line/column to 1-based (clamp negatives to 0 first)
	line := frame.LineNumber
	if line < 0 {
		line = 0
	}
	col := frame.ColumnNumber
	if col < 0 {
		col = 0
	}
	sourceLocation = fmt.Sprintf("%d:%d", line+1, col+1)

	return sourceURL, sourceLocation
}

// categorizeRenderError maps render errors to structured error types
func categorizeRenderError(err error) string {
	if err == nil {
		return ""
	}

	// Check sentinel errors first (most reliable)
	if errors.Is(err, ErrWaitTimeout) {
		return types.ErrorTypeSoftTimeout
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return types.ErrorTypeSoftTimeout
	}
	if errors.Is(err, ErrExtractHTML) {
		return types.ErrorTypeHTMLExtractionFailed
	}
	if errors.Is(err, ErrStatusCapture) {
		return types.ErrorTypeStatusCaptureFailed
	}
	if errors.Is(err, ErrNavigateFailed) {
		return types.ErrorTypeNavigationFailed
	}

	// Fallback: string matching for chromedp/Chrome errors we don't control
	errMsg := strings.ToLower(err.Error())

	// Network errors (Chrome's net::ERR_* codes)
	if strings.Contains(errMsg, "net::err_") ||
		strings.Contains(errMsg, "dns") ||
		strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "ssl") ||
		strings.Contains(errMsg, "tls") ||
		strings.Contains(errMsg, "certificate") {
		return types.ErrorTypeNetworkError
	}

	// Default to navigation failed for unknown render errors
	return types.ErrorTypeNavigationFailed
}
