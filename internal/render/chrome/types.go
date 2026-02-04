package chrome

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// ChromeStatus represents the current state of a Chrome instance
type ChromeStatus int

const (
	// ChromeStatusIdle indicates the instance is ready for rendering
	ChromeStatusIdle ChromeStatus = iota
	// ChromeStatusRendering indicates the instance is currently processing a request
	ChromeStatusRendering
	// ChromeStatusRestarting indicates the instance is being restarted
	ChromeStatusRestarting
	// ChromeStatusDead indicates the instance has crashed or been terminated
	ChromeStatusDead
)

// headerEdgeRender is the header name used to identify requests from Render Service
const headerEdgeRender = "X-Edge-Render"

// String returns the string representation of ChromeStatus
func (s ChromeStatus) String() string {
	switch s {
	case ChromeStatusIdle:
		return "idle"
	case ChromeStatusRendering:
		return "rendering"
	case ChromeStatusRestarting:
		return "restarting"
	case ChromeStatusDead:
		return "dead"
	default:
		return "unknown"
	}
}

// ChromeInstance represents a single Chrome browser instance
type ChromeInstance struct {
	ID              int                // Immutable
	serviceID       string             // Render Service ID (immutable)
	ctx             context.Context    // Immutable after creation
	cancel          context.CancelFunc // Immutable after creation
	allocatorCtx    context.Context    // Immutable after creation
	allocatorCancel context.CancelFunc // Immutable after creation
	createdAt       time.Time          // Immutable after creation
	logger          *zap.Logger        // Immutable
	browserVersion  string             // Immutable after creation (e.g., "Chrome/120.0.6099.109")

	// Mutable fields - protected by atomic operations
	status           int32 // ChromeStatus as int32
	requestsDone     int32
	lastUsedNano     int64  // Unix nanoseconds
	currentRequestID string // Set by AcquireChrome, cleared by ReleaseChrome
}

// PoolStats represents statistics about the Chrome pool
type PoolStats struct {
	TotalInstances     int
	AvailableInstances int
	ActiveInstances    int
	QueueDepth         int
	TotalRenders       int64
	TotalRestarts      int64
	Uptime             time.Duration
}

// RenderMetrics represents basic metrics collected during rendering
type RenderMetrics struct {
	StatusCode   int
	FinalURL     string
	LoadTime     int64 // milliseconds
	DOMReadyTime int64 // milliseconds
}
