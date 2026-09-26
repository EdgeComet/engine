package cachedaemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/redis"
	pkgtypes "github.com/edgecomet/engine/pkg/types"
)

const (
	defaultLimit = 25
	maxLimit     = 100
)

// cacheListTimeBudget bounds the total time ListURLs spends looping bounded
// Lua chunks over a shard keyspace. Checked between Evals; each Eval itself
// stays bounded by max_scan_iterations. If shards outgrow what this budget
// can walk (~10M+ keys), the fix is a per-host index, not a bigger budget.
const cacheListTimeBudget = 2 * time.Second

// Bounds for one Eval of a script that walks the shard keyspace with SCAN
// (summary, invalidate-all). Redis serves no other client while a script runs,
// so every Eval must stay short however large the shard or the host is.
// Measured on a 5M-key production shard: ~0.6us per key SCAN passes over and
// ~4us per matched key read, so the iteration cap bounds a small host among
// large neighbors and the matched cap bounds a large host, each to ~10-15ms.
// Both are checked only between SCAN calls: SCAN has already advanced the
// cursor past every key of a returned batch.
const (
	scanChunkCount          = 1000
	scanChunkMaxIterations  = 20
	scanChunkMaxMatchedKeys = 2500
)

// cacheSummaryTimeBudget bounds a whole GetSummary walk, below the 10s timeout
// callers put on daemon requests with room for the last chunk and the response.
// It is checked between Evals, so a walk overruns it by at most one chunk. The
// walk costs the whole shard plus the host's own keys, so both a very large
// host and a very large shard can exhaust it. A summary has no partial form,
// so running out of budget is an error rather than a truncated count.
const cacheSummaryTimeBudget = 9 * time.Second

var errCacheSummaryBudgetExceeded = errors.New("cache summary exceeded time budget")

const luaCacheList = `
local prefix = "meta:cache:" .. ARGV[1] .. ":"
local stale_ttl = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local cursor = ARGV[4]
local stop_threshold = tonumber(ARGV[5])
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
local title_filter = ARGV[16]
local title_filter_lower = string.lower(title_filter)
local created_at_min = tonumber(ARGV[17])
local created_at_max = tonumber(ARGV[18])
local expires_at_min = tonumber(ARGV[19])
local expires_at_max = tonumber(ARGV[20])
local last_access_min = tonumber(ARGV[21])
local last_access_max = tonumber(ARGV[22])
local last_bot_hit_min = tonumber(ARGV[23])
local last_bot_hit_max = tonumber(ARGV[24])
local url_starts_with = string.lower(ARGV[25])
local url_ends_with = string.lower(ARGV[26])
local url_neq = string.lower(ARGV[27])
local url_not_contains = string.lower(ARGV[28])
local title_starts_with = string.lower(ARGV[29])
local title_ends_with = string.lower(ARGV[30])
local title_neq = string.lower(ARGV[31])
local title_not_contains = string.lower(ARGV[32])
local last_bot_hit_exists = ARGV[33]
local scan_count = tonumber(ARGV[34])

local max_scan_iterations = 200
local scan_iterations = 0
local results = {}

-- SCAN advances the cursor past every key in the returned batch, so we only
-- stop at a batch boundary: every fetched key is either emitted or filtered,
-- never silently skipped. The Go loop shrinks stop_threshold to the items
-- still missing from the page while scan_count stays pinned to the original
-- request limit, so a page missing only its last items keeps scanning
-- full-size batches instead of degrading to tiny Evals.
if scan_count < 1 then scan_count = 1 end

repeat
    local res = redis.call("SCAN", cursor, "MATCH", prefix .. "*", "COUNT", scan_count)
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

        if pass and title_filter ~= "" then
            if not string.find(string.lower(hash["title"] or ""), title_filter_lower, 1, true) then
                pass = false
            end
        end

        if pass then
            local created = tonumber(hash["created_at"] or "0")
            if created_at_min > 0 and created < created_at_min then pass = false end
            if pass and created_at_max > 0 and created > created_at_max then pass = false end
        end

        if pass then
            local exp = tonumber(hash["expires_at"] or "0")
            if expires_at_min > 0 and exp < expires_at_min then pass = false end
            if pass and expires_at_max > 0 and exp > expires_at_max then pass = false end
        end

        if pass then
            local la = tonumber(hash["last_access"] or "0")
            if last_access_min > 0 and la < last_access_min then pass = false end
            if pass and last_access_max > 0 and la > last_access_max then pass = false end
        end

        if pass then
            local lbh = tonumber(hash["last_bot_hit"] or "0")
            if last_bot_hit_min > 0 then
                if lbh == 0 then pass = false
                elseif lbh < last_bot_hit_min then pass = false end
            end
            if pass and last_bot_hit_max > 0 then
                if lbh == 0 then pass = false
                elseif lbh > last_bot_hit_max then pass = false end
            end
        end

        if pass then
            local lower_url = string.lower(hash["url"] or "")
            if url_starts_with ~= "" then
                if string.sub(lower_url, 1, #url_starts_with) ~= url_starts_with then
                    pass = false
                end
            end
            if pass and url_ends_with ~= "" then
                if string.sub(lower_url, -#url_ends_with) ~= url_ends_with then
                    pass = false
                end
            end
            if pass and url_neq ~= "" then
                if lower_url == url_neq then
                    pass = false
                end
            end
            if pass and url_not_contains ~= "" then
                if string.find(lower_url, url_not_contains, 1, true) then
                    pass = false
                end
            end
        end

        if pass then
            local lower_title = string.lower(hash["title"] or "")
            if title_starts_with ~= "" then
                if string.sub(lower_title, 1, #title_starts_with) ~= title_starts_with then
                    pass = false
                end
            end
            if pass and title_ends_with ~= "" then
                if string.sub(lower_title, -#title_ends_with) ~= title_ends_with then
                    pass = false
                end
            end
            if pass and title_neq ~= "" then
                if lower_title == title_neq then
                    pass = false
                end
            end
            if pass and title_not_contains ~= "" then
                if string.find(lower_title, title_not_contains, 1, true) then
                    pass = false
                end
            end
        end

        if pass and last_bot_hit_exists ~= "" then
            local lbh = tonumber(hash["last_bot_hit"] or "0")
            if last_bot_hit_exists == "true" then
                if lbh == 0 then pass = false end
            elseif last_bot_hit_exists == "false" then
                if lbh ~= 0 then pass = false end
            end
        end

        if pass then
            hash["_status"] = status
            hash["_age"] = tostring(now - tonumber(hash["created_at"] or "0"))
            table.insert(results, cjson.encode(hash))
        end
    end

    if #results >= stop_threshold then break end
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
local scan_count = tonumber(ARGV[5])
local max_scan_iterations = tonumber(ARGV[6])
local max_matched_keys = tonumber(ARGV[7])

local scan_iterations = 0
local total, active, stale, expired = 0, 0, 0, 0
local total_size = 0
local dim_counts = {}
local source_counts = {}

-- cjson implementations disagree on an empty table (Redis: {}, miniredis: []),
-- and most chunks of a small host match nothing.
local function encode_counts(t)
    if next(t) == nil then return "{}" end
    return cjson.encode(t)
end

repeat
    local res = redis.call("SCAN", cursor, "MATCH", prefix .. "*", "COUNT", scan_count)
    cursor = res[1]
    scan_iterations = scan_iterations + 1

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
    end

    if total >= max_matched_keys or scan_iterations >= max_scan_iterations then break end
until cursor == "0"

return {cursor, total, active, stale, expired,
        tostring(total_size),
        encode_counts(dim_counts),
        encode_counts(source_counts)}
`

