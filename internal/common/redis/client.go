package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
)

type Client struct {
	rdb    *redis.Client
	logger *zap.Logger
	config *config.RedisConfig
}

func NewClient(cfg *config.RedisConfig, logger *zap.Logger) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("redis config is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Use go-redis library defaults:
	// - DialTimeout: 5s
	// - ReadTimeout: 3s
	// - WriteTimeout: 3s
	// - PoolSize: 10 * runtime.GOMAXPROCS(0)
	// - MinIdleConns: 0
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	client := &Client{
		rdb:    rdb,
		logger: logger,
		config: cfg,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Debug("Redis client connected successfully",
		zap.String("addr", cfg.Addr),
		zap.Int("db", cfg.DB))

	return client, nil
}

func (c *Client) Ping(ctx context.Context) error {
	result, err := c.rdb.Ping(ctx).Result()
	if err != nil {
		c.logger.Error("Redis ping failed", zap.Error(err))
		return err
	}

	if result != "PONG" {
		err := fmt.Errorf("unexpected ping response: %s", result)
		c.logger.Error("Redis ping returned unexpected response", zap.String("response", result))
		return err
	}

	return nil
}

func (c *Client) HealthCheck(ctx context.Context) error {
	start := time.Now().UTC()

	if err := c.Ping(ctx); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	duration := time.Since(start)
	c.logger.Debug("Redis health check passed", zap.Duration("duration", duration))
	return nil
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	result, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		c.logger.Error("Redis GET failed",
			zap.String("key", key),
			zap.Error(err))
		return "", fmt.Errorf("redis get failed: %w", err)
	}
	return result, nil
}

func (c *Client) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	err := c.rdb.Set(ctx, key, value, expiration).Err()
	if err != nil {
		c.logger.Error("Redis SET failed",
			zap.String("key", key),
			zap.Duration("expiration", expiration),
			zap.Error(err))
		return fmt.Errorf("redis set failed: %w", err)
	}
	return nil
}

func (c *Client) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	result, err := c.rdb.SetNX(ctx, key, value, expiration).Result()
	if err != nil {
		c.logger.Error("Redis SETNX failed",
			zap.String("key", key),
			zap.Duration("expiration", expiration),
			zap.Error(err))
		return false, fmt.Errorf("redis setnx failed: %w", err)
	}
	return result, nil
}

func (c *Client) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	err := c.rdb.Del(ctx, keys...).Err()
	if err != nil {
		c.logger.Error("Redis DEL failed",
			zap.Strings("keys", keys),
			zap.Error(err))
		return fmt.Errorf("redis del failed: %w", err)
	}
	return nil
}

// HDel deletes one or more hash fields
func (c *Client) HDel(ctx context.Context, key string, fields ...string) error {
	if len(fields) == 0 {
		return nil
	}

	err := c.rdb.HDel(ctx, key, fields...).Err()
	if err != nil {
		c.logger.Error("Redis HDEL failed",
			zap.String("key", key),
			zap.Strings("fields", fields),
			zap.Error(err))
		return fmt.Errorf("redis hdel failed: %w", err)
	}
	return nil
}

func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	result, err := c.rdb.Exists(ctx, key).Result()
	if err != nil {
		c.logger.Error("Redis EXISTS failed",
			zap.String("key", key),
			zap.Error(err))
		return false, fmt.Errorf("redis exists failed: %w", err)
	}
	return result > 0, nil
}

func (c *Client) HGet(ctx context.Context, key, field string) (string, error) {
	result, err := c.rdb.HGet(ctx, key, field).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		c.logger.Error("Redis HGET failed",
			zap.String("key", key),
			zap.String("field", field),
			zap.Error(err))
		return "", fmt.Errorf("redis hget failed: %w", err)
	}
	return result, nil
}

func (c *Client) HSet(ctx context.Context, key string, values ...interface{}) error {
	err := c.rdb.HSet(ctx, key, values...).Err()
	if err != nil {
		c.logger.Error("Redis HSET failed",
			zap.String("key", key),
			zap.Error(err))
		return fmt.Errorf("redis hset failed: %w", err)
	}
	return nil
}

