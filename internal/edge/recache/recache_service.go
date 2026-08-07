package recache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/configtypes"
	"github.com/edgecomet/engine/internal/common/hash"
	"github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/internal/common/requestid"
	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
	"github.com/edgecomet/engine/internal/edge/events"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/internal/edge/rsclient"
	"github.com/edgecomet/engine/internal/render/registry"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	redisTabOperationTimeout   = 2 * time.Second
	redisCacheOperationTimeout = 5 * time.Second
)

// RecacheService handles background cache recaching operations
type RecacheService struct {
	configManager    configtypes.EGConfigManager
	cacheCoord       *orchestrator.CacheCoordinator
	bypassSvc        *bypass.BypassService
	tabSelector      *registry.TabSelector
	rsClient         *rsclient.RSClient
	metadataStore    *cache.MetadataStore
	eventEmitter     events.EventEmitter
	contentProcessor orchestrator.ContentProcessor
	instanceID       string
	logger           *zap.Logger
}

// NewRecacheService creates a new RecacheService instance
func NewRecacheService(
	configManager configtypes.EGConfigManager,
	cacheCoord *orchestrator.CacheCoordinator,
	bypassSvc *bypass.BypassService,
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
		bypassSvc:     bypassSvc,
		tabSelector:   registry.NewTabSelector(redisClient, logger),
		rsClient:      rsClient,
		metadataStore: metadataStore,
		eventEmitter:  eventEmitter,
		instanceID:    instanceID,
		logger:        logger,
	}
}

