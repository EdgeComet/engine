package chrome

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

func TestNetworkMetricsCollector_Basic(t *testing.T) {
	navStart := time.Now()
	collector := NewNetworkMetricsCollector("https://example.com/page", navStart)
	require.NotNil(t, collector)
	assert.Equal(t, "example.com", collector.baseHost)
}

func TestNetworkMetricsCollector_OnRequestSent(t *testing.T) {
	navStart := time.Now()
	collector := NewNetworkMetricsCollector("https://example.com", navStart)

	time.Sleep(10 * time.Millisecond)
	collector.OnRequestSent()

	assert.True(t, collector.hasFirstRequest)
	assert.Greater(t, collector.timeToFirstRequest, 0.0)

	// Second call should not update
	firstTime := collector.timeToFirstRequest
	time.Sleep(10 * time.Millisecond)
	collector.OnRequestSent()
	assert.Equal(t, firstTime, collector.timeToFirstRequest)
}

func TestNetworkMetricsCollector_ResponseAndLoading(t *testing.T) {
	navStart := time.Now()
	collector := NewNetworkMetricsCollector("https://example.com", navStart)

	// Same-origin request
	collector.OnResponseReceived("req1", "Script", 200, "https://example.com/app.js", 0)
	collector.OnLoadingFinished("req1", 1000)
	// Third-party request
	collector.OnResponseReceived("req2", "Image", 200, "https://cdn.other.com/image.png", 0)
	collector.OnLoadingFinished("req2", 5000)
	// Another third-party from same domain
	collector.OnResponseReceived("req3", "Image", 200, "https://cdn.other.com/image2.png", 0)
	collector.OnLoadingFinished("req3", 3000)
	// Different third-party
	collector.OnResponseReceived("req4", "Font", 200, "https://fonts.example.org/font.woff", 0)
	collector.OnLoadingFinished("req4", 2000)
	// Error response
	collector.OnResponseReceived("req5", "XHR", 404, "https://example.com/api/missing", 0)
	collector.OnLoadingFinished("req5", 100)

	assert.Equal(t, 5, collector.totalRequests)
	assert.Equal(t, int64(11100), collector.totalBytes)
	assert.Equal(t, 2, collector.sameOriginRequests)
	assert.Equal(t, int64(1100), collector.sameOriginBytes)
	assert.Equal(t, 3, collector.thirdPartyRequests)
	assert.Equal(t, int64(10000), collector.thirdPartyBytes)
	assert.Equal(t, 2, len(collector.thirdPartyDomains))

	assert.Equal(t, int64(1000), collector.bytesByType["Script"])
	assert.Equal(t, int64(8000), collector.bytesByType["Image"])
	assert.Equal(t, int64(2000), collector.bytesByType["Font"])
	assert.Equal(t, int64(100), collector.bytesByType["XHR"])

	assert.Equal(t, int64(4), collector.statusCounts[types.StatusClass2xx])
	assert.Equal(t, int64(1), collector.statusCounts[types.StatusClass4xx])
}

func TestNetworkMetricsCollector_BlockedAndFailed(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	collector.OnRequestBlocked("blocked1", "tracker.com")
	collector.OnRequestBlocked("blocked2", "ads.network.com")
	collector.OnRequestFailed("req1")

	assert.Equal(t, 2, collector.blockedCount)
	assert.Equal(t, 1, collector.failedCount)
}

func TestNetworkMetricsCollector_BlockedNotCountedAsFailed(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Simulate blocked request flow: OnRequestBlocked then OnRequestFailed (Chrome emits LoadingFailed)
	collector.OnRequestBlocked("blocked-req", "tracker.com")
	collector.OnRequestFailed("blocked-req")

	// Should only count as blocked, not failed
	assert.Equal(t, 1, collector.blockedCount)
	assert.Equal(t, 0, collector.failedCount)

	// A truly failed request (not blocked) should still be counted
	collector.OnRequestFailed("real-failure")
	assert.Equal(t, 1, collector.failedCount)
}

