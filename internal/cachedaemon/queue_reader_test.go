package cachedaemon

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

func setupTestQueueReader(t *testing.T) (*QueueReader, *miniredis.Miniredis, *InternalQueue, *HostConcurrencyLimiter) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	logger := zap.NewNop()
	redisClient, err := redis.NewClient(&configtypes.RedisConfig{
		Addr: mr.Addr(),
	}, logger)
	require.NoError(t, err)

	keyGen := redis.NewKeyGenerator()
	iq := NewInternalQueue(100)
	limiter := NewHostConcurrencyLimiter(nil, nil)
	qr := NewQueueReader(redisClient, keyGen, iq, limiter, logger)
	return qr, mr, iq, limiter
}

func addQueueMember(mr *miniredis.Miniredis, hostID int, priority string, url string, dimID int, scheduledAt float64) {
	key := fmt.Sprintf("recache:%d:%s", hostID, priority)
	member, _ := json.Marshal(types.RecacheMember{URL: url, DimensionID: dimID})
	mr.ZAdd(key, scheduledAt, string(member))
}

func TestQueueReader_ListQueueItems(t *testing.T) {
	dimensions := map[string]types.Dimension{
		"mobile":  {ID: 1},
		"desktop": {ID: 2},
	}

	t.Run("no priority filter returns all queues", func(t *testing.T) {
		qr, mr, _, _ := setupTestQueueReader(t)

		addQueueMember(mr, 1, "high", "https://example.com/h1", 1, 1000)
		addQueueMember(mr, 1, "normal", "https://example.com/n1", 1, 2000)
		addQueueMember(mr, 1, "autorecache", "https://example.com/a1", 2, 3000)

		result, err := qr.ListQueueItems(QueueListParams{
			HostID: 1,
			Cursor: "0",
			Limit:  100,
		}, dimensions)
		require.NoError(t, err)
		assert.Len(t, result.Items, 3)
	})

	t.Run("priority filter high only", func(t *testing.T) {
		qr, mr, _, _ := setupTestQueueReader(t)

		addQueueMember(mr, 1, "high", "https://example.com/h1", 1, 1000)
		addQueueMember(mr, 1, "normal", "https://example.com/n1", 1, 2000)
		addQueueMember(mr, 1, "autorecache", "https://example.com/a1", 2, 3000)

		result, err := qr.ListQueueItems(QueueListParams{
			HostID:         1,
			Cursor:         "0",
			Limit:          100,
			PriorityFilter: []string{"high"},
		}, dimensions)
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "high", result.Items[0].Priority)
	})

	t.Run("cursor pagination", func(t *testing.T) {
		qr, mr, _, _ := setupTestQueueReader(t)

		for i := 0; i < 10; i++ {
			addQueueMember(mr, 1, "normal", fmt.Sprintf("https://example.com/page%d", i), 1, float64(1000+i))
		}

		// First page
		result1, err := qr.ListQueueItems(QueueListParams{
			HostID: 1,
			Cursor: "0",
			Limit:  4,
		}, dimensions)
		require.NoError(t, err)
		assert.Len(t, result1.Items, 4)
		assert.Equal(t, "4", result1.Cursor)
		assert.True(t, result1.HasMore)

		// Second page
		result2, err := qr.ListQueueItems(QueueListParams{
			HostID: 1,
			Cursor: result1.Cursor,
			Limit:  4,
		}, dimensions)
		require.NoError(t, err)
		assert.Len(t, result2.Items, 4)
		assert.Equal(t, "8", result2.Cursor)
		assert.True(t, result2.HasMore)

		// Third page (last 2)
		result3, err := qr.ListQueueItems(QueueListParams{
			HostID: 1,
			Cursor: result2.Cursor,
			Limit:  4,
		}, dimensions)
		require.NoError(t, err)
		assert.Len(t, result3.Items, 2)
		assert.Equal(t, "0", result3.Cursor)
		assert.False(t, result3.HasMore)
	})

	t.Run("dimension mapping", func(t *testing.T) {
		qr, mr, _, _ := setupTestQueueReader(t)

		addQueueMember(mr, 1, "normal", "https://example.com/mob", 1, 1000)
		addQueueMember(mr, 1, "normal", "https://example.com/desk", 2, 2000)

		result, err := qr.ListQueueItems(QueueListParams{
			HostID: 1,
			Cursor: "0",
			Limit:  100,
		}, dimensions)
		require.NoError(t, err)
		require.Len(t, result.Items, 2)

		dimNames := map[string]bool{}
		for _, item := range result.Items {
			dimNames[item.Dimension] = true
		}
		assert.True(t, dimNames["mobile"])
		assert.True(t, dimNames["desktop"])
	})

	t.Run("unknown dimension_id", func(t *testing.T) {
		qr, mr, _, _ := setupTestQueueReader(t)

		addQueueMember(mr, 1, "normal", "https://example.com/unknown", 99, 1000)

		result, err := qr.ListQueueItems(QueueListParams{
			HostID: 1,
			Cursor: "0",
			Limit:  100,
		}, dimensions)
		require.NoError(t, err)
		require.Len(t, result.Items, 1)
		assert.Equal(t, "unknown", result.Items[0].Dimension)
	})

	t.Run("malformed JSON member", func(t *testing.T) {
		qr, mr, _, _ := setupTestQueueReader(t)

		key := "recache:1:normal"
		mr.ZAdd(key, 1000, "not-valid-json")
		addQueueMember(mr, 1, "normal", "https://example.com/valid", 1, 2000)

		result, err := qr.ListQueueItems(QueueListParams{
			HostID: 1,
			Cursor: "0",
			Limit:  100,
		}, dimensions)
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "https://example.com/valid", result.Items[0].URL)
	})

	t.Run("empty queues", func(t *testing.T) {
		qr, _, _, _ := setupTestQueueReader(t)

		result, err := qr.ListQueueItems(QueueListParams{
			HostID: 1,
			Cursor: "0",
			Limit:  100,
		}, dimensions)
		require.NoError(t, err)
		assert.Empty(t, result.Items)
		assert.Equal(t, "0", result.Cursor)
		assert.False(t, result.HasMore)
	})
}

