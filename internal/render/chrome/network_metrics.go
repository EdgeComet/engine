package chrome

import (
	"sort"
	"sync"
	"time"

	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/pkg/types"
)

const maxDomainStats = 100

type pendingRequest struct {
	resourceType string
	statusCode   int
	requestURL   string
	requestHost  string
	startTimeMs  float64 // ms from navigation start (for download time calc)
	ttfbMs       float64 // ms: time to first byte from Chrome
}

// domainStats tracks internal per-domain metrics during collection.
// This is converted to types.DomainStats when populating final metrics.
type domainStats struct {
	requests     int
	bytes        int64
	failed       int
	blocked      int
	totalLatency float64 // sum of latencies for averaging
	latencyCount int     // requests with latency data (excludes failed)
	isSameOrigin bool
}

// NetworkMetricsCollector collects lightweight network metrics during rendering.
// Thread-safe for concurrent event handler calls.
type NetworkMetricsCollector struct {
	mu                 sync.Mutex
	baseHost           string
	pendingRequests    map[string]*pendingRequest
	blockedRequestIDs  map[string]struct{}
	bytesByType        map[string]int64
	requestsByType     map[string]int64
	statusCounts       map[string]int64
	totalRequests      int
	totalBytes         int64
	sameOriginRequests int
	sameOriginBytes    int64
	thirdPartyRequests int
	thirdPartyBytes    int64
	thirdPartyDomains  map[string]struct{}
	blockedCount       int
	failedCount        int
	timeToFirstRequest float64
	timeToLastResponse float64
	navStartTime       time.Time
	hasFirstRequest    bool
	domainStats        map[string]*domainStats // keyed by hostname (no port)
}

// NewNetworkMetricsCollector creates a new collector for the given base URL.
// navStart should be time.Now() from the start of the render.
func NewNetworkMetricsCollector(baseURL string, navStart time.Time) *NetworkMetricsCollector {
	return &NetworkMetricsCollector{
		baseHost:          urlutil.ExtractHost(baseURL),
		pendingRequests:   make(map[string]*pendingRequest),
		blockedRequestIDs: make(map[string]struct{}),
		bytesByType:       make(map[string]int64),
		requestsByType:    make(map[string]int64),
		statusCounts:      make(map[string]int64),
		thirdPartyDomains: make(map[string]struct{}),
		domainStats:       make(map[string]*domainStats),
		navStartTime:      navStart,
	}
}

// OnRequestSent records when a request is sent.
// Only the first request updates TimeToFirstRequest.
func (c *NetworkMetricsCollector) OnRequestSent() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.hasFirstRequest {
		c.timeToFirstRequest = time.Since(c.navStartTime).Seconds()
		c.hasFirstRequest = true
	}
}

// OnResponseReceived stores response metadata for correlation with LoadingFinished.
// ttfbMs is the time to first byte in milliseconds from Chrome's timing data.
func (c *NetworkMetricsCollector) OnResponseReceived(requestID, resourceType string, statusCode int, requestURL string, ttfbMs float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pendingRequests[requestID] = &pendingRequest{
		resourceType: resourceType,
		statusCode:   statusCode,
		requestURL:   requestURL,
		requestHost:  urlutil.ExtractHost(requestURL),
		startTimeMs:  float64(time.Since(c.navStartTime).Milliseconds()),
		ttfbMs:       ttfbMs,
	}
}

