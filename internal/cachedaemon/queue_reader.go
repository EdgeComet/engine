package cachedaemon

import (
	"context"
	"encoding/json"
	"strconv"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
)

type QueueReader struct {
	redis         *redis.Client
	keyGenerator  *redis.KeyGenerator
	internalQueue *InternalQueue
	logger        *zap.Logger
}

func NewQueueReader(redisClient *redis.Client, keyGenerator *redis.KeyGenerator, internalQueue *InternalQueue, logger *zap.Logger) *QueueReader {
	return &QueueReader{
		redis:         redisClient,
		keyGenerator:  keyGenerator,
		internalQueue: internalQueue,
		logger:        logger,
	}
}

type QueueItem struct {
	URL         string `json:"url"`
	Dimension   string `json:"dimension"`
	Priority    string `json:"priority"`
	ScheduledAt int64  `json:"scheduled_at"`
}

type QueueListResponse struct {
	Items   []QueueItem `json:"items"`
	Cursor  string      `json:"cursor"`
	HasMore bool        `json:"has_more"`
}

type QueueSummaryResponse struct {
	Pending    int `json:"pending"`
	Processing int `json:"processing"`
}

type QueueListParams struct {
	HostID         int
	Cursor         string
	Limit          int
	PriorityFilter []string
}

func (qr *QueueReader) ListQueueItems(params QueueListParams, dimensions map[string]types.Dimension) (*QueueListResponse, error) {
	priorities := params.PriorityFilter
	if len(priorities) == 0 {
		priorities = []string{redis.PriorityHigh, redis.PriorityNormal, redis.PriorityAutorecache}
	}

	dimIDToName := make(map[int]string)
	for name, dim := range dimensions {
		dimIDToName[dim.ID] = name
	}

	var allItems []QueueItem
	for _, priority := range priorities {
		key := qr.keyGenerator.RecacheQueueKey(params.HostID, priority)
		members, err := qr.redis.ZRangeWithScores(context.Background(), key, 0, -1)
		if err != nil {
			return nil, err
		}

		for _, z := range members {
			memberStr, ok := z.Member.(string)
			if !ok {
				continue
			}

			var member types.RecacheMember
			if err := json.Unmarshal([]byte(memberStr), &member); err != nil {
				qr.logger.Warn("Malformed queue member, skipping",
					zap.String("member", memberStr),
					zap.Error(err))
				continue
			}

			dimName := "unknown"
			if name, exists := dimIDToName[member.DimensionID]; exists {
				dimName = name
			}

			allItems = append(allItems, QueueItem{
				URL:         member.URL,
				Dimension:   dimName,
				Priority:    priority,
				ScheduledAt: int64(z.Score),
			})
		}
	}

	offset := 0
	if params.Cursor != "" && params.Cursor != "0" {
		var err error
		offset, err = strconv.Atoi(params.Cursor)
		if err != nil {
			offset = 0
		}
	}

	if offset > len(allItems) {
		offset = len(allItems)
	}
	end := offset + params.Limit
	if end > len(allItems) {
		end = len(allItems)
	}

	items := allItems[offset:end]
	if items == nil {
		items = []QueueItem{}
	}
	hasMore := end < len(allItems)

	nextCursor := "0"
	if hasMore {
		nextCursor = strconv.Itoa(end)
	}

	return &QueueListResponse{
		Items:   items,
		Cursor:  nextCursor,
		HasMore: hasMore,
	}, nil
}

func (qr *QueueReader) GetQueueSummary(hostID int) (*QueueSummaryResponse, error) {
	ctx := context.Background()

	highKey := qr.keyGenerator.RecacheQueueKey(hostID, redis.PriorityHigh)
	normalKey := qr.keyGenerator.RecacheQueueKey(hostID, redis.PriorityNormal)
	autoKey := qr.keyGenerator.RecacheQueueKey(hostID, redis.PriorityAutorecache)

	highCount, err := qr.redis.ZCard(ctx, highKey)
	if err != nil {
		return nil, err
	}
	normalCount, err := qr.redis.ZCard(ctx, normalKey)
	if err != nil {
		return nil, err
	}
	autoCount, err := qr.redis.ZCard(ctx, autoKey)
	if err != nil {
		return nil, err
	}

	pending := int(highCount + normalCount + autoCount)
	processing := qr.internalQueue.CountByHostID(hostID)

	return &QueueSummaryResponse{
		Pending:    pending,
		Processing: processing,
	}, nil
}