func TestQueueReader_GetQueueSummary(t *testing.T) {
	t.Run("sums all three queues for pending", func(t *testing.T) {
		qr, mr, _, _ := setupTestQueueReader(t)

		for i := 0; i < 3; i++ {
			addQueueMember(mr, 1, "high", fmt.Sprintf("https://example.com/h%d", i), 1, float64(1000+i))
		}
		for i := 0; i < 2; i++ {
			addQueueMember(mr, 1, "normal", fmt.Sprintf("https://example.com/n%d", i), 1, float64(2000+i))
		}
		for i := 0; i < 5; i++ {
			addQueueMember(mr, 1, "autorecache", fmt.Sprintf("https://example.com/a%d", i), 1, float64(3000+i))
		}

		result, err := qr.GetQueueSummary(1)
		require.NoError(t, err)
		assert.Equal(t, 10, result.Pending)
	})

	t.Run("processing counts in-flight renders from the limiter", func(t *testing.T) {
		qr, _, _, limiter := setupTestQueueReader(t)

		// Acquire 3 slots for host 1 and 1 for host 2. Cap is DefaultMaxConcurrent
		// (5) since the limiter was built with nil config, so all succeed.
		var host1Slots []Slot
		for i := 0; i < 3; i++ {
			slot, ok := limiter.TryAcquire(1)
			require.True(t, ok)
			host1Slots = append(host1Slots, slot)
		}
		otherSlot, ok := limiter.TryAcquire(2)
		require.True(t, ok)

		result, err := qr.GetQueueSummary(1)
		require.NoError(t, err)
		assert.Equal(t, 3, result.Processing, "processing must reflect host 1's in-flight renders only")

		// Releasing slots returns processing to 0.
		for _, slot := range host1Slots {
			limiter.Release(slot)
		}
		limiter.Release(otherSlot)

		result, err = qr.GetQueueSummary(1)
		require.NoError(t, err)
		assert.Equal(t, 0, result.Processing)
	})

	t.Run("pending includes internalQueue entries (waiting for a slot or retry)", func(t *testing.T) {
		qr, mr, iq, _ := setupTestQueueReader(t)

		// 2 durable in Redis + 3 staged in the internalQueue for host 1.
		addQueueMember(mr, 1, "high", "https://example.com/r1", 1, 1000)
		addQueueMember(mr, 1, "normal", "https://example.com/r2", 1, 2000)
		for i := 0; i < 3; i++ {
			iq.Enqueue(InternalQueueEntry{HostID: 1, URL: fmt.Sprintf("https://example.com/iq%d", i)})
		}
		// Another host's internalQueue entry must not leak into host 1's count.
		iq.Enqueue(InternalQueueEntry{HostID: 2, URL: "https://other.com/1"})

		result, err := qr.GetQueueSummary(1)
		require.NoError(t, err)
		assert.Equal(t, 5, result.Pending, "pending = Redis depth + internalQueue depth for the host")
		assert.Equal(t, 0, result.Processing, "internalQueue entries are not in-flight")
	})

	t.Run("pending is independent of in-flight", func(t *testing.T) {
		qr, mr, _, limiter := setupTestQueueReader(t)

		addQueueMember(mr, 1, "high", "https://example.com/p1", 1, 1000)
		slot, ok := limiter.TryAcquire(1)
		require.True(t, ok)
		defer limiter.Release(slot)

		result, err := qr.GetQueueSummary(1)
		require.NoError(t, err)
		assert.Equal(t, 1, result.Pending, "the durable entry stays pending while a render is in-flight")
		assert.Equal(t, 1, result.Processing)
	})

	t.Run("processing matches the limiter stats source (consistency guard)", func(t *testing.T) {
		qr, _, _, limiter := setupTestQueueReader(t)

		slot1, ok := limiter.TryAcquire(1)
		require.True(t, ok)
		defer limiter.Release(slot1)
		slot2, ok := limiter.TryAcquire(1)
		require.True(t, ok)
		defer limiter.Release(slot2)

		result, err := qr.GetQueueSummary(1)
		require.NoError(t, err)

		// /status reports in-flight via AllStats(); the summary endpoint must
		// report the identical number from the same limiter.
		assert.Equal(t, int(limiter.AllStats()[1].InFlight), result.Processing)
		assert.Equal(t, int(limiter.Stats(1).InFlight), result.Processing)
	})

	t.Run("empty queues", func(t *testing.T) {
		qr, _, _, _ := setupTestQueueReader(t)

		result, err := qr.GetQueueSummary(1)
		require.NoError(t, err)
		assert.Equal(t, 0, result.Pending)
		assert.Equal(t, 0, result.Processing)
	})
}
