package main

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

type MismatchDetail struct {
	URL            string
	ExpectedStatus int
	ActualStatus   int
	RequestID      string
}

type requestSnapshot struct {
	timestamp time.Time
	count     int64
}

type GlobalStats struct {
	TotalRequests    int64
	Success2xx       int64
	Redirect3xx      int64
	ClientError4xx   int64
	ServerError5xx   int64
	NetworkErrors    int64
	TimeoutErrors    int64
	ConnectionErrors int64

	StatusMismatches int64
	Mismatches       []MismatchDetail
	mismatchMu       sync.Mutex

	CacheHits   int64
	Rendered    int64
	Bypass      int64
	BypassCache int64

	TotalBytes int64

	ResponseTimes            *hdrhistogram.Histogram
	ResponseTimesCache       *hdrhistogram.Histogram
	ResponseTimesRendered    *hdrhistogram.Histogram
	ResponseTimesBypass      *hdrhistogram.Histogram
	ResponseTimesBypassCache *hdrhistogram.Histogram
	histogramMu              sync.Mutex

	HostStats map[string]*HostStats
	mu        sync.RWMutex

	startTime     time.Time
	lastRPSCheck  time.Time
	lastRPSCount  int64
	currentRPS    float64
	lastBWCheck   time.Time
	lastBWBytes   int64
	currentBWRate float64

	cacheSnapshots       []requestSnapshot
	renderedSnapshots    []requestSnapshot
	bypassSnapshots      []requestSnapshot
	bypassCacheSnapshots []requestSnapshot
	cacheRPS             float64
	renderedRPS          float64
	bypassRPS            float64
	bypassCacheRPS       float64
	snapshotsMu          sync.Mutex

	activeRequests  *int64
	baseConcurrency int
}

type HostStats struct {
	TotalRequests    int64
	Success2xx       int64
	Redirect3xx      int64
	ClientError4xx   int64
	ServerError5xx   int64
	NetworkErrors    int64
	TimeoutErrors    int64
	ConnectionErrors int64

	StatusMismatches int64

	CacheHits   int64
	Rendered    int64
	Bypass      int64
	BypassCache int64

	TotalBytes int64

	ResponseTimes *hdrhistogram.Histogram
	histogramMu   sync.Mutex
}

func NewGlobalStats() *GlobalStats {
	return &GlobalStats{
		ResponseTimes:            hdrhistogram.New(1, 300000, 3),
		ResponseTimesCache:       hdrhistogram.New(1, 300000, 3),
		ResponseTimesRendered:    hdrhistogram.New(1, 300000, 3),
		ResponseTimesBypass:      hdrhistogram.New(1, 300000, 3),
		ResponseTimesBypassCache: hdrhistogram.New(1, 300000, 3),
		HostStats:                make(map[string]*HostStats),
		Mismatches:               make([]MismatchDetail, 0),
		startTime:                time.Now(),
		lastRPSCheck:             time.Now(),
		lastBWCheck:              time.Now(),
		cacheSnapshots:           make([]requestSnapshot, 0, 60),
		renderedSnapshots:        make([]requestSnapshot, 0, 60),
		bypassSnapshots:          make([]requestSnapshot, 0, 60),
		bypassCacheSnapshots:     make([]requestSnapshot, 0, 60),
	}
}

func NewHostStats() *HostStats {
	return &HostStats{
		ResponseTimes: hdrhistogram.New(1, 300000, 3),
	}
}

