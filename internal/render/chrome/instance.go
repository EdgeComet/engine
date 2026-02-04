package chrome

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
	"go.uber.org/zap"
)

// NewChromeInstance creates a new Chrome instance with the given configuration
func NewChromeInstance(id int, serviceID string, config *Config, logger *zap.Logger) (*ChromeInstance, error) {
	now := time.Now().UTC()
	instance := &ChromeInstance{
		ID:           id,
		serviceID:    serviceID,
		createdAt:    now,
		logger:       logger,
		status:       int32(ChromeStatusIdle),
		requestsDone: 0,
		lastUsedNano: now.UnixNano(),
	}

	if err := instance.createBrowser(config); err != nil {
		return nil, fmt.Errorf("failed to create Chrome instance %d: %w", id, err)
	}

	instance.logger.Info("Chrome instance created",
		zap.Int("instance_id", id),
		zap.Time("created_at", instance.createdAt))

	// Warmup the instance
	if err := instance.Warmup(config); err != nil {
		instance.logger.Warn("Chrome instance warmup failed",
			zap.Int("instance_id", id),
			zap.Error(err))
		// Don't fail on warmup error, just log it
	}

	return instance, nil
}

// createBrowser initializes the Chrome browser process
func (ci *ChromeInstance) createBrowser(config *Config) error {
	// Build Chrome options with hardcoded flags (no config needed)
	opts := []chromedp.ExecAllocatorOption{
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-translate", true),
	}

	// Create allocator context
	allocatorOpts := append(chromedp.DefaultExecAllocatorOptions[:], opts...)
	ci.allocatorCtx, ci.allocatorCancel = chromedp.NewExecAllocator(context.Background(), allocatorOpts...)

	// Create browser context
	ci.ctx, ci.cancel = chromedp.NewContext(ci.allocatorCtx)

	// Start the browser (this doesn't navigate anywhere yet)
	if err := chromedp.Run(ci.ctx); err != nil {
		return fmt.Errorf("failed to start Chrome: %w", err)
	}

	// Capture browser version for HAR metadata
	if err := chromedp.Run(ci.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, product, _, _, _, err := browser.GetVersion().Do(ctx)
		if err != nil {
			return err
		}
		ci.browserVersion = product
		return nil
	})); err != nil {
		ci.logger.Warn("Failed to capture browser version",
			zap.Int("instance_id", ci.ID),
			zap.Error(err))
	}

	return nil
}

// Warmup navigates to a test page to ensure the browser is ready
func (ci *ChromeInstance) Warmup(config *Config) error {
	ctx, cancel := context.WithTimeout(ci.ctx, config.WarmupTimeout)
	defer cancel()

	err := chromedp.Run(ctx, chromedp.Navigate(config.WarmupURL))
	if err != nil {
		return fmt.Errorf("warmup navigation failed: %w", err)
	}

	ci.logger.Info("Chrome instance warmed up",
		zap.Int("instance_id", ci.ID),
		zap.String("warmup_url", config.WarmupURL))

	return nil
}

// IsAlive checks if the Chrome instance is still responsive
func (ci *ChromeInstance) IsAlive() bool {
	if ChromeStatus(atomic.LoadInt32(&ci.status)) == ChromeStatusDead {
		return false
	}

	// Try to get browser version as a health check
	ctx, cancel := context.WithTimeout(ci.ctx, 5*time.Second)
	defer cancel()

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, product, _, _, _, err := browser.GetVersion().Do(ctx)
		if err != nil {
			return err
		}
		// Successfully got version
		_ = product
		return nil
	}))

	return err == nil
}

// Age returns how long the instance has been running
func (ci *ChromeInstance) Age() time.Duration {
	return time.Now().UTC().Sub(ci.createdAt)
}

// ShouldRestart determines if the instance needs to be restarted based on policies
func (ci *ChromeInstance) ShouldRestart(config *Config) bool {
	// Check request count
	if int(atomic.LoadInt32(&ci.requestsDone)) >= config.RestartAfterCount {
		return true
	}

	// Check age
	if ci.Age() >= config.RestartAfterTime {
		return true
	}

	return false
}

// Restart terminates and recreates the Chrome instance
func (ci *ChromeInstance) Restart(config *Config) error {
	ci.logger.Info("Restarting Chrome instance",
		zap.String("request_id", ci.currentRequestID),
		zap.Int("instance_id", ci.ID),
		zap.Int32("requests_done", ci.GetRequestsDone()),
		zap.Duration("age", ci.Age()))

	// Terminate existing instance
	if err := ci.Terminate(); err != nil {
		ci.logger.Warn("Error terminating instance during restart",
			zap.String("request_id", ci.currentRequestID),
			zap.Int("instance_id", ci.ID),
			zap.Error(err))
	}

	// Reset state
	now := time.Now().UTC()
	atomic.StoreInt32(&ci.requestsDone, 0)
	ci.createdAt = now
	atomic.StoreInt64(&ci.lastUsedNano, now.UnixNano())
	atomic.StoreInt32(&ci.status, int32(ChromeStatusIdle))

	// Create new browser
	if err := ci.createBrowser(config); err != nil {
		atomic.StoreInt32(&ci.status, int32(ChromeStatusDead))
		return fmt.Errorf("%w: %v", ErrRestartFailed, err)
	}

	// Warmup
	if err := ci.Warmup(config); err != nil {
		ci.logger.Warn("Warmup failed after restart",
			zap.String("request_id", ci.currentRequestID),
			zap.Int("instance_id", ci.ID),
			zap.Error(err))
	}

	ci.logger.Info("Chrome instance restarted successfully",
		zap.String("request_id", ci.currentRequestID),
		zap.Int("instance_id", ci.ID))
	return nil
}

// Terminate cleanly shuts down the Chrome instance
func (ci *ChromeInstance) Terminate() error {
	atomic.StoreInt32(&ci.status, int32(ChromeStatusDead))

	// Cancel contexts
	if ci.cancel != nil {
		ci.cancel()
	}
	if ci.allocatorCancel != nil {
		ci.allocatorCancel()
	}

	return nil
}

// IncrementRequests increments the request counter
func (ci *ChromeInstance) IncrementRequests() {
	atomic.AddInt32(&ci.requestsDone, 1)
	atomic.StoreInt64(&ci.lastUsedNano, time.Now().UTC().UnixNano())
}

// GetContext returns a new context for rendering
func (ci *ChromeInstance) GetContext() (context.Context, context.CancelFunc) {
	return chromedp.NewContext(ci.ctx)
}

// GetStatus returns the current status
func (ci *ChromeInstance) GetStatus() ChromeStatus {
	return ChromeStatus(atomic.LoadInt32(&ci.status))
}

// SetStatus updates the instance status
func (ci *ChromeInstance) SetStatus(status ChromeStatus) {
	atomic.StoreInt32(&ci.status, int32(status))
}

// GetRequestsDone returns the number of completed requests
func (ci *ChromeInstance) GetRequestsDone() int32 {
	return atomic.LoadInt32(&ci.requestsDone)
}

// GetLastUsed returns the last used time
func (ci *ChromeInstance) GetLastUsed() time.Time {
	return time.Unix(0, atomic.LoadInt64(&ci.lastUsedNano))
}

// GetBrowserVersion returns the browser version string (e.g., "Chrome/120.0.6099.109")
func (ci *ChromeInstance) GetBrowserVersion() string {
	return ci.browserVersion
}
