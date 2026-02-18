package recache

import (
	"context"
	"fmt"
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/events"
	"github.com/edgecomet/engine/internal/edge/hash"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/internal/edge/rsclient"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	redisTabOperationTimeout   = 2 * time.Second
	redisCacheOperationTimeout = 5 * time.Second
)

// selectAndReserveScript atomically selects a healthy render service and reserves an available tab
const selectAndReserveScript = `
-- Atomically selects a healthy render service and reserves an available tab
-- ARGV[1] = request_id
-- ARGV[2] = strategy ("least_loaded", "most_available", or "round_robin")
-- ARGV[3] = reservation TTL (seconds, typically 2)

local request_id = ARGV[1]
local strategy = ARGV[2]
local reservation_ttl = tonumber(ARGV[3])

-- 1. Find all render services
local service_keys = redis.call('KEYS', 'service:render:*')
if #service_keys == 0 then
    return {false, 'no_services'}
end

-- 2. Filter healthy services and collect tab availability info
local candidates = {}
for _, service_key in ipairs(service_keys) do
    local service_data = redis.call('GET', service_key)
    if service_data then
        local service = cjson.decode(service_data)

        if service.status == 'healthy' and service.capacity and service.capacity > 0 then
            local service_id = service.id
            local tabs_key = 'tabs:' .. service_id

            if redis.call('EXISTS', tabs_key) == 1 then
                local tabs = redis.call('HGETALL', tabs_key)
                local available_count = 0
                local first_available = nil

                for i = 1, #tabs, 2 do
                    local tab_id = tonumber(tabs[i])
                    local tab_value = tabs[i + 1]

                    if tab_value == '' then
                        available_count = available_count + 1
                        if first_available == nil then
                            first_available = tab_id
                        end
                    end
                end

                if available_count > 0 then
                    local load = service.load or 0
                    local load_pct = load / service.capacity

                    table.insert(candidates, {
                        service_id = service_id,
                        address = service.address,
                        port = service.port,
                        available_count = available_count,
                        load_pct = load_pct,
                        first_available_tab = first_available
                    })
                end
            end
        end
    end
end

if #candidates == 0 then
    return {false, 'no_capacity'}
end

-- 3. Select best service based on strategy
local selected = candidates[1]
if strategy == 'least_loaded' then
    for _, candidate in ipairs(candidates) do
        if candidate.load_pct < selected.load_pct then
            selected = candidate
        end
    end
elseif strategy == 'most_available' then
    for _, candidate in ipairs(candidates) do
        if candidate.available_count > selected.available_count then
            selected = candidate
        end
    end
end

-- 4. Reserve the tab
local tabs_key = 'tabs:' .. selected.service_id
redis.call('HSET', tabs_key, tostring(selected.first_available_tab), request_id)
redis.call('EXPIRE', tabs_key, reservation_ttl)

return {selected.service_id, tostring(selected.first_available_tab), selected.address, tostring(selected.port)}
`

// TabReservation contains service and tab info
type TabReservation struct {
	ServiceID string
	TabID     int
	Address   string
	Port      int
}

// RecacheService handles background cache recaching operations
type RecacheService struct {
	configManager configtypes.EGConfigManager
	cacheCoord    *orchestrator.CacheCoordinator
	redis         *redis.Client
	rsClient      *rsclient.RSClient
	metadataStore *cache.MetadataStore
	eventEmitter  events.EventEmitter
	instanceID    string
	logger        *zap.Logger
}

// NewRecacheService creates a new RecacheService instance
func NewRecacheService(
	configManager configtypes.EGConfigManager,
	cacheCoord *orchestrator.CacheCoordinator,
	redisClient *redis.Client,
	rsClient *rsclient.RSClient,
	metadataStore *cache.MetadataStore,
	eventEmitter events.EventEmitter,
	instanceID string,
	logger *zap.Logger,
) *RecacheService {
	return &RecacheService{
		configManager: configManager,
		cacheCoord:    cacheCoord,
		redis:         redisClient,
		rsClient:      rsClient,
		metadataStore: metadataStore,
		eventEmitter:  eventEmitter,
		instanceID:    instanceID,
		logger:        logger,
	}
}