func (gs *GlobalStats) RecordRequest(result *RequestResult) {
	atomic.AddInt64(&gs.TotalRequests, 1)

	if result.Success {
		gs.histogramMu.Lock()
		gs.ResponseTimes.RecordValue(result.Duration.Milliseconds())
		gs.histogramMu.Unlock()
		atomic.AddInt64(&gs.TotalBytes, int64(result.BytesReceived))

		switch {
		case result.StatusCode >= 200 && result.StatusCode < 300:
			atomic.AddInt64(&gs.Success2xx, 1)
		case result.StatusCode >= 300 && result.StatusCode < 400:
			atomic.AddInt64(&gs.Redirect3xx, 1)
		case result.StatusCode >= 400 && result.StatusCode < 500:
			atomic.AddInt64(&gs.ClientError4xx, 1)
		case result.StatusCode >= 500 && result.StatusCode < 600:
			atomic.AddInt64(&gs.ServerError5xx, 1)
		}

		switch result.RenderSource {
		case "cache":
			atomic.AddInt64(&gs.CacheHits, 1)
			gs.histogramMu.Lock()
			gs.ResponseTimesCache.RecordValue(result.Duration.Milliseconds())
			gs.histogramMu.Unlock()
		case "rendered":
			atomic.AddInt64(&gs.Rendered, 1)
			gs.histogramMu.Lock()
			gs.ResponseTimesRendered.RecordValue(result.Duration.Milliseconds())
			gs.histogramMu.Unlock()
		case "bypass":
			atomic.AddInt64(&gs.Bypass, 1)
			gs.histogramMu.Lock()
			gs.ResponseTimesBypass.RecordValue(result.Duration.Milliseconds())
			gs.histogramMu.Unlock()
		case "bypass_cache":
			atomic.AddInt64(&gs.BypassCache, 1)
			gs.histogramMu.Lock()
			gs.ResponseTimesBypassCache.RecordValue(result.Duration.Milliseconds())
			gs.histogramMu.Unlock()
		}

		if result.IsMismatch {
			atomic.AddInt64(&gs.StatusMismatches, 1)
			gs.mismatchMu.Lock()
			gs.Mismatches = append(gs.Mismatches, MismatchDetail{
				URL:            result.URL,
				ExpectedStatus: result.ExpectedStatus,
				ActualStatus:   result.StatusCode,
				RequestID:      result.RequestID,
			})
			gs.mismatchMu.Unlock()
		}
	} else {
		atomic.AddInt64(&gs.NetworkErrors, 1)

		switch result.Error {
		case "timeout":
			atomic.AddInt64(&gs.TimeoutErrors, 1)
		case "connection_refused":
			atomic.AddInt64(&gs.ConnectionErrors, 1)
		}
	}

	gs.recordHostStats(result)
}

func (gs *GlobalStats) recordHostStats(result *RequestResult) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	host := result.Host
	if host == "" {
		return
	}

	hostStats, exists := gs.HostStats[host]
	if !exists {
		hostStats = NewHostStats()
		gs.HostStats[host] = hostStats
	}

	atomic.AddInt64(&hostStats.TotalRequests, 1)

	if result.Success {
		hostStats.histogramMu.Lock()
		hostStats.ResponseTimes.RecordValue(result.Duration.Milliseconds())
		hostStats.histogramMu.Unlock()
		atomic.AddInt64(&hostStats.TotalBytes, int64(result.BytesReceived))

		switch {
		case result.StatusCode >= 200 && result.StatusCode < 300:
			atomic.AddInt64(&hostStats.Success2xx, 1)
		case result.StatusCode >= 300 && result.StatusCode < 400:
			atomic.AddInt64(&hostStats.Redirect3xx, 1)
		case result.StatusCode >= 400 && result.StatusCode < 500:
			atomic.AddInt64(&hostStats.ClientError4xx, 1)
		case result.StatusCode >= 500 && result.StatusCode < 600:
			atomic.AddInt64(&hostStats.ServerError5xx, 1)
		}

		switch result.RenderSource {
		case "cache":
			atomic.AddInt64(&hostStats.CacheHits, 1)
		case "rendered":
			atomic.AddInt64(&hostStats.Rendered, 1)
		case "bypass":
			atomic.AddInt64(&hostStats.Bypass, 1)
		case "bypass_cache":
			atomic.AddInt64(&hostStats.BypassCache, 1)
		}

		if result.IsMismatch {
			atomic.AddInt64(&hostStats.StatusMismatches, 1)
		}
	} else {
		atomic.AddInt64(&hostStats.NetworkErrors, 1)

		switch result.Error {
		case "timeout":
			atomic.AddInt64(&hostStats.TimeoutErrors, 1)
		case "connection_refused":
			atomic.AddInt64(&hostStats.ConnectionErrors, 1)
		}
	}
}

func (gs *GlobalStats) UpdateRPS() {
	now := time.Now()
	elapsed := now.Sub(gs.lastRPSCheck).Seconds()
	if elapsed > 0 {
		currentCount := atomic.LoadInt64(&gs.TotalRequests)
		newRequests := currentCount - gs.lastRPSCount
		gs.currentRPS = float64(newRequests) / elapsed
		gs.lastRPSCheck = now
		gs.lastRPSCount = currentCount
	}
}

func (gs *GlobalStats) UpdateBandwidthRate() {
	now := time.Now()
	elapsed := now.Sub(gs.lastBWCheck).Seconds()
	if elapsed > 0 {
		currentBytes := atomic.LoadInt64(&gs.TotalBytes)
		newBytes := currentBytes - gs.lastBWBytes
		gs.currentBWRate = float64(newBytes) / elapsed
		gs.lastBWCheck = now
		gs.lastBWBytes = currentBytes
	}
}

