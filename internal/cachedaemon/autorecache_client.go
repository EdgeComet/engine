package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/edge/hash"
	"github.com/edgecomet/engine/pkg/types"
)

// Lua script for conditional ZSET add (autorecache scheduling)
// Adds entry with new score only if:
// - Entry doesn't exist (return 1), OR
// - Existing score is LATER than new score (return 2, reschedule sooner)
// Otherwise no-op (return 0, already scheduled sooner)
const luaConditionalZAdd = `
local current_score = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not current_score then
  redis.call('ZADD', KEYS[1], ARGV[2], ARGV[1])
  return 1
elseif tonumber(current_score) > tonumber(ARGV[2]) then
  redis.call('ZADD', KEYS[1], ARGV[2], ARGV[1])
  return 2
else
  return 0
end
`

// AutorecacheClient handles autorecache scheduling operations
type AutorecacheClient struct {
	redis        *redis.Client
	normalizer   *hash.URLNormalizer
	keyGenerator *redis.KeyGenerator
	logger       *zap.Logger
}

// NewAutorecacheClient creates a new AutorecacheClient
func NewAutorecacheClient(redisClient *redis.Client, logger *zap.Logger) *AutorecacheClient {
	return &AutorecacheClient{
		redis:        redisClient,
		normalizer:   hash.NewURLNormalizer(),
		keyGenerator: redis.NewKeyGenerator(),
		logger:       logger,
	}
}

// ScheduleAutorecache adds URL to autorecache ZSET with conditional logic
// Uses Lua script to ensure earlier scheduled recaches are not overwritten
// Called after bot detection on cache hit
func (ac *AutorecacheClient) ScheduleAutorecache(
	ctx context.Context,
	hostID int,
	url string,
	dimensionID int,
	scheduledAt time.Time,
) error {
	// Normalize URL before ZADD to prevent duplicates
	normalizeResult, err := ac.normalizer.Normalize(url, nil)
	if err != nil {
		return fmt.Errorf("URL normalization failed: %w", err)
	}

	// Build ZSET member (JSON-encoded RecacheMember)
	member := types.RecacheMember{
		URL:         normalizeResult.NormalizedURL,
		DimensionID: dimensionID,
	}
	memberJSON, err := json.Marshal(member)
	if err != nil {
		return fmt.Errorf("failed to marshal RecacheMember: %w", err)
	}

	// Calculate score
	newScore := scheduledAt.Unix()

	// Build ZSET key
	zsetKey := ac.keyGenerator.RecacheQueueKey(hostID, redis.PriorityAutorecache)

	// Execute Lua script for conditional ZADD
	result, err := ac.redis.Eval(ctx, luaConditionalZAdd, []string{zsetKey}, string(memberJSON), newScore)
	if err != nil {
		return fmt.Errorf("failed to update autorecache ZSET: %w", err)
	}

	// Log result (0=noop, 1=added, 2=updated)
	resultCode, ok := result.(int64)
	if ok {
		switch resultCode {
		case 0:
			ac.logger.Debug("Autorecache schedule unchanged (already scheduled sooner)",
				zap.String("host_id", fmt.Sprintf("%d", hostID)),
				zap.String("url", normalizeResult.NormalizedURL),
				zap.Int("dimension_id", dimensionID))
		case 1:
			ac.logger.Info("Autorecache schedule added",
				zap.String("host_id", fmt.Sprintf("%d", hostID)),
				zap.String("url", normalizeResult.NormalizedURL),
				zap.Int("dimension_id", dimensionID),
				zap.Int64("scheduled_at", newScore))
		case 2:
			ac.logger.Info("Autorecache schedule updated (rescheduled sooner)",
				zap.String("host_id", fmt.Sprintf("%d", hostID)),
				zap.String("url", normalizeResult.NormalizedURL),
				zap.Int("dimension_id", dimensionID),
				zap.Int64("scheduled_at", newScore))
		}
	}

	return nil
}