func TestNetworkMetricsCollector_PopulateMetrics(t *testing.T) {
	navStart := time.Now()
	collector := NewNetworkMetricsCollector("https://example.com", navStart)

	collector.OnRequestSent()
	collector.OnResponseReceived("req1", "Script", 200, "https://example.com/app.js", 50) // 50ms TTFB
	time.Sleep(10 * time.Millisecond)
	collector.OnLoadingFinished("req1", 1000)
	collector.OnResponseReceived("req2", "Image", 304, "https://cdn.other.com/image.png", 0)
	collector.OnLoadingFinished("req2", 500)
	collector.OnRequestBlocked("blocked1", "ads.tracker.com")

	// Add a failed request to same domain as successful
	collector.OnResponseReceived("req3", "XHR", 200, "https://cdn.other.com/api", 0)
	collector.OnRequestFailed("req3")

	metrics := &types.PageMetrics{}
	collector.PopulateMetrics(metrics)

	// Existing assertions
	assert.Equal(t, 2, metrics.TotalRequests)
	assert.Equal(t, int64(1500), metrics.TotalBytes)
	assert.Equal(t, 1, metrics.SameOriginRequests)
	assert.Equal(t, 1, metrics.ThirdPartyRequests)
	assert.Equal(t, 1, metrics.ThirdPartyDomains)
	assert.Equal(t, 1, metrics.BlockedCount)
	assert.Equal(t, 1, metrics.FailedCount)
	assert.Greater(t, metrics.TimeToFirstRequest, 0.0)
	assert.Greater(t, metrics.TimeToLastResponse, 0.0)

	// Domain stats assertions
	require.NotNil(t, metrics.DomainStats)
	assert.Len(t, metrics.DomainStats, 3) // example.com, cdn.other.com, ads.tracker.com

	exampleStats := metrics.DomainStats["example.com"]
	require.NotNil(t, exampleStats)
	assert.Equal(t, 1, exampleStats.Requests)
	assert.Equal(t, int64(1000), exampleStats.Bytes)
	assert.Equal(t, 0, exampleStats.Failed)
	assert.Equal(t, 0, exampleStats.Blocked)
	assert.Greater(t, exampleStats.AvgLatency, 0.0)

	cdnStats := metrics.DomainStats["cdn.other.com"]
	require.NotNil(t, cdnStats)
	assert.Equal(t, 1, cdnStats.Requests)
	assert.Equal(t, int64(500), cdnStats.Bytes)
	assert.Equal(t, 1, cdnStats.Failed)
	assert.Equal(t, 0, cdnStats.Blocked)

	trackerStats := metrics.DomainStats["ads.tracker.com"]
	require.NotNil(t, trackerStats)
	assert.Equal(t, 0, trackerStats.Requests)
	assert.Equal(t, 0, trackerStats.Failed)
	assert.Equal(t, 1, trackerStats.Blocked)
}

func TestClassifyStatusCode(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{200, types.StatusClass2xx},
		{201, types.StatusClass2xx},
		{299, types.StatusClass2xx},
		{301, types.StatusClass3xx},
		{304, types.StatusClass3xx},
		{400, types.StatusClass4xx},
		{404, types.StatusClass4xx},
		{500, types.StatusClass5xx},
		{503, types.StatusClass5xx},
		{0, ""},
		{100, ""},
		{199, ""},
		{600, ""},
	}

	for _, tt := range tests {
		result := classifyStatusCode(tt.code)
		assert.Equal(t, tt.expected, result, "code %d", tt.code)
	}
}

func TestNetworkMetricsCollector_EmptyResourceType(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	collector.OnResponseReceived("req1", "", 200, "https://example.com/unknown", 0)
	collector.OnLoadingFinished("req1", 500)

	assert.Equal(t, int64(500), collector.bytesByType[types.ResourceTypeOther])
}