func (c *Client) HSetWithExpire(ctx context.Context, key string, expiration time.Duration, values ...interface{}) error {
	pipe := c.rdb.Pipeline()
	pipe.HSet(ctx, key, values...)
	pipe.Expire(ctx, key, expiration)

	_, err := pipe.Exec(ctx)
	if err != nil {
		c.logger.Error("Redis HSET+EXPIRE pipeline failed",
			zap.String("key", key),
			zap.Duration("expiration", expiration),
			zap.Error(err))
		return fmt.Errorf("redis hset with expire failed: %w", err)
	}
	return nil
}

func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	result, err := c.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		c.logger.Error("Redis HGETALL failed",
			zap.String("key", key),
			zap.Error(err))
		return nil, fmt.Errorf("redis hgetall failed: %w", err)
	}
	return result, nil
}

func (c *Client) Keys(ctx context.Context, pattern string) ([]string, error) {
	result, err := c.rdb.Keys(ctx, pattern).Result()
	if err != nil {
		c.logger.Error("Redis KEYS failed",
			zap.String("pattern", pattern),
			zap.Error(err))
		return nil, fmt.Errorf("redis keys failed: %w", err)
	}
	return result, nil
}

func (c *Client) Expire(ctx context.Context, key string, expiration time.Duration) error {
	err := c.rdb.Expire(ctx, key, expiration).Err()
	if err != nil {
		c.logger.Error("Redis EXPIRE failed",
			zap.String("key", key),
			zap.Duration("expiration", expiration),
			zap.Error(err))
		return fmt.Errorf("redis expire failed: %w", err)
	}
	return nil
}

func (c *Client) TTL(ctx context.Context, key string) (time.Duration, error) {
	result, err := c.rdb.TTL(ctx, key).Result()
	if err != nil {
		c.logger.Error("Redis TTL failed",
			zap.String("key", key),
			zap.Error(err))
		return 0, fmt.Errorf("redis ttl failed: %w", err)
	}
	return result, nil
}

func (c *Client) Close() error {
	if c.rdb != nil {
		err := c.rdb.Close()
		if err != nil {
			c.logger.Error("Failed to close Redis client", zap.Error(err))
			return err
		}
		c.logger.Debug("Redis client closed")
	}
	return nil
}

func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	result, err := c.rdb.Eval(ctx, script, keys, args...).Result()
	if err != nil {
		c.logger.Error("Redis EVAL failed",
			zap.Int("num_keys", len(keys)),
			zap.Int("num_args", len(args)),
			zap.Error(err))
		return nil, fmt.Errorf("redis eval failed: %w", err)
	}
	return result, nil
}

// ZPopMin removes and returns up to count members with lowest scores from sorted set
func (c *Client) ZPopMin(ctx context.Context, key string, count int64) ([]redis.Z, error) {
	result, err := c.rdb.ZPopMin(ctx, key, count).Result()
	if err != nil {
		c.logger.Error("Redis ZPOPMIN failed",
			zap.String("key", key),
			zap.Int64("count", count),
			zap.Error(err))
		return nil, fmt.Errorf("redis zpopmin failed: %w", err)
	}
	return result, nil
}

// ZCard returns the number of members in a sorted set
func (c *Client) ZCard(ctx context.Context, key string) (int64, error) {
	result, err := c.rdb.ZCard(ctx, key).Result()
	if err != nil {
		c.logger.Error("Redis ZCARD failed",
			zap.String("key", key),
			zap.Error(err))
		return 0, fmt.Errorf("redis zcard failed: %w", err)
	}
	return result, nil
}

// ZCount returns count of members with scores between min and max
func (c *Client) ZCount(ctx context.Context, key string, min, max string) (int64, error) {
	result, err := c.rdb.ZCount(ctx, key, min, max).Result()
	if err != nil {
		c.logger.Error("Redis ZCOUNT failed",
			zap.String("key", key),
			zap.String("min", min),
			zap.String("max", max),
			zap.Error(err))
		return 0, fmt.Errorf("redis zcount failed: %w", err)
	}
	return result, nil
}

// ZAdd adds a member with score to a sorted set
func (c *Client) ZAdd(ctx context.Context, key string, score float64, member string) error {
	err := c.rdb.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
	if err != nil {
		c.logger.Error("Redis ZADD failed",
			zap.String("key", key),
			zap.Float64("score", score),
			zap.Error(err))
		return fmt.Errorf("redis zadd failed: %w", err)
	}
	return nil
}

func (c *Client) GetClient() *redis.Client {
	return c.rdb
}