// ProcessRecache processes a recache request from the cache daemon
// Validates host and dimension, renders the URL, and saves to cache
func (rs *RecacheService) ProcessRecache(ctx context.Context, url string, hostID, dimensionID int) error {
	startTime := time.Now()

	// Get host config
	host := rs.getHostByID(hostID)
	if host == nil {
		return fmt.Errorf("host not found: %d", hostID)
	}

	// Validate dimension ID and get dimension name
	var dimensionName string
	dimensionFound := false
	for dimName, dim := range host.Render.Dimensions {
		if dim.ID == dimensionID {
			dimensionName = dimName
			dimensionFound = true
			break
		}
	}
	if !dimensionFound {
		return fmt.Errorf("dimension %d not found for host %d", dimensionID, hostID)
	}

	// SSRF protection: validate URL hostname
	parsedURL, err := neturl.Parse(url)
	if err != nil {
		return fmt.Errorf("failed to parse recache URL: %w", err)
	}
	if err := urlutil.ValidateHostNotPrivateIP(parsedURL.Hostname()); err != nil {
		return fmt.Errorf("SSRF protection: %w", err)
	}

	// Verify URL hostname matches one of the host's configured domains
	urlHostname := strings.ToLower(parsedURL.Hostname())
	if !hostHasDomain(host, urlHostname) {
		return fmt.Errorf("URL hostname %q does not match any configured domain for host %d", urlHostname, hostID)
	}

	// Generate request ID and build render context early
	requestID := fmt.Sprintf("recache-%d-%d-%d", hostID, dimensionID, time.Now().UTC().Unix())
	renderCtx, err := rs.buildRecacheContext(url, host, dimensionID, dimensionName, requestID)
	if err != nil {
		return err
	}

	rs.logger.Info("Processing recache request",
		zap.String("url", url),
		zap.Int("host_id", hostID),
		zap.Int("dimension_id", dimensionID),
		zap.String("dimension_name", dimensionName))

	// Select and reserve render service tab
	reservation, err := rs.selectServiceAndReserveTab(ctx, requestID)
	if err != nil || reservation == nil {
		return fmt.Errorf("no render services available: %w", err)
	}

	// Release tab when done
	defer rs.releaseTabReservation(context.Background(), reservation)

	// Build render request
	dimension := host.Render.Dimensions[dimensionName]

	// Merge blocked patterns: Global → Host-level → Host Render config
	globalCfg := rs.configManager.GetConfig()
	var blockedPatterns []string
	var blockedResourceTypes []string

	// Start with global config
	if len(globalCfg.Render.BlockedPatterns) > 0 {
		blockedPatterns = append(blockedPatterns, globalCfg.Render.BlockedPatterns...)
	}
	if len(globalCfg.Render.BlockedResourceTypes) > 0 {
		blockedResourceTypes = append(blockedResourceTypes, globalCfg.Render.BlockedResourceTypes...)
	}

	// Host render config overrides if specified
	if len(host.Render.BlockedPatterns) > 0 {
		blockedPatterns = host.Render.BlockedPatterns
	}
	if len(host.Render.BlockedResourceTypes) > 0 {
		blockedResourceTypes = host.Render.BlockedResourceTypes
	}

	renderReq := &types.RenderRequest{
		URL:                  url,
		Timeout:              time.Duration(host.Render.Timeout),
		TabID:                reservation.TabID,
		RequestID:            requestID,
		ViewportWidth:        dimension.Width,
		ViewportHeight:       dimension.Height,
		UserAgent:            dimension.RenderUA,
		BlockedPatterns:      blockedPatterns,
		BlockedResourceTypes: blockedResourceTypes,
	}

	// Build service URL
	serviceURL := fmt.Sprintf("http://%s:%d", reservation.Address, reservation.Port)

	// Call render service
	rs.logger.Info("Sending render request to service",
		zap.String("service_id", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.String("service_url", serviceURL))

	renderResp, err := rs.rsClient.CallRenderService(ctx, serviceURL, renderReq)
	if err != nil {
		return fmt.Errorf("render service failed: %w", err)
	}

	if renderResp.Metrics.StatusCode != 200 {
		return fmt.Errorf("page returned non-200 status: %d", renderResp.Metrics.StatusCode)
	}

	rs.logger.Info("Render completed successfully",
		zap.String("url", url),
		zap.Int("status_code", renderResp.Metrics.StatusCode),
		zap.Int("html_size", len(renderResp.HTML)))

	// Convert response to RenderServiceResult and save to cache
	renderResult := rs.buildRenderResult(renderResp)
	totalDuration := time.Since(startTime)
	if err := rs.saveToCache(ctx, renderCtx, renderResult, reservation.ServiceID, totalDuration); err != nil {
		return fmt.Errorf("failed to save to cache: %w", err)
	}

	rs.logger.Info("Recache completed successfully",
		zap.String("url", url),
		zap.Int("host_id", hostID),
		zap.Int("dimension_id", dimensionID))

	return nil
}

// saveToCache saves rendered content to cache without serving it
func (rs *RecacheService) saveToCache(
	ctx context.Context,
	renderCtx *edgectx.RenderContext,
	renderResult *orchestrator.RenderServiceResult,
	serviceID string,
	totalDuration time.Duration,
) error {
	// Save to cache using cache coordinator (handles sharding)
	if err := rs.cacheCoord.SaveRenderCache(renderCtx, renderResult); err != nil {
		return fmt.Errorf("failed to save cache: %w", err)
	}

	// Clear last_bot_hit field (lifecycle completion)
	if err := rs.metadataStore.ClearLastBotHit(ctx, renderCtx.CacheKey); err != nil {
		rs.logger.Error("Failed to clear last_bot_hit",
			zap.String("cache_key", renderCtx.CacheKey.String()),
			zap.Error(err))
		// Non-fatal error, continue
	}

	// Emit precache event for access logging
	if rs.eventEmitter != nil {
		result := &orchestrator.RenderResult{
			Source:      orchestrator.ServedFromRender,
			ServiceID:   serviceID,
			Duration:    totalDuration,
			BytesServed: int64(len(renderResult.HTML)),
			StatusCode:  renderResult.StatusCode,
			Metrics:     &renderResult.Metrics,
			RenderTime:  renderResult.RenderTime,
		}
		event := events.BuildRequestEvent(renderCtx, result, totalDuration, rs.instanceID)
		rs.eventEmitter.Emit(event)
	}

	rs.logger.Info("Recache saved to cache successfully",
		zap.String("url", renderCtx.TargetURL),
		zap.String("cache_key", renderCtx.CacheKey.String()),
		zap.Int("html_size", len(renderResult.HTML)))

	return nil
}

// getHostByID retrieves a host configuration by ID
func (rs *RecacheService) getHostByID(hostID int) *types.Host {
	hosts := rs.configManager.GetHosts()
	for i := range hosts {
		if hosts[i].ID == hostID {
			return &hosts[i]
		}
	}
	return nil
}

// buildRecacheContext creates RenderContext for a recache request
func (rs *RecacheService) buildRecacheContext(url string, host *types.Host, dimensionID int, dimensionName, requestID string) (*edgectx.RenderContext, error) {
	// Normalize URL and generate cache key
	normalizer := hash.NewURLNormalizer()
	normalizedResult, err := normalizer.Normalize(url, nil)
	if err != nil {
		return nil, fmt.Errorf("url normalization failed: %w", err)
	}

	urlHash := normalizer.Hash(normalizedResult.NormalizedURL)
	cacheKey := &types.CacheKey{
		HostID:      host.ID,
		DimensionID: dimensionID,
		URLHash:     urlHash,
	}

	renderCtx := &edgectx.RenderContext{
		TargetURL:  url,
		URLHash:    urlHash,
		Host:       host,
		Dimension:  dimensionName,
		CacheKey:   cacheKey,
		RequestID:  requestID,
		Logger:     rs.logger,
		IsPrecache: true,
	}

	// Resolve config for TTL and other cache settings
	egConfig := rs.configManager.GetConfig()
	resolver := config.NewConfigResolver(
		&egConfig.Render,
		&egConfig.Bypass,
		egConfig.TrackingParams,
		egConfig.CacheSharding,
		egConfig.BothitRecache,
		egConfig.Headers,
		egConfig.Storage.Compression,
		host,
	)
	renderCtx.ResolvedConfig = resolver.ResolveForURL(url)

	return renderCtx, nil
}

// buildRenderResult converts render response to RenderServiceResult
func (rs *RecacheService) buildRenderResult(renderResp *types.RenderResponse) *orchestrator.RenderServiceResult {
	return &orchestrator.RenderServiceResult{
		HTML:             []byte(renderResp.HTML),
		StatusCode:       renderResp.Metrics.StatusCode,
		RedirectLocation: "",
		RenderTime:       renderResp.RenderTime,
		ChromeID:         "recache",
		Metrics:          renderResp.Metrics,
	}
}

// selectServiceAndReserveTab atomically selects a healthy render service and reserves an available tab
func (rs *RecacheService) selectServiceAndReserveTab(ctx context.Context, requestID string) (*TabReservation, error) {
	redisCtx, cancel := context.WithTimeout(context.Background(), redisTabOperationTimeout)
	defer cancel()

	// Get selection strategy from config (default applied in config.applyDefaults())
	strategy := rs.configManager.GetConfig().Registry.SelectionStrategy

	// Execute Lua script to atomically select service and reserve tab
	result, err := rs.redis.Eval(
		redisCtx,
		selectAndReserveScript,
		[]string{},
		requestID,
		strategy, // selection strategy from config
		2,
	)

	if err != nil {
		rs.logger.Error("Lua script execution failed", zap.Error(err))
		return nil, fmt.Errorf("failed to execute service selection script: %w", err)
	}

	// Parse result
	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) < 2 {
		rs.logger.Error("Invalid script result format")
		return nil, fmt.Errorf("invalid script result")
	}

	// Check for error codes
	if resultSlice[0] == nil || resultSlice[0] == false {
		reason := "unknown"
		if len(resultSlice) > 1 {
			if r, ok := resultSlice[1].(string); ok {
				reason = r
			}
		}

		rs.logger.Warn("Service selection failed", zap.String("reason", reason))

		if reason == "no_services" {
			return nil, fmt.Errorf("no healthy services available")
		}
		if reason == "no_capacity" {
			return nil, fmt.Errorf("all services at capacity")
		}
		return nil, fmt.Errorf("service selection failed: %s", reason)
	}

	// Parse successful result
	if len(resultSlice) < 4 {
		rs.logger.Error("Incomplete result from script", zap.Int("length", len(resultSlice)))
		return nil, fmt.Errorf("incomplete script result")
	}

	serviceID, _ := resultSlice[0].(string)
	tabIDStr, _ := resultSlice[1].(string)
	address, _ := resultSlice[2].(string)
	portStr, _ := resultSlice[3].(string)

	tabID, err := strconv.Atoi(tabIDStr)
	if err != nil {
		rs.logger.Error("Invalid tab_id", zap.String("tab_id_str", tabIDStr), zap.Error(err))
		return nil, fmt.Errorf("invalid tab_id: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		rs.logger.Error("Invalid port", zap.String("port_str", portStr), zap.Error(err))
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	reservation := &TabReservation{
		ServiceID: serviceID,
		TabID:     tabID,
		Address:   address,
		Port:      port,
	}

	rs.logger.Debug("Selected service and reserved tab",
		zap.String("service_id", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.String("address", reservation.Address),
		zap.Int("port", reservation.Port))

	return reservation, nil
}

// releaseTabReservation clears the reserved tab in Redis
func (rs *RecacheService) releaseTabReservation(ctx context.Context, reservation *TabReservation) {
	if reservation == nil {
		return
	}

	tabsKey := fmt.Sprintf("tabs:%s", reservation.ServiceID)
	err := rs.redis.HSet(ctx, tabsKey, fmt.Sprintf("%d", reservation.TabID), "")
	if err != nil {
		rs.logger.Error("Failed to release tab reservation",
			zap.String("service_id", reservation.ServiceID),
			zap.Int("tab_id", reservation.TabID),
			zap.Error(err))
		return
	}

	rs.logger.Debug("Released tab reservation",
		zap.String("service_id", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID))
}

// hostHasDomain checks if the given hostname matches any of the host's configured domains
func hostHasDomain(host *types.Host, hostname string) bool {
	for _, domain := range host.Domains {
		if strings.ToLower(domain) == hostname {
			return true
		}
	}
	return false
}
