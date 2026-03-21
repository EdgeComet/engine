package recache_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/pkg/types"
)

// Helper function to add entry to recache ZSET
func addToRecacheZSET(client *redis.Client, hostID int, priority string, url string, dimensionID int, score float64) error {
	ctx := context.Background()
	zsetKey := fmt.Sprintf("recache:%d:%s", hostID, priority)

	member := types.RecacheMember{
		URL:         url,
		DimensionID: dimensionID,
	}
	memberJSON, err := json.Marshal(member)
	if err != nil {
		return err
	}

	return client.ZAdd(ctx, zsetKey, &redis.Z{
		Score:  score,
		Member: string(memberJSON),
	}).Err()
}

// Helper function to simulate bot hit autorecache scheduling
func simulateBotHit(client *redis.Client, hostID int, url string, dimensionID int, scheduledAt time.Time) error {
	ctx := context.Background()

	// Build ZSET member
	member := types.RecacheMember{
		URL:         url,
		DimensionID: dimensionID,
	}
	memberJSON, err := json.Marshal(member)
	if err != nil {
		return err
	}

	score := float64(scheduledAt.Unix())
	zsetKey := fmt.Sprintf("recache:%d:autorecache", hostID)

	// Use Lua script for conditional ZADD (matching production behavior)
	luaScript := `
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

	_, err = client.Eval(ctx, luaScript, []string{zsetKey}, string(memberJSON), score).Result()
	return err
}

// Helper to set cache metadata with last_bot_hit
func setCacheMetadataWithBotHit(client *redis.Client, cacheKey string, lastBotHit time.Time) error {
	ctx := context.Background()
	metaKey := "meta:" + cacheKey

	metadata := map[string]interface{}{
		"url":          "https://example.com/test",
		"created_at":   time.Now().Unix(),
		"expires_at":   time.Now().Add(1 * time.Hour).Unix(),
		"last_bot_hit": lastBotHit.Unix(),
		"status_code":  200,
		"source":       "render",
	}

	return client.HSet(ctx, metaKey, metadata).Err()
}

// Helper to get all ZSET entries with scores
func getZSETEntries(client *redis.Client, key string) ([]redis.Z, error) {
	ctx := context.Background()
	return client.ZRangeWithScores(ctx, key, 0, -1).Result()
}

// Helper to count ZSET entries
func getZSETCount(client *redis.Client, key string) (int64, error) {
	ctx := context.Background()
	return client.ZCard(ctx, key).Result()
}

// Helper to parse RecacheMember from ZSET entry
func parseRecacheMember(memberStr string) (*types.RecacheMember, error) {
	var member types.RecacheMember
	if err := json.Unmarshal([]byte(memberStr), &member); err != nil {
		return nil, err
	}
	return &member, nil
}

// populateCacheEntry creates a cache metadata hash in miniredis
func populateCacheEntry(mr *miniredis.Miniredis, hostID, dimID int, urlHash string, fields map[string]string) {
	key := fmt.Sprintf("meta:cache:%d:%d:%s", hostID, dimID, urlHash)
	for k, v := range fields {
		mr.HSet(key, k, v)
	}
}

// makeDaemonGETRequest sends an HTTP GET request to the daemon and returns parsed JSON
func makeDaemonGETRequest(baseURL, path, authKey string) (*http.Response, map[string]interface{}) {
	req, err := http.NewRequest("GET", baseURL+path, nil)
	Expect(err).NotTo(HaveOccurred())
	if authKey != "" {
		req.Header.Set("X-Internal-Auth", authKey)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	Expect(err).NotTo(HaveOccurred())

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	Expect(err).NotTo(HaveOccurred())

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	Expect(err).NotTo(HaveOccurred())

	return resp, result
}
