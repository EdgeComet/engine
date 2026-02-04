package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// HAR storage constants
const (
	harKeyPrefix  = "debug:har"
	defaultHARTTL = 5 * time.Minute
)

// StoreHAR stores compressed HAR data in Redis
func (c *Client) StoreHAR(ctx context.Context, hostID, requestID string, data []byte, ttl time.Duration) error {
	if ttl == 0 {
		ttl = defaultHARTTL
	}

	key := harKey(hostID, requestID)
	return c.rdb.Set(ctx, key, data, ttl).Err()
}

// GetHAR retrieves compressed HAR data from Redis
func (c *Client) GetHAR(ctx context.Context, hostID, requestID string) ([]byte, error) {
	key := harKey(hostID, requestID)
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// DeleteHAR removes HAR data from Redis
func (c *Client) DeleteHAR(ctx context.Context, hostID, requestID string) error {
	key := harKey(hostID, requestID)
	return c.rdb.Del(ctx, key).Err()
}

// harKey generates the Redis key for HAR storage
func harKey(hostID, requestID string) string {
	return fmt.Sprintf("%s:%s:%s", harKeyPrefix, hostID, requestID)
}