func (gs *GlobalStats) UpdateSourceRPS() {
	now := time.Now()
	cutoff := now.Add(-60 * time.Second)

	gs.snapshotsMu.Lock()
	defer gs.snapshotsMu.Unlock()

	cacheCount := atomic.LoadInt64(&gs.CacheHits)
	renderedCount := atomic.LoadInt64(&gs.Rendered)
	bypassCount := atomic.LoadInt64(&gs.Bypass)
	bypassCacheCount := atomic.LoadInt64(&gs.BypassCache)

	gs.cacheSnapshots = append(gs.cacheSnapshots, requestSnapshot{timestamp: now, count: cacheCount})
	gs.renderedSnapshots = append(gs.renderedSnapshots, requestSnapshot{timestamp: now, count: renderedCount})
	gs.bypassSnapshots = append(gs.bypassSnapshots, requestSnapshot{timestamp: now, count: bypassCount})
	gs.bypassCacheSnapshots = append(gs.bypassCacheSnapshots, requestSnapshot{timestamp: now, count: bypassCacheCount})

	gs.cacheSnapshots = removeOldSnapshots(gs.cacheSnapshots, cutoff)
	gs.renderedSnapshots = removeOldSnapshots(gs.renderedSnapshots, cutoff)
	gs.bypassSnapshots = removeOldSnapshots(gs.bypassSnapshots, cutoff)
	gs.bypassCacheSnapshots = removeOldSnapshots(gs.bypassCacheSnapshots, cutoff)

	gs.cacheRPS = calculateRPS(gs.cacheSnapshots, cacheCount)
	gs.renderedRPS = calculateRPS(gs.renderedSnapshots, renderedCount)
	gs.bypassRPS = calculateRPS(gs.bypassSnapshots, bypassCount)
	gs.bypassCacheRPS = calculateRPS(gs.bypassCacheSnapshots, bypassCacheCount)
}

func removeOldSnapshots(snapshots []requestSnapshot, cutoff time.Time) []requestSnapshot {
	firstValid := 0
	for i, snap := range snapshots {
		if snap.timestamp.After(cutoff) {
			firstValid = i
			break
		}
	}
	if firstValid > 0 && len(snapshots) > 0 {
		return snapshots[firstValid:]
	}
	return snapshots
}

func calculateRPS(snapshots []requestSnapshot, currentCount int64) float64 {
	if len(snapshots) < 2 {
		return 0.0
	}
	oldestSnapshot := snapshots[0]
	elapsed := time.Since(oldestSnapshot.timestamp).Seconds()
	if elapsed == 0 {
		return 0.0
	}
	requestDelta := currentCount - oldestSnapshot.count
	return float64(requestDelta) / elapsed
}

func (gs *GlobalStats) GetCurrentRPS() float64 {
	return gs.currentRPS
}

func (gs *GlobalStats) GetCurrentBWRate() float64 {
	return gs.currentBWRate
}

func (gs *GlobalStats) GetCacheRPS() float64 {
	gs.snapshotsMu.Lock()
	defer gs.snapshotsMu.Unlock()
	return gs.cacheRPS
}

func (gs *GlobalStats) GetRenderedRPS() float64 {
	gs.snapshotsMu.Lock()
	defer gs.snapshotsMu.Unlock()
	return gs.renderedRPS
}

func (gs *GlobalStats) GetBypassRPS() float64 {
	gs.snapshotsMu.Lock()
	defer gs.snapshotsMu.Unlock()
	return gs.bypassRPS
}

func (gs *GlobalStats) GetBypassCacheRPS() float64 {
	gs.snapshotsMu.Lock()
	defer gs.snapshotsMu.Unlock()
	return gs.bypassCacheRPS
}

func (gs *GlobalStats) GetAverageRPS(source string, duration time.Duration) float64 {
	if duration.Seconds() == 0 {
		return 0.0
	}

	var count int64
	switch source {
	case "total":
		count = atomic.LoadInt64(&gs.TotalRequests)
	case "cache":
		count = atomic.LoadInt64(&gs.CacheHits)
	case "rendered":
		count = atomic.LoadInt64(&gs.Rendered)
	case "bypass":
		count = atomic.LoadInt64(&gs.Bypass)
	case "bypass_cache":
		count = atomic.LoadInt64(&gs.BypassCache)
	default:
		return 0.0
	}

	return float64(count) / duration.Seconds()
}

func (gs *GlobalStats) SetActiveRequests(activeRequests *int64, baseConcurrency int) {
	gs.activeRequests = activeRequests
	gs.baseConcurrency = baseConcurrency
}

func (gs *GlobalStats) GetActiveRequests() int64 {
	if gs.activeRequests == nil {
		return 0
	}
	return atomic.LoadInt64(gs.activeRequests)
}
