package registry

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
)

// TabManager handles Redis-based tab reservations for a render service
type TabManager struct {
	redis     *redis.Client
	serviceID string
	tabsKey   string // "tabs:rs-1"
	poolSize  int
	logger    *zap.Logger
}

// NewTabManager creates a new TabManager instance
func NewTabManager(redisClient *redis.Client, serviceID string, poolSize int, logger *zap.Logger) *TabManager {
	return &TabManager{
		redis:     redisClient,
		serviceID: serviceID,
		tabsKey:   fmt.Sprintf("tabs:%s", serviceID),
		poolSize:  poolSize,
		logger:    logger,
	}
}

// RegisterTabs creates Redis hash on startup with all tabs marked as available
func (tm *TabManager) RegisterTabs(ctx context.Context) error {
	// Create all tabs as available (empty string)
	for i := 0; i < tm.poolSize; i++ {
		if err := tm.redis.HSet(ctx, tm.tabsKey, strconv.Itoa(i), ""); err != nil {
			return fmt.Errorf("failed to register tab %d: %w", i, err)
		}
	}

	// Set initial TTL to match service registry
	if err := tm.redis.Expire(ctx, tm.tabsKey, RegistryTTL); err != nil {
		return fmt.Errorf("failed to set initial TTL: %w", err)
	}

	tm.logger.Info("Registered tabs in Redis",
		zap.String("tabs_key", tm.tabsKey),
		zap.Int("pool_size", tm.poolSize))

	return nil
}

// ExtendTTL extends the entire hash TTL
func (tm *TabManager) ExtendTTL(ctx context.Context, ttl time.Duration) error {
	return tm.redis.Expire(ctx, tm.tabsKey, ttl)
}

// SyncTabs efficiently updates tabs hash:
// - If exists: only refresh TTL (lightweight)
// - If missing: recreate entire hash with current occupancy
func (tm *TabManager) SyncTabs(ctx context.Context, acquiredTabs map[int]string, poolSize int) error {
	// Check if key exists
	exists, err := tm.redis.Exists(ctx, tm.tabsKey)
	if err != nil {
		return fmt.Errorf("failed to check tabs key existence: %w", err)
	}

	if exists {
		// Key exists - only refresh TTL (efficient path)
		return tm.redis.Expire(ctx, tm.tabsKey, RegistryTTL)
	}

	// Key missing - recreate entire hash using pipeline
	pipe := tm.redis.GetClient().Pipeline()

	// Set all tabs: acquired with request ID, others as available ("")
	for i := 0; i < poolSize; i++ {
		value := ""
		if reqID, exists := acquiredTabs[i]; exists {
			value = reqID
		}
		pipe.HSet(ctx, tm.tabsKey, strconv.Itoa(i), value)
	}

	// Set TTL to match service registry (3 seconds)
	pipe.Expire(ctx, tm.tabsKey, RegistryTTL)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to recreate tabs hash: %w", err)
	}

	tm.logger.Info("Recreated tabs hash in Redis",
		zap.String("tabs_key", tm.tabsKey),
		zap.Int("pool_size", poolSize),
		zap.Int("acquired_count", len(acquiredTabs)))

	return nil
}

// ClearReservation sets tab value to empty string
func (tm *TabManager) ClearReservation(ctx context.Context, tabID int) error {
	return tm.redis.HSet(ctx, tm.tabsKey, strconv.Itoa(tabID), "")
}

// CountReservations counts non-empty tab values
func (tm *TabManager) CountReservations(ctx context.Context) int {
	allTabs, err := tm.redis.HGetAll(ctx, tm.tabsKey)
	if err != nil {
		tm.logger.Error("Failed to count reservations", zap.Error(err))
		return 0
	}

	count := 0
	for _, value := range allTabs {
		if value != "" {
			count++
		}
	}
	return count
}

// DeleteTabs removes the tabs hash from Redis (called during shutdown)
func (tm *TabManager) DeleteTabs(ctx context.Context) error {
	if err := tm.redis.Del(ctx, tm.tabsKey); err != nil {
		return fmt.Errorf("failed to delete tabs hash: %w", err)
	}

	tm.logger.Info("Deleted tabs hash from Redis", zap.String("tabs_key", tm.tabsKey))
	return nil
}

// GetTabsKey returns the Redis key for this service's tabs hash
func (tm *TabManager) GetTabsKey() string {
	return tm.tabsKey
}

// GetServiceID returns the service ID
func (tm *TabManager) GetServiceID() string {
	return tm.serviceID
}

// GetPoolSize returns the pool size
func (tm *TabManager) GetPoolSize() int {
	return tm.poolSize
}