// ProcessRecache processes a recache request from the cache daemon.
// Validates host and dimension, then renders or bypass-fetches the URL and saves
// to cache. mode optionally overrides the configured action: "render" forces a
// Chrome render (stored as render cache), "bypass" forces an origin fetch (stored
// as bypass cache), and "" respects the dimension/url-rule action.
func (rs *RecacheService) ProcessRecache(ctx context.Context, url string, hostID, dimensionID int, mode string) error {
	startTime := time.Now()

	// Get host config
	host := rs.getHostByID(hostID)
	if host == nil {
		return fmt.Errorf("host not found: %d", hostID)
	}

	// Validate dimension ID and get dimension name
	var dimensionName string
	dimensionFound := false
	for dimName, dim := range host.Dimensions {
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

	// Generate request ID and build render context early. Route through the
	// shared helper so the ID carries a random prefix (crypto/rand) and stays
	// unique across concurrent renders for the same host+dimension; a plain
	// host-dimension-second format collides during bulk drains.
	requestID := requestid.GenerateRequestID(fmt.Sprintf("recache-%d-%d", hostID, dimensionID))
	renderCtx, err := rs.buildRecacheContext(url, host, dimensionID, dimensionName, requestID)
	if err != nil {
		return err
	}

	rs.logger.Info("Processing recache request",
		zap.String("url", url),
		zap.Int("host_id", hostID),
		zap.Int("dimension_id", dimensionID),
		zap.String("dimension_name", dimensionName))

	// Apply dimension action override when no URL rule matched (same logic as server.go)
	dimConfig := host.Dimensions[dimensionName]
	if renderCtx.ResolvedConfig.MatchedRuleID == "" {
		dimAction := dimConfig.EffectiveAction()
		if dimAction != types.ActionRender {
			renderCtx.ResolvedConfig.Action = dimAction
		}
	}

	// Apply the per-request mode override as the final action decision (overrides the
	// dimension/url-rule action). Empty mode keeps the resolved action unchanged.
	switch mode {
	case types.RecacheModeRender:
		renderCtx.ResolvedConfig.Action = types.ActionRender
	case types.RecacheModeBypass:
		renderCtx.ResolvedConfig.Action = types.ActionBypass
	}

	// A forced render needs resolved render/cache config. ResolveForURL fills it only
	// when the URL's action is render (no bypass url_rule matched) - true for a
	// bypass-by-dimension host, where Render.Timeout is populated. Fail loudly on the
	// unsupported url_rule-bypass + mode:render combination rather than rendering with
	// a zero-valued config.
	if mode == types.RecacheModeRender && renderCtx.ResolvedConfig.Render.Timeout == 0 {
		return fmt.Errorf("mode:render unsupported for URL %q: render config unresolved (bypass set via url_rule)", url)
	}

	// Route to bypass recache if the effective action is bypass
	if renderCtx.ResolvedConfig.Action == types.ActionBypass {
		return rs.processBypassRecache(ctx, url, renderCtx, startTime)
	}

	// Select and reserve render service tab
	reservation, err := rs.selectServiceAndReserveTab(ctx, requestID)
	if err != nil || reservation == nil {
		return fmt.Errorf("no render services available: %w", err)
	}

	// Release tab when done
	defer rs.releaseTabReservation(context.Background(), reservation)

	// Build render request using resolved config (includes merged Global -> Host -> Pattern settings)
	dimension := host.Dimensions[dimensionName]
	renderReq := orchestrator.BuildRenderRequest(url, requestID, reservation.TabID, &renderCtx.ResolvedConfig.Render, &dimension)
	if host.RenderKey != "" {
		if renderReq.Headers == nil {
			renderReq.Headers = make(map[string][]string)
		}
		renderReq.Headers[types.HeaderRenderKey] = []string{host.RenderKey}
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

	stripScripts := renderCtx.ResolvedConfig.Render.StripScripts
	processed := orchestrator.ProcessContent(
		ctx,
		renderResult.HTML,
		renderResult.StatusCode,
		renderResult.Headers,
		url,
		stripScripts,
		renderCtx.Host.ID,
		rs.contentProcessor,
		rs.logger,
	)
	if processed.Override != nil {
		return rs.saveOverrideToCache(ctx, renderCtx, processed, url, startTime, overrideParams{
			cacheSource: cache.SourceRender,
			cacheTTL:    renderCtx.ResolvedConfig.Cache.TTL,
			expired:     renderCtx.ResolvedConfig.Cache.Expired,
			eventSource: orchestrator.ServedFromRender,
			serviceID:   reservation.ServiceID,
			metrics:     &renderResult.Metrics,
			renderTime:  renderResult.RenderTime,
		})
	}

	renderResult.HTML = processed.HTML

	totalDuration := time.Since(startTime)
	if err := rs.saveToCache(ctx, renderCtx, renderResult, processed.PageSEO, processed.RuleIDs, processed.OriginalPageSEO, processed.Extraction, reservation.ServiceID, totalDuration); err != nil {
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
	pageSEO *types.PageSEO,
	ruleIDs []uint32,
	originalPageSEO *types.PageSEO,
	extraction json.RawMessage,
	serviceID string,
	totalDuration time.Duration,
) error {
	// Save to cache using cache coordinator (handles sharding)
	if err := rs.cacheCoord.SaveRenderCache(renderCtx, renderResult, pageSEO); err != nil {
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
			Source:          orchestrator.ServedFromRender,
			ServiceID:       serviceID,
			Duration:        totalDuration,
			BytesServed:     int64(len(renderResult.HTML)),
			StatusCode:      renderResult.StatusCode,
			Metrics:         &renderResult.Metrics,
			RenderTime:      renderResult.RenderTime,
			PageSEO:         pageSEO,
			RuleIDs:         ruleIDs,
			OriginalPageSEO: originalPageSEO,
			Extraction:      extraction,
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

type overrideParams struct {
	cacheSource string
	cacheTTL    time.Duration
	expired     types.CacheExpiredConfig
	eventSource orchestrator.ResponseSource
	serviceID   string
	metrics     *types.PageMetrics
	renderTime  time.Duration
}

func (rs *RecacheService) saveOverrideToCache(
	ctx context.Context,
	renderCtx *edgectx.RenderContext,
	processed *orchestrator.ProcessedContent,
	url string,
	startTime time.Time,
	params overrideParams,
) error {
	if err := rs.cacheCoord.SaveOverrideCache(
		renderCtx, processed.Override,
		params.cacheSource, params.cacheTTL, params.expired,
	); err != nil {
		return fmt.Errorf("failed to cache override: %w", err)
	}

	if err := rs.metadataStore.ClearLastBotHit(ctx, renderCtx.CacheKey); err != nil {
		rs.logger.Error("Failed to clear last_bot_hit",
			zap.String("cache_key", renderCtx.CacheKey.String()),
			zap.Error(err))
	}

	totalDuration := time.Since(startTime)
	if rs.eventEmitter != nil {
		result := &orchestrator.RenderResult{
			Source:          params.eventSource,
			ServiceID:       params.serviceID,
			Duration:        totalDuration,
			BytesServed:     0,
			StatusCode:      processed.Override.StatusCode,
			Metrics:         params.metrics,
			RenderTime:      params.renderTime,
			RedirectTo:      processed.Override.Location,
			PageSEO:         processed.PageSEO,
			RuleIDs:         processed.RuleIDs,
			OriginalPageSEO: processed.OriginalPageSEO,
			Extraction:      processed.Extraction,
		}
		event := events.BuildRequestEvent(renderCtx, result, totalDuration, rs.instanceID)
		rs.eventEmitter.Emit(event)
	}

	rs.logger.Info("Recache override cached",
		zap.String("url", url),
		zap.Int("status_code", processed.Override.StatusCode),
		zap.String("location", processed.Override.Location))

	return nil
}

// processBypassRecache fetches content from origin via bypass and saves to bypass cache
func (rs *RecacheService) processBypassRecache(ctx context.Context, url string, renderCtx *edgectx.RenderContext, startTime time.Time) error {
	rs.logger.Info("Processing bypass recache request",
		zap.String("url", url),
		zap.String("cache_key", renderCtx.CacheKey.String()))

	if !renderCtx.ResolvedConfig.Bypass.Cache.Enabled {
		return fmt.Errorf("bypass cache disabled, skipping recache")
	}

	if renderCtx.ResolvedConfig.Bypass.Cache.TTL == 0 {
		return fmt.Errorf("bypass cache TTL is 0, skipping recache")
	}

	bypassResp, err := rs.bypassSvc.FetchContent(url, nil, renderCtx.Host.RenderKey, renderCtx.Logger)
	if err != nil {
		return fmt.Errorf("bypass fetch failed: %w", err)
	}

	rs.logger.Info("Bypass fetch completed successfully",
		zap.String("url", url),
		zap.Int("status_code", bypassResp.StatusCode),
		zap.Int("response_size", len(bypassResp.Body)))

	if canSave, reason := rs.cacheCoord.CanSaveBypassCache(renderCtx, bypassResp.StatusCode); !canSave {
		return fmt.Errorf("bypass cache save skipped: %s", reason)
	}

	// Only HTML responses are content-processed, mirroring the live bypass serve path
	// (render_orchestrator serveBypass): non-HTML bodies (PDF, JSON, images) must not be
	// parsed as HTML, run through SEO/EdgeSEO, or fed to extraction.
	var processed *orchestrator.ProcessedContent
	if orchestrator.IsHTMLContentTypeValue(bypassResp.ContentType) {
		processed = orchestrator.ProcessContent(
			ctx, bypassResp.Body, bypassResp.StatusCode, bypassResp.Headers, url,
			false, renderCtx.Host.ID, rs.contentProcessor, rs.logger,
		)
	}

	if processed != nil && processed.Override != nil {
		return rs.saveOverrideToCache(ctx, renderCtx, processed, url, startTime, overrideParams{
			cacheSource: cache.SourceBypass,
			cacheTTL:    renderCtx.ResolvedConfig.Bypass.Cache.TTL,
			expired:     renderCtx.ResolvedConfig.Bypass.Cache.Expired,
			eventSource: orchestrator.ServedFromBypass,
		})
	}

	var pageSEO *types.PageSEO
	if processed != nil {
		pageSEO = processed.PageSEO
		if processed.OriginalPageSEO != nil {
			bypassResp.Body = processed.HTML
		}
	}

	if err := rs.cacheCoord.SaveBypassCache(renderCtx, bypassResp, pageSEO); err != nil {
		return fmt.Errorf("failed to save bypass cache: %w", err)
	}

	// Clear last_bot_hit field (lifecycle completion)
	if err := rs.metadataStore.ClearLastBotHit(ctx, renderCtx.CacheKey); err != nil {
		rs.logger.Error("Failed to clear last_bot_hit",
			zap.String("cache_key", renderCtx.CacheKey.String()),
			zap.Error(err))
	}

	totalDuration := time.Since(startTime)

	// Emit recache event for access logging
	if rs.eventEmitter != nil {
		result := &orchestrator.RenderResult{
			Source:      orchestrator.ServedFromBypass,
			Duration:    totalDuration,
			BytesServed: int64(len(bypassResp.Body)),
			StatusCode:  bypassResp.StatusCode,
			PageSEO:     pageSEO,
		}
		if processed != nil {
			result.RuleIDs = processed.RuleIDs
			result.OriginalPageSEO = processed.OriginalPageSEO
			result.Extraction = processed.Extraction
		}
		event := events.BuildRequestEvent(renderCtx, result, totalDuration, rs.instanceID)
		rs.eventEmitter.Emit(event)
	}

	rs.logger.Info("Bypass recache completed successfully",
		zap.String("url", url),
		zap.String("cache_key", renderCtx.CacheKey.String()))

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
	// Normalize URL and generate cache key. Passing nil strip patterns is correct only
	// because the URL arriving here is already daemon-normalized (hostURLNormalizer strips
	// tracking params via the same resolver as the edge), so re-normalizing is idempotent
	// and yields the same hash the live request computes. If the recache entry source ever
	// changes to deliver raw URLs, this must resolve and pass the host's strip patterns.
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
		TargetURL:   url,
		OriginalURL: url,
		URLHash:     urlHash,
		Host:        host,
		Dimension:   dimensionName,
		CacheKey:    cacheKey,
		RequestID:   requestID,
		Logger:      rs.logger,
		IsPrecache:  true,
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
		Headers:          renderResp.Headers,
	}
}

// selectServiceAndReserveTab atomically selects a healthy render service and reserves an available tab
func (rs *RecacheService) selectServiceAndReserveTab(ctx context.Context, requestID string) (*registry.TabReservation, error) {
	redisCtx, cancel := context.WithTimeout(context.Background(), redisTabOperationTimeout)
	defer cancel()

	// Get selection strategy from config (default applied in config.applyDefaults())
	strategy := rs.configManager.GetConfig().Registry.SelectionStrategy

	reservation, err := rs.tabSelector.SelectAndReserve(redisCtx, requestID, strategy)
	if err != nil {
		// Saturation and an empty registry are expected states, not faults
		if errors.Is(err, registry.ErrNoServices) || errors.Is(err, registry.ErrNoCapacity) {
			rs.logger.Warn("Service selection failed", zap.Error(err))
		} else {
			rs.logger.Error("Service selection failed", zap.Error(err))
		}
		return nil, err
	}

	rs.logger.Debug("Selected service and reserved tab",
		zap.String("service_id", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.String("address", reservation.Address),
		zap.Int("port", reservation.Port))

	return reservation, nil
}

// releaseTabReservation clears the reserved tab in Redis
func (rs *RecacheService) releaseTabReservation(ctx context.Context, reservation *registry.TabReservation) {
	if reservation == nil {
		return
	}

	if err := rs.tabSelector.Release(ctx, reservation); err != nil {
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

func (rs *RecacheService) SetContentProcessor(cp orchestrator.ContentProcessor) {
	rs.contentProcessor = cp
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
