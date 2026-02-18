package cachedaemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
)

const (
	defaultLimit = 25
	maxLimit     = 100
)

const luaCacheList = `
local prefix = "meta:cache:" .. ARGV[1] .. ":"
local stale_ttl = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local cursor = ARGV[4]
local limit = tonumber(ARGV[5])
local status_filter = ARGV[6]
local dimension_filter = ARGV[7]
local url_contains = ARGV[8]
local url_contains_lower = string.lower(url_contains)
local size_min = tonumber(ARGV[9])
local size_max = tonumber(ARGV[10])
local cache_age_min = tonumber(ARGV[11])
local cache_age_max = tonumber(ARGV[12])
local status_code_filter = ARGV[13]
local source_filter = ARGV[14]
local index_status_filter = ARGV[15]

local max_scan_iterations = 200
local scan_iterations = 0
local results = {}

repeat
    local res = redis.call("SCAN", cursor, "MATCH", prefix .. "*", "COUNT", 500)
    cursor = res[1]

    for _, key in ipairs(res[2]) do
        local data = redis.call("HGETALL", key)
        local hash = {}
        for i = 1, #data, 2 do
            hash[data[i]] = data[i+1]
        end

        local expires_at = tonumber(hash["expires_at"] or "0")
        local status
        if now < expires_at then
            status = "active"
        elseif now < expires_at + stale_ttl then
            status = "stale"
        else
            status = "expired"
        end

        local pass = true

        if pass and status_filter ~= "" then
            if not string.find("," .. status_filter .. ",", "," .. status .. ",", 1, true) then
                pass = false
            end
        end

        if pass and dimension_filter ~= "" then
            local dim = hash["dimension"] or ""
            if not string.find("," .. dimension_filter .. ",", "," .. dim .. ",", 1, true) then
                pass = false
            end
        end

        if pass and url_contains ~= "" then
            if not string.find(string.lower(hash["url"] or ""), url_contains_lower, 1, true) then
                pass = false
            end
        end

        if pass then
            local size = tonumber(hash["size"] or "0")
            if size_min > 0 and size < size_min then pass = false end
            if pass and size_max > 0 and size > size_max then pass = false end
        end

        if pass then
            local created = tonumber(hash["created_at"] or "0")
            local age = now - created
            if cache_age_min > 0 and age < cache_age_min then pass = false end
            if pass and cache_age_max > 0 and age > cache_age_max then pass = false end
        end

        if pass and status_code_filter ~= "" then
            local sc = hash["status_code"] or ""
            if not string.find("," .. status_code_filter .. ",", "," .. sc .. ",", 1, true) then
                pass = false
            end
        end

        if pass and source_filter ~= "" then
            if (hash["source"] or "") ~= source_filter then
                pass = false
            end
        end

        if pass and index_status_filter ~= "" then
            local is = hash["index_status"] or "0"
            if not string.find("," .. index_status_filter .. ",", "," .. is .. ",", 1, true) then
                pass = false
            end
        end

        if pass then
            hash["_status"] = status
            hash["_age"] = tostring(now - tonumber(hash["created_at"] or "0"))
            table.insert(results, cjson.encode(hash))
            if #results >= limit then break end
        end
    end

    if #results >= limit then break end
    scan_iterations = scan_iterations + 1
    if scan_iterations >= max_scan_iterations then break end
until cursor == "0"

local output = {cursor}
for _, r in ipairs(results) do
    table.insert(output, r)
end
return output
`