// OnLoadingFinished completes a request with final byte count.
func (c *NetworkMetricsCollector) OnLoadingFinished(requestID string, bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, ok := c.pendingRequests[requestID]
	if !ok {
		return
	}
	delete(c.pendingRequests, requestID)

	c.timeToLastResponse = time.Since(c.navStartTime).Seconds()

	c.totalRequests++
	c.totalBytes += bytes

	resourceType := req.resourceType
	if resourceType == "" {
		resourceType = types.ResourceTypeOther
	}
	c.bytesByType[resourceType] += bytes
	c.requestsByType[resourceType]++

	statusClass := classifyStatusCode(req.statusCode)
	if statusClass != "" {
		c.statusCounts[statusClass]++
	}

	// Extract hostname without port for consistent tracking
	hostname := urlutil.ExtractHostname(req.requestHost)

	isSameOrigin := urlutil.IsSameOrigin(c.baseHost, req.requestHost)
	if isSameOrigin {
		c.sameOriginRequests++
		c.sameOriginBytes += bytes
	} else {
		c.thirdPartyRequests++
		c.thirdPartyBytes += bytes
		if hostname != "" {
			c.thirdPartyDomains[hostname] = struct{}{}
		}
	}

	// Track per-domain statistics
	if hostname != "" {
		stats := c.getOrCreateDomainStats(hostname, isSameOrigin)
		stats.requests++
		stats.bytes += bytes

		// Calculate total latency: TTFB + download time (in ms, then convert to seconds)
		if req.ttfbMs > 0 || req.startTimeMs > 0 {
			downloadTimeMs := float64(time.Since(c.navStartTime).Milliseconds()) - req.startTimeMs
			totalLatencyMs := req.ttfbMs + downloadTimeMs
			if totalLatencyMs > 0 {
				stats.totalLatency += totalLatencyMs / 1000 // store in seconds
				stats.latencyCount++
			}
		}
	}
}

// OnRequestBlocked records a blocked request.
// requestHost should be the host (may include port) from the blocked request URL.
func (c *NetworkMetricsCollector) OnRequestBlocked(requestID string, requestHost string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.blockedCount++
	c.blockedRequestIDs[requestID] = struct{}{}

	// Track per-domain blocked count
	hostname := urlutil.ExtractHostname(requestHost)
	if hostname != "" {
		isSameOrigin := urlutil.IsSameOrigin(c.baseHost, requestHost)
		stats := c.getOrCreateDomainStats(hostname, isSameOrigin)
		stats.blocked++
	}
}

// OnRequestFailed increments the failed request counter and cleans up pending request.
// Requests that were intentionally blocked are not counted as failed.
func (c *NetworkMetricsCollector) OnRequestFailed(requestID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, ok := c.pendingRequests[requestID]
	if !ok {
		// Request failed before response - no domain info available
		// Still counted in aggregate failedCount below if not blocked
		if _, wasBlocked := c.blockedRequestIDs[requestID]; !wasBlocked {
			c.failedCount++
		}
		return
	}

	delete(c.pendingRequests, requestID)

	if _, wasBlocked := c.blockedRequestIDs[requestID]; !wasBlocked {
		c.failedCount++

		// Track per-domain failure (only if we have host info from response)
		hostname := urlutil.ExtractHostname(req.requestHost)
		if hostname != "" {
			isSameOrigin := urlutil.IsSameOrigin(c.baseHost, req.requestHost)
			stats := c.getOrCreateDomainStats(hostname, isSameOrigin)
			stats.failed++
			// Note: failed requests do NOT contribute to latency average
		}
	}
}