type CacheReader struct {
	redis        *redis.Client
	keyGenerator *redis.KeyGenerator
	nowFunc      func() time.Time
	logger       *zap.Logger
}

func NewCacheReader(redisClient *redis.Client, keyGenerator *redis.KeyGenerator, logger *zap.Logger) *CacheReader {
	return &CacheReader{
		redis:        redisClient,
		keyGenerator: keyGenerator,
		nowFunc:      time.Now,
		logger:       logger,
	}
}

// CacheURLItem is an alias to the shared type to avoid drift between OSS and EE.
type CacheURLItem = pkgtypes.CacheURLItem

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
	Title             string
	CreatedAtMin      int64
	CreatedAtMax      int64
	ExpiresAtMin      int64
	ExpiresAtMax      int64
	LastAccessMin     int64
	LastAccessMax     int64
	LastBotHitMin     int64
	LastBotHitMax     int64
	URLStartsWith     string
	URLEndsWith       string
	URLNeq            string
	URLNotContains    string
	TitleStartsWith   string
	TitleEndsWith     string
	TitleNeq          string
	TitleNotContains  string
	LastBotHitExists  string
	StaleTTL          int64
}

// ListURLs walks the shard keyspace with bounded Lua chunks (same pattern as
// GetSummary) until the page fills, the cursor exhausts, or the time budget
// expires. A single bounded Eval is not enough: SCAN examines the whole shard
// and the host MATCH only post-filters, so a small host colocated with a large
// neighbor would return an empty first page.
func (cr *CacheReader) ListURLs(ctx context.Context, params CacheListParams) (*CacheURLsResponse, error) {
	start := cr.nowFunc()
	deadline := start.Add(cacheListTimeBudget)
	now := strconv.FormatInt(start.Unix(), 10)
	cursor := params.Cursor
	items := make([]CacheURLItem, 0, params.Limit)

	for {
		result, err := cr.redis.Eval(
			ctx,
			luaCacheList,
			[]string{},
			strconv.Itoa(params.HostID),
			strconv.FormatInt(params.StaleTTL, 10),
			now,
			cursor,
			strconv.Itoa(params.Limit-len(items)),
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
			params.Title,
			strconv.FormatInt(params.CreatedAtMin, 10),
			strconv.FormatInt(params.CreatedAtMax, 10),
			strconv.FormatInt(params.ExpiresAtMin, 10),
			strconv.FormatInt(params.ExpiresAtMax, 10),
			strconv.FormatInt(params.LastAccessMin, 10),
			strconv.FormatInt(params.LastAccessMax, 10),
			strconv.FormatInt(params.LastBotHitMin, 10),
			strconv.FormatInt(params.LastBotHitMax, 10),
			params.URLStartsWith,
			params.URLEndsWith,
			params.URLNeq,
			params.URLNotContains,
			params.TitleStartsWith,
			params.TitleEndsWith,
			params.TitleNeq,
			params.TitleNotContains,
			params.LastBotHitExists,
			strconv.Itoa(params.Limit),
		)
		if err != nil {
			return nil, err
		}

		arr, ok := result.([]interface{})
		if !ok || len(arr) == 0 {
			cr.logger.Error("Unexpected Lua cache list result format")
			break
		}

		cursor = fmt.Sprintf("%v", arr[0])
		items = cr.appendListItems(items, arr[1:])

		if cursor == "0" || len(items) >= params.Limit {
			break
		}
		if !cr.nowFunc().Before(deadline) {
			cr.logger.Warn("Cache list time budget expired, returning partial page",
				zap.Int("host_id", params.HostID),
				zap.Int("items_accumulated", len(items)),
				zap.Int("limit", params.Limit))
			break
		}
	}

	return &CacheURLsResponse{
		Items:   items,
		Cursor:  cursor,
		HasMore: cursor != "0",
	}, nil
}