const luaCacheSummaryChunk = `
local prefix = "meta:cache:" .. ARGV[1] .. ":"
local stale_ttl = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local cursor = ARGV[4]

local max_keys = 50000
local keys_scanned = 0
local total, active, stale, expired = 0, 0, 0, 0
local total_size = 0
local dim_counts = {}
local source_counts = {}

repeat
    local res = redis.call("SCAN", cursor, "MATCH", prefix .. "*", "COUNT", 1000)
    cursor = res[1]

    for _, key in ipairs(res[2]) do
        local vals = redis.call("HMGET", key, "expires_at", "size", "dimension", "source")
        local expires_at = tonumber(vals[1] or "0")
        local size = tonumber(vals[2] or "0")
        local dim = vals[3] or "unknown"
        local src = vals[4] or "unknown"

        if now < expires_at then
            active = active + 1
        elseif now < expires_at + stale_ttl then
            stale = stale + 1
        else
            expired = expired + 1
        end

        total = total + 1
        total_size = total_size + size

        dim_counts[dim] = (dim_counts[dim] or 0) + 1
        source_counts[src] = (source_counts[src] or 0) + 1

        keys_scanned = keys_scanned + 1
    end

    if keys_scanned >= max_keys then break end
until cursor == "0"

return {cursor, total, active, stale, expired,
        tostring(total_size),
        cjson.encode(dim_counts),
        cjson.encode(source_counts)}
`

type CacheReader struct {
	redis        *redis.Client
	keyGenerator *redis.KeyGenerator
	logger       *zap.Logger
}

func NewCacheReader(redisClient *redis.Client, keyGenerator *redis.KeyGenerator, logger *zap.Logger) *CacheReader {
	return &CacheReader{
		redis:        redisClient,
		keyGenerator: keyGenerator,
		logger:       logger,
	}
}

type CacheURLItem struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Dimension   string `json:"dimension"`
	Status      string `json:"status"`
	CacheAge    int64  `json:"cache_age"`
	Size        int64  `json:"size"`
	DiskSize    int64  `json:"disk_size"`
	LastAccess  int64  `json:"last_access"`
	CacheKey    string `json:"cache_key"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at"`
	StatusCode  int    `json:"status_code"`
	Source      string `json:"source"`
	IndexStatus int    `json:"index_status"`
	LastBotHit  *int64 `json:"last_bot_hit,omitempty"`
}

type CacheURLsResponse struct {
	Items   []CacheURLItem `json:"items"`
	Cursor  string         `json:"cursor"`
	HasMore bool           `json:"has_more"`
}

type CacheSummaryResponse struct {
	TotalUrls    int            `json:"total_urls"`
	ActiveCount  int            `json:"active_count"`
	StaleCount   int            `json:"stale_count"`
	ExpiredCount int            `json:"expired_count"`
	TotalSize    int64          `json:"total_size"`
	ByDimension  map[string]int `json:"by_dimension"`
	BySource     map[string]int `json:"by_source"`
}

type CacheListParams struct {
	HostID            int
	Cursor            string
	Limit             int
	StatusFilter      string
	DimensionFilter   string
	URLContains       string
	SizeMin           int64
	SizeMax           int64
	CacheAgeMin       int64
	CacheAgeMax       int64
	StatusCodeFilter  string
	SourceFilter      string
	IndexStatusFilter string
	StaleTTL          int64
}