func TestNetworkMetricsCollector_SubdomainSameOrigin(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// www subdomain should be same-origin
	collector.OnResponseReceived("req1", "Script", 200, "https://www.example.com/app.js", 0)
	collector.OnLoadingFinished("req1", 1000)
	// cdn subdomain should be same-origin
	collector.OnResponseReceived("req2", "Image", 200, "https://cdn.example.com/image.png", 0)
	collector.OnLoadingFinished("req2", 2000)
	// nested subdomain should be same-origin
	collector.OnResponseReceived("req3", "Font", 200, "https://static.cdn.example.com/font.woff", 0)
	collector.OnLoadingFinished("req3", 500)

	assert.Equal(t, 3, collector.sameOriginRequests)
	assert.Equal(t, int64(3500), collector.sameOriginBytes)
	assert.Equal(t, 0, collector.thirdPartyRequests)
}

func TestNetworkMetricsCollector_PopulateMetrics_EmptyMaps(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	metrics := &types.PageMetrics{}
	collector.PopulateMetrics(metrics)

	// Maps should be nil when empty (not allocated)
	assert.Nil(t, metrics.BytesByType)
	assert.Nil(t, metrics.StatusCounts)
}

func TestNetworkMetricsCollector_PopulateMetrics_NoDomainStats(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	metrics := &types.PageMetrics{}
	collector.PopulateMetrics(metrics)

	// DomainStats should be nil when empty
	assert.Nil(t, metrics.DomainStats)
}

func TestNetworkMetricsCollector_LoadingFinishedWithoutResponse(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// LoadingFinished without prior ResponseReceived should be ignored
	collector.OnLoadingFinished("unknown-req", 1000)

	assert.Equal(t, 0, collector.totalRequests)
	assert.Equal(t, int64(0), collector.totalBytes)
}

func TestNetworkMetricsCollector_FailedCleansUpPending(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Response received but then failed before loading finished
	collector.OnResponseReceived("req1", "Script", 200, "https://example.com/app.js", 0)
	assert.Equal(t, 1, len(collector.pendingRequests))

	collector.OnRequestFailed("req1")
	assert.Equal(t, 0, len(collector.pendingRequests))
	assert.Equal(t, 1, collector.failedCount)

	// LoadingFinished after failure should be ignored
	collector.OnLoadingFinished("req1", 1000)
	assert.Equal(t, 0, collector.totalRequests)
}

func TestNetworkMetricsCollector_DomainStatsInit(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())
	require.NotNil(t, collector.domainStats)
	assert.Empty(t, collector.domainStats)
}

func TestNetworkMetricsCollector_GetOrCreateDomainStats(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// First call creates new entry
	stats1 := collector.getOrCreateDomainStats("cdn.example.com", true)
	require.NotNil(t, stats1)
	assert.True(t, stats1.isSameOrigin)
	assert.Equal(t, 1, len(collector.domainStats))

	// Second call returns same entry
	stats2 := collector.getOrCreateDomainStats("cdn.example.com", false)
	assert.Same(t, stats1, stats2)
	assert.True(t, stats2.isSameOrigin) // original value preserved
	assert.Equal(t, 1, len(collector.domainStats))

	// Different hostname creates new entry
	stats3 := collector.getOrCreateDomainStats("third-party.com", false)
	require.NotNil(t, stats3)
	assert.False(t, stats3.isSameOrigin)
	assert.Equal(t, 2, len(collector.domainStats))
}

func TestNetworkMetricsCollector_DomainStatsRequests(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Same-origin requests
	collector.OnResponseReceived("req1", "Script", 200, "https://example.com/app.js", 0)
	collector.OnLoadingFinished("req1", 1000)
	collector.OnResponseReceived("req2", "Image", 200, "https://example.com/logo.png", 0)
	collector.OnLoadingFinished("req2", 5000)

	// Third-party request
	collector.OnResponseReceived("req3", "Script", 200, "https://cdn.other.com/lib.js", 0)
	collector.OnLoadingFinished("req3", 2000)

	// Verify domain stats
	assert.Len(t, collector.domainStats, 2)

	exampleStats := collector.domainStats["example.com"]
	require.NotNil(t, exampleStats)
	assert.Equal(t, 2, exampleStats.requests)
	assert.Equal(t, int64(6000), exampleStats.bytes)
	assert.True(t, exampleStats.isSameOrigin)

	cdnStats := collector.domainStats["cdn.other.com"]
	require.NotNil(t, cdnStats)
	assert.Equal(t, 1, cdnStats.requests)
	assert.Equal(t, int64(2000), cdnStats.bytes)
	assert.False(t, cdnStats.isSameOrigin)
}