func (cr *CacheReader) appendListItems(items []CacheURLItem, rawItems []interface{}) []CacheURLItem {
	for _, entry := range rawItems {
		jsonStr, ok := entry.(string)
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
	return items
}

// GetSummary aggregates a host's cache metadata over one full SCAN pass, split
// into bounded Evals. Every chunk classifies against the same "now". Any chunk
// failure fails the whole summary: totals from part of the pass are wrong
// numbers, not a smaller page.
func (cr *CacheReader) GetSummary(ctx context.Context, hostID int, staleTTL int64) (*CacheSummaryResponse, error) {
	start := cr.nowFunc()
	deadline := start.Add(cacheSummaryTimeBudget)
	cursor := "0"
	now := strconv.FormatInt(start.Unix(), 10)
	hostIDStr := strconv.Itoa(hostID)
	staleTTLStr := strconv.FormatInt(staleTTL, 10)

	resp := &CacheSummaryResponse{
		ByDimension: make(map[string]int),
		BySource:    make(map[string]int),
	}

	for {
		result, err := cr.redis.Eval(
			ctx,
			luaCacheSummaryChunk,
			[]string{},
			hostIDStr,
			staleTTLStr,
			now,
			cursor,
			strconv.Itoa(scanChunkCount),
			strconv.Itoa(scanChunkMaxIterations),
			strconv.Itoa(scanChunkMaxMatchedKeys),
		)
		if err != nil {
			return nil, err
		}

		arr, ok := result.([]interface{})
		if !ok || len(arr) < 8 {
			return nil, fmt.Errorf("unexpected cache summary chunk result: %T", result)
		}

		cursor = fmt.Sprintf("%v", arr[0])
		counts := [4]*int{&resp.TotalUrls, &resp.ActiveCount, &resp.StaleCount, &resp.ExpiredCount}
		for i, dst := range counts {
			n, err := intFromLuaResult(arr[i+1])
			if err != nil {
				return nil, fmt.Errorf("failed to parse count from cache summary chunk: %w", err)
			}
			*dst += n
		}

		sizeVal, err := strconv.ParseInt(fmt.Sprintf("%v", arr[5]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse size from cache summary chunk: %w", err)
		}
		resp.TotalSize += sizeVal

		dimJSON := fmt.Sprintf("%v", arr[6])
		var dimCounts map[string]int
		if err := json.Unmarshal([]byte(dimJSON), &dimCounts); err != nil {
			return nil, fmt.Errorf("failed to parse dimension counts from cache summary chunk: %w", err)
		}
		for k, v := range dimCounts {
			resp.ByDimension[k] += v
		}

		srcJSON := fmt.Sprintf("%v", arr[7])
		var srcCounts map[string]int
		if err := json.Unmarshal([]byte(srcJSON), &srcCounts); err != nil {
			return nil, fmt.Errorf("failed to parse source counts from cache summary chunk: %w", err)
		}
		for k, v := range srcCounts {
			resp.BySource[k] += v
		}

		if cursor == "0" {
			break
		}
		if !cr.nowFunc().Before(deadline) {
			cr.logger.Warn("Cache summary time budget expired",
				zap.Int("host_id", hostID),
				zap.Int("urls_counted", resp.TotalUrls),
				zap.Float64("budget_seconds", cacheSummaryTimeBudget.Seconds()))
			return nil, errCacheSummaryBudgetExceeded
		}
	}

	return resp, nil
}

func intFromLuaResult(v interface{}) (int, error) {
	switch val := v.(type) {
	case int64:
		return int(val), nil
	case string:
		return strconv.Atoi(val)
	default:
		return 0, fmt.Errorf("unexpected Lua integer type %T", v)
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