// PopulateMetrics fills the network-related fields of PageMetrics.
func (c *NetworkMetricsCollector) PopulateMetrics(metrics *types.PageMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	metrics.TotalRequests = c.totalRequests
	metrics.TotalBytes = c.totalBytes
	metrics.SameOriginRequests = c.sameOriginRequests
	metrics.SameOriginBytes = c.sameOriginBytes
	metrics.ThirdPartyRequests = c.thirdPartyRequests
	metrics.ThirdPartyBytes = c.thirdPartyBytes
	metrics.ThirdPartyDomains = len(c.thirdPartyDomains)
	metrics.BlockedCount = c.blockedCount
	metrics.FailedCount = c.failedCount
	metrics.TimeToFirstRequest = c.timeToFirstRequest
	metrics.TimeToLastResponse = c.timeToLastResponse

	if len(c.bytesByType) > 0 {
		metrics.BytesByType = make(map[string]int64, len(c.bytesByType))
		for k, v := range c.bytesByType {
			metrics.BytesByType[k] = v
		}
	}

	if len(c.requestsByType) > 0 {
		metrics.RequestsByType = make(map[string]int64, len(c.requestsByType))
		for k, v := range c.requestsByType {
			metrics.RequestsByType[k] = v
		}
	}

	if len(c.statusCounts) > 0 {
		metrics.StatusCounts = make(map[string]int64, len(c.statusCounts))
		for k, v := range c.statusCounts {
			metrics.StatusCounts[k] = v
		}
	}

	// Populate domain stats (with 100-domain limit)
	relevantStats := c.getRelevantDomainStats()
	if len(relevantStats) > 0 {
		metrics.DomainStats = make(map[string]*types.DomainStats, len(relevantStats))
		for domain, stats := range relevantStats {
			ds := &types.DomainStats{
				Requests: stats.requests,
				Bytes:    stats.bytes,
				Failed:   stats.failed,
				Blocked:  stats.blocked,
			}
			// Calculate average latency (only if we have data)
			if stats.latencyCount > 0 {
				ds.AvgLatency = stats.totalLatency / float64(stats.latencyCount)
			}
			metrics.DomainStats[domain] = ds
		}
	}
}

// getOrCreateDomainStats returns existing stats for hostname or creates new entry.
// hostname should already have port stripped.
func (c *NetworkMetricsCollector) getOrCreateDomainStats(hostname string, isSameOrigin bool) *domainStats {
	stats, exists := c.domainStats[hostname]
	if !exists {
		stats = &domainStats{isSameOrigin: isSameOrigin}
		c.domainStats[hostname] = stats
	}
	return stats
}

// getRelevantDomainStats returns domain stats capped at maxDomainStats entries.
// Domains are sorted by request count descending, with same-origin always included.
func (c *NetworkMetricsCollector) getRelevantDomainStats() map[string]*domainStats {
	if len(c.domainStats) == 0 {
		return nil
	}

	// If under limit, return all
	if len(c.domainStats) <= maxDomainStats {
		return c.domainStats
	}

	// Sort domains by request count descending
	type domainEntry struct {
		hostname string
		stats    *domainStats
	}
	entries := make([]domainEntry, 0, len(c.domainStats))
	var sameOriginEntry *domainEntry

	for hostname, stats := range c.domainStats {
		entry := domainEntry{hostname, stats}
		entries = append(entries, entry)
		// Track same-origin entry with highest request count for deterministic selection
		if stats.isSameOrigin {
			if sameOriginEntry == nil || stats.requests > sameOriginEntry.stats.requests {
				sameOriginEntry = &domainEntry{hostname, stats}
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].stats.requests > entries[j].stats.requests
	})

	// Determine if same-origin will be in top maxDomainStats
	sameOriginInTop := false
	for i := 0; i < maxDomainStats && i < len(entries); i++ {
		if entries[i].stats.isSameOrigin {
			sameOriginInTop = true
			break
		}
	}

	// Reserve a slot for same-origin if it won't be in top entries
	limit := maxDomainStats
	if !sameOriginInTop && sameOriginEntry != nil {
		limit = maxDomainStats - 1
	}

	// Take top entries
	result := make(map[string]*domainStats, maxDomainStats)
	for i := 0; i < len(entries) && len(result) < limit; i++ {
		result[entries[i].hostname] = entries[i].stats
	}

	// Add same-origin if not already included
	if !sameOriginInTop && sameOriginEntry != nil {
		result[sameOriginEntry.hostname] = sameOriginEntry.stats
	}

	return result
}

func classifyStatusCode(code int) string {
	switch {
	case code >= 200 && code < 300:
		return types.StatusClass2xx
	case code >= 300 && code < 400:
		return types.StatusClass3xx
	case code >= 400 && code < 500:
		return types.StatusClass4xx
	case code >= 500 && code < 600:
		return types.StatusClass5xx
	default:
		return ""
	}
}