func TestNetworkMetricsCollector_DomainStatsPortMerge(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Requests to same host with different ports should merge
	collector.OnResponseReceived("req1", "Script", 200, "https://api.example.com:8080/v1", 0)
	collector.OnLoadingFinished("req1", 1000)
	collector.OnResponseReceived("req2", "Script", 200, "https://api.example.com:9090/v2", 0)
	collector.OnLoadingFinished("req2", 2000)
	collector.OnResponseReceived("req3", "Script", 200, "https://api.example.com/v3", 0)
	collector.OnLoadingFinished("req3", 3000)

	// All should merge into "api.example.com"
	assert.Len(t, collector.domainStats, 1)

	apiStats := collector.domainStats["api.example.com"]
	require.NotNil(t, apiStats)
	assert.Equal(t, 3, apiStats.requests)
	assert.Equal(t, int64(6000), apiStats.bytes)
}

func TestNetworkMetricsCollector_DomainStatsLatency(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Simulate requests with TTFB + download time
	// Request 1: 100ms TTFB + 50ms download = ~150ms total
	collector.OnResponseReceived("req1", "Script", 200, "https://example.com/slow.js", 100) // 100ms TTFB
	time.Sleep(50 * time.Millisecond)
	collector.OnLoadingFinished("req1", 1000)

	// Request 2: 50ms TTFB + 10ms download = ~60ms total
	collector.OnResponseReceived("req2", "Script", 200, "https://example.com/fast.js", 50) // 50ms TTFB
	time.Sleep(10 * time.Millisecond)
	collector.OnLoadingFinished("req2", 500)

	stats := collector.domainStats["example.com"]
	require.NotNil(t, stats)
	assert.Equal(t, 2, stats.latencyCount)
	assert.Greater(t, stats.totalLatency, 0.0)

	// Average latency should be around 105ms (150+60)/2
	avgLatency := stats.totalLatency / float64(stats.latencyCount)
	assert.Greater(t, avgLatency, 0.08) // at least 80ms
	assert.Less(t, avgLatency, 0.2)     // less than 200ms
}

func TestNetworkMetricsCollector_DomainStatsFailed(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Successful request
	collector.OnResponseReceived("req1", "Script", 200, "https://api.example.com/data", 0)
	collector.OnLoadingFinished("req1", 1000)

	// Failed request (has response but then fails)
	collector.OnResponseReceived("req2", "Script", 500, "https://api.example.com/broken", 0)
	collector.OnRequestFailed("req2")

	// Another failure
	collector.OnResponseReceived("req3", "XHR", 200, "https://api.example.com/timeout", 0)
	collector.OnRequestFailed("req3")

	stats := collector.domainStats["api.example.com"]
	require.NotNil(t, stats)
	assert.Equal(t, 1, stats.requests) // Only successful request counted
	assert.Equal(t, 2, stats.failed)   // Both failures counted
	assert.Equal(t, int64(1000), stats.bytes)
}

func TestNetworkMetricsCollector_DomainStatsFailedNoLatency(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Successful request with latency (50ms TTFB + 10ms download)
	collector.OnResponseReceived("req1", "Script", 200, "https://example.com/ok.js", 50)
	time.Sleep(10 * time.Millisecond)
	collector.OnLoadingFinished("req1", 1000)

	// Failed request - should not affect latency
	collector.OnResponseReceived("req2", "Script", 200, "https://example.com/fail.js", 200)
	time.Sleep(100 * time.Millisecond) // Long "latency" that shouldn't count
	collector.OnRequestFailed("req2")

	stats := collector.domainStats["example.com"]
	require.NotNil(t, stats)
	assert.Equal(t, 1, stats.latencyCount)  // Only successful request
	assert.Less(t, stats.totalLatency, 0.1) // Should be ~60ms, not 300+ms
}