func (cr *CacheReader) ListURLs(params CacheListParams) (*CacheURLsResponse, error) {
	result, err := cr.redis.Eval(
		context.Background(),
		luaCacheList,
		[]string{},
		strconv.Itoa(params.HostID),
		strconv.FormatInt(params.StaleTTL, 10),
		strconv.FormatInt(time.Now().Unix(), 10),
		params.Cursor,
		strconv.Itoa(params.Limit),
		params.StatusFilter,
		params.DimensionFilter,
		params.URLContains,
		strconv.FormatInt(params.SizeMin, 10),
		strconv.FormatInt(params.SizeMax, 10),
		strconv.FormatInt(params.CacheAgeMin, 10),
		strconv.FormatInt(params.CacheAgeMax, 10),
		params.StatusCodeFilter,
		params.SourceFilter,
		params.IndexStatusFilter,
	)
	if err != nil {
		return nil, err
	}

	arr, ok := result.([]interface{})
	if !ok || len(arr) == 0 {
		return &CacheURLsResponse{Items: []CacheURLItem{}, Cursor: "0"}, nil
	}

	nextCursor := fmt.Sprintf("%v", arr[0])
	items := make([]CacheURLItem, 0, len(arr)-1)

	for i := 1; i < len(arr); i++ {
		jsonStr, ok := arr[i].(string)
		if !ok {
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
			cr.logger.Error("Failed to parse Lua cache list item", zap.Error(err))
			continue
		}

		item := CacheURLItem{
			URL:         stringFromMap(raw, "url"),
			Title:       stringFromMap(raw, "title"),
			Dimension:   stringFromMap(raw, "dimension"),
			Status:      stringFromMap(raw, "_status"),
			CacheAge:    int64FromMap(raw, "_age"),
			Size:        int64FromMap(raw, "size"),
			DiskSize:    int64FromMap(raw, "disk_size"),
			LastAccess:  int64FromMap(raw, "last_access"),
			CacheKey:    stringFromMap(raw, "key"),
			CreatedAt:   int64FromMap(raw, "created_at"),
			ExpiresAt:   int64FromMap(raw, "expires_at"),
			StatusCode:  int(int64FromMap(raw, "status_code")),
			Source:      stringFromMap(raw, "source"),
			IndexStatus: int(int64FromMap(raw, "index_status")),
		}

		if lbh := int64FromMap(raw, "last_bot_hit"); lbh > 0 {
			item.LastBotHit = &lbh
		}

		items = append(items, item)
	}

	return &CacheURLsResponse{
		Items:   items,
		Cursor:  nextCursor,
		HasMore: nextCursor != "0",
	}, nil
}

func (cr *CacheReader) GetSummary(hostID int, staleTTL int64) (*CacheSummaryResponse, error) {
	cursor := "0"
	now := strconv.FormatInt(time.Now().Unix(), 10)
	hostIDStr := strconv.Itoa(hostID)
	staleTTLStr := strconv.FormatInt(staleTTL, 10)

	resp := &CacheSummaryResponse{
		ByDimension: make(map[string]int),
		BySource:    make(map[string]int),
	}

	for {
		result, err := cr.redis.Eval(
			context.Background(),
			luaCacheSummaryChunk,
			[]string{},
			hostIDStr,
			staleTTLStr,
			now,
			cursor,
		)
		if err != nil {
			return nil, err
		}

		arr, ok := result.([]interface{})
		if !ok || len(arr) < 8 {
			cr.logger.Error("Unexpected Lua summary chunk result format")
			break
		}

		cursor = fmt.Sprintf("%v", arr[0])
		resp.TotalUrls += intFromLuaResult(arr[1])
		resp.ActiveCount += intFromLuaResult(arr[2])
		resp.StaleCount += intFromLuaResult(arr[3])
		resp.ExpiredCount += intFromLuaResult(arr[4])

		sizeStr := fmt.Sprintf("%v", arr[5])
		sizeVal, _ := strconv.ParseInt(sizeStr, 10, 64)
		resp.TotalSize += sizeVal

		dimJSON := fmt.Sprintf("%v", arr[6])
		var dimCounts map[string]int
		if err := json.Unmarshal([]byte(dimJSON), &dimCounts); err != nil {
			cr.logger.Warn("Failed to parse dimension counts from Lua result", zap.Error(err))
		} else {
			for k, v := range dimCounts {
				resp.ByDimension[k] += v
			}
		}

		srcJSON := fmt.Sprintf("%v", arr[7])
		var srcCounts map[string]int
		if err := json.Unmarshal([]byte(srcJSON), &srcCounts); err != nil {
			cr.logger.Warn("Failed to parse source counts from Lua result", zap.Error(err))
		} else {
			for k, v := range srcCounts {
				resp.BySource[k] += v
			}
		}

		if cursor == "0" {
			break
		}
	}

	return resp, nil
}

func intFromLuaResult(v interface{}) int {
	switch val := v.(type) {
	case int64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(val)
		return n
	default:
		s := fmt.Sprintf("%v", v)
		n, _ := strconv.Atoi(s)
		return n
	}
}

func stringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func int64FromMap(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	}
	return 0
}