func TestNetworkMetricsCollector_DomainStatsFailedWithoutResponse(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Request fails before response (DNS error, connection refused)
	// No OnResponseReceived call
	collector.OnRequestFailed("early-fail")

	// Aggregate failedCount should still be incremented
	assert.Equal(t, 1, collector.failedCount)
	// But no domain stats (we don't know the domain)
	assert.Empty(t, collector.domainStats)
}

func TestNetworkMetricsCollector_DomainStatsBlocked(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Block requests from tracker domain
	collector.OnRequestBlocked("b1", "tracker.com")
	collector.OnRequestBlocked("b2", "tracker.com")
	collector.OnRequestBlocked("b3", "ads.network.com")

	assert.Len(t, collector.domainStats, 2)

	trackerStats := collector.domainStats["tracker.com"]
	require.NotNil(t, trackerStats)
	assert.Equal(t, 0, trackerStats.requests)
	assert.Equal(t, 2, trackerStats.blocked)
	assert.False(t, trackerStats.isSameOrigin)

	adsStats := collector.domainStats["ads.network.com"]
	require.NotNil(t, adsStats)
	assert.Equal(t, 1, adsStats.blocked)
}

func TestNetworkMetricsCollector_DomainStatsBlockedEmptyHost(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Block with empty host (edge case)
	collector.OnRequestBlocked("b1", "")

	assert.Equal(t, 1, collector.blockedCount)
	assert.Empty(t, collector.domainStats) // No domain to track
}

func TestNetworkMetricsCollector_DomainStatsUnderLimit(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	// Add 5 domains
	for i := 0; i < 5; i++ {
		host := fmt.Sprintf("domain%d.com", i)
		collector.OnResponseReceived(fmt.Sprintf("req%d", i), "Script", 200, "https://"+host+"/app.js", 0)
		collector.OnLoadingFinished(fmt.Sprintf("req%d", i), 1000)
	}

	relevant := collector.getRelevantDomainStats()
	assert.Len(t, relevant, 5) // All included
}

func TestNetworkMetricsCollector_DomainStatsOverLimit(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://main.com", time.Now())

	// Add 150 third-party domains with varying request counts
	for i := 0; i < 150; i++ {
		host := fmt.Sprintf("third-party-%03d.com", i)
		// Higher numbered domains get more requests
		requestCount := i + 1
		for j := 0; j < requestCount; j++ {
			reqID := fmt.Sprintf("req-%d-%d", i, j)
			collector.OnResponseReceived(reqID, "Script", 200, "https://"+host+"/app.js", 0)
			collector.OnLoadingFinished(reqID, 100)
		}
	}

	// Add same-origin with just 1 request (low count)
	collector.OnResponseReceived("main-req", "Document", 200, "https://main.com/", 0)
	collector.OnLoadingFinished("main-req", 5000)

	relevant := collector.getRelevantDomainStats()
	assert.Len(t, relevant, maxDomainStats)

	// Same-origin must be included despite low request count
	mainStats, exists := relevant["main.com"]
	assert.True(t, exists, "same-origin should always be included")
	assert.Equal(t, 1, mainStats.requests)
	assert.True(t, mainStats.isSameOrigin)

	// Top third-party domains should be included (highest request counts)
	// third-party-149.com has 150 requests, should definitely be included
	topStats, exists := relevant["third-party-149.com"]
	assert.True(t, exists, "highest request count domain should be included")
	assert.Equal(t, 150, topStats.requests)
}

func TestNetworkMetricsCollector_DomainStatsEmpty(t *testing.T) {
	collector := NewNetworkMetricsCollector("https://example.com", time.Now())

	relevant := collector.getRelevantDomainStats()
	assert.Nil(t, relevant)
}
