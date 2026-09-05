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

// ErrRecacheSkipped marks a recache the configuration declines by design: bypass caching is off
// for the URL, the resolved cache TTL is zero (bypass or render - the live path caches in neither
// case), or a render cache entry already owns the key. The outcome is terminal - a retry resolves
// to the same decision - so the handler answers 200 and logs at info.
// Reporting it as a failure made the daemon retry to MaxRetries, and every attempt raised an
// error on both services, which is what drowned Rollbar.
// Origin failures are NOT skips: they are classified via recacheError so they stay countable.
var ErrRecacheSkipped = errors.New("recache skipped")

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
	preRenderHook    orchestrator.PreRenderHook
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
func (rs *RecacheService) ProcessRecache(ctx context.Context, url string, hostID, dimensionID int, mode string) (err error) {
	attempt := &precacheAttempt{url: url, hostID: hostID, dimensionID: dimensionID, startTime: time.Now()}

	// Emission sits on the way out rather than at every failure return: each terminal failure
	// leaves through here, and the attempt carries whatever the flow resolved before it failed.
	defer func() {
		if failure := classifiedFailure(err); failure != nil {
			rs.emitPrecacheFailure(attempt, failure)
		}
	}()

	// Get host config
	host := rs.getHostByID(hostID)
	if host == nil {
		// Retryable: a cluster move reaches the EGs asynchronously, so a host unknown now can
		// become known inside the daemon's retry window.
		return retryableFailure(types.ErrorTypeInvalidRequest, noOriginStatus,
			fmt.Sprintf("host not found: %d", hostID))
	}
	attempt.host = host

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
		return permanentFailure(types.ErrorTypeInvalidRequest, noOriginStatus,
			fmt.Sprintf("dimension %d not found for host %d", dimensionID, hostID))
	}
	attempt.dimension = dimensionName

	// SSRF protection: validate URL hostname
	parsedURL, err := neturl.Parse(url)
	if err != nil {
		return permanentFailure(types.ErrorTypeInvalidRequest, noOriginStatus,
			fmt.Sprintf("failed to parse recache URL: %v", err)).withCause(err)
	}
	if err := urlutil.ValidateHostNotPrivateIP(parsedURL.Hostname()); err != nil {
		return permanentFailure(types.ErrorTypeInvalidRequest, noOriginStatus,
			fmt.Sprintf("SSRF protection: %v", err)).withCause(err)
	}

	// Verify URL hostname matches one of the host's configured domains
	urlHostname := strings.ToLower(parsedURL.Hostname())
	if !hostHasDomain(host, urlHostname) {
		return permanentFailure(types.ErrorTypeInvalidRequest, noOriginStatus,
			fmt.Sprintf("URL hostname %q does not match any configured domain for host %d", urlHostname, hostID))
	}

	// Generate request ID and build render context early. Route through the
	// shared helper so the ID carries a random prefix (crypto/rand) and stays
	// unique across concurrent renders for the same host+dimension; a plain
	// host-dimension-second format collides during bulk drains.
	requestID := requestid.GenerateRequestID(fmt.Sprintf("recache-%d-%d", hostID, dimensionID))
	renderCtx, err := rs.buildRecacheContext(url, host, dimensionID, dimensionName, requestID)
	if err != nil {
		return permanentFailure(types.ErrorTypeInvalidRequest, noOriginStatus, err.Error()).withCause(err)
	}
	attempt.renderCtx = renderCtx

	// Debug, not Info: the handler already logged this request at Info before dispatch. This line
	// only adds the dimension name and the scoped fields, which is detail, not a second event.
	renderCtx.Logger.Debug("Recache context resolved",
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
		return permanentFailure(types.ErrorTypeInvalidRequest, noOriginStatus,
			fmt.Sprintf("mode:render unsupported for URL %q: render config unresolved (bypass set via url_rule)", url))
	}

	// Route to bypass recache if the effective action is bypass
	if renderCtx.ResolvedConfig.Action == types.ActionBypass {
		return rs.processBypassRecache(ctx, url, renderCtx, attempt.startTime)
	}

	// A render with no configured TTL has nothing to write: the live path gates its cache write
	// on Cache.TTL > 0 (render_orchestrator.go) and a URL matched by a status url_rule carries no
	// resolved cache section at all. Terminal by configuration, exactly like the bypass sibling.
	if renderCtx.ResolvedConfig.Cache.TTL == 0 {
		return fmt.Errorf("%w: render cache TTL is 0", ErrRecacheSkipped)
	}

	// Ahead of tab reservation so a short-circuit never occupies Chrome. Precache is where this
	// pays: a URL the origin cannot report a status for otherwise costs a full render, every
	// scheduled pass, to produce content that should not be cached at all.
	if decision := orchestrator.RunPreRenderHook(ctx, rs.preRenderHook, renderCtx); decision != nil {
		return rs.saveOverrideToCache(ctx, renderCtx, decision.AsProcessedContent(), attempt.startTime, overrideParams{
			cacheSource: cache.SourceRender,
			cacheTTL:    renderCtx.ResolvedConfig.Cache.TTL,
			expired:     renderCtx.ResolvedConfig.Cache.Expired,
			eventSource: orchestrator.ServedFromRender,
		})
	}

	// Select and reserve render service tab
	reservation, err := rs.selectServiceAndReserveTab(ctx, requestID, renderCtx.Logger)
	if err != nil || reservation == nil {
		message := "no render service available"
		if err != nil {
			message = fmt.Sprintf("%s: %v", message, err)
		}
		return retryableFailure(types.ErrorTypeRenderUnavailable, noOriginStatus, message).withCause(err)
	}

	// Release tab when done
	defer rs.releaseTabReservation(context.Background(), reservation, renderCtx.Logger)

	// Build render request using resolved config (includes merged Global -> Host -> Pattern settings)
	dimension := host.Dimensions[dimensionName]
	renderReq := orchestrator.BuildRenderRequest(url, requestID, host.RenderKey, reservation.TabID, &renderCtx.ResolvedConfig.Render, &dimension)
	renderReq.Headers = renderCtx.ClientHeaders

	// Build service URL
	serviceURL := fmt.Sprintf("http://%s:%d", reservation.Address, reservation.Port)

	// Call render service
	renderCtx.Logger.Info("Sending render request to service",
		zap.String("service_id", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.String("service_url", serviceURL))

	// Stamped before the call: a call that times out after Chrome already reached the origin
	// still records what was attempted. OriginResponseHeaders is cleared in the same step so a
	// response from an earlier attempt cannot outlive the request that produced it.
	renderCtx.OriginRequestHeaders = renderCtx.ResolvedConfig.RedactRequestHeaders(renderReq.InjectedHeaders())
	renderCtx.OriginResponseHeaders = nil

	renderResp, err := rs.rsClient.CallRenderService(ctx, serviceURL, renderReq)
	if err != nil {
		return classifyRenderCallError(err)
	}

	renderCtx.OriginResponseHeaders = renderResp.Headers

	if failure := orchestrator.ValidateRenderResponse(renderResp); failure != nil {
		return classifyRenderFailure(failure)
	}

	// Recache succeeds exactly where the live path caches (render_orchestrator.go: status in
	// Cache.StatusCodes), so a host configuring status_codes: [200, 404] can refresh its 404s.
	statusCode := renderResp.Metrics.StatusCode
	if failure := rs.classifyStatus(statusCode, renderCtx.ResolvedConfig.Cache.StatusCodes); failure != nil {
		return failure.withRedirect(renderResp.Metrics.FinalURL)
	}

	renderCtx.Logger.Info("Render completed",
		zap.Int("status_code", statusCode),
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
		renderCtx.Logger,
	)
	if processed.Override != nil {
		return rs.saveOverrideToCache(ctx, renderCtx, processed, attempt.startTime, overrideParams{
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

	totalDuration := time.Since(attempt.startTime)
	if err := rs.saveToCache(ctx, renderCtx, renderResult, processed.PageSEO, processed.RuleIDs, processed.OriginalPageSEO, processed.Extraction, reservation.ServiceID, totalDuration); err != nil {
		return err
	}

	renderCtx.Logger.Info("Recache completed successfully",
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
		return retryableFailure(types.ErrorTypeCacheWriteFailed, renderResult.StatusCode,
			fmt.Sprintf("failed to save cache: %v", err)).withCause(err)
	}

	// Clear last_bot_hit field (lifecycle completion)
	if err := rs.metadataStore.ClearLastBotHit(ctx, renderCtx.CacheKey); err != nil {
		renderCtx.Logger.Error("Failed to clear last_bot_hit",
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

	renderCtx.Logger.Info("Recache saved to cache successfully",
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
	startTime time.Time,
	params overrideParams,
) error {
	if err := rs.cacheCoord.SaveOverrideCache(
		renderCtx, processed.Override,
		params.cacheSource, params.cacheTTL, params.expired,
	); err != nil {
		return retryableFailure(types.ErrorTypeCacheWriteFailed, processed.Override.StatusCode,
			fmt.Sprintf("failed to cache override: %v", err)).withCause(err)
	}

	if err := rs.metadataStore.ClearLastBotHit(ctx, renderCtx.CacheKey); err != nil {
		renderCtx.Logger.Error("Failed to clear last_bot_hit",
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

	renderCtx.Logger.Info("Recache override cached",
		zap.Int("status_code", processed.Override.StatusCode),
		zap.String("location", processed.Override.Location))

	return nil
}

// processBypassRecache fetches content from origin via bypass and saves to bypass cache
func (rs *RecacheService) processBypassRecache(ctx context.Context, url string, renderCtx *edgectx.RenderContext, startTime time.Time) error {
	renderCtx.Logger.Info("Processing bypass recache request",
		zap.String("cache_key", renderCtx.CacheKey.String()))

	if !renderCtx.ResolvedConfig.Bypass.Cache.Enabled {
		return fmt.Errorf("%w: bypass cache disabled", ErrRecacheSkipped)
	}

	if renderCtx.ResolvedConfig.Bypass.Cache.TTL == 0 {
		return fmt.Errorf("%w: bypass cache TTL is 0", ErrRecacheSkipped)
	}

	bypassResp, err := rs.bypassSvc.FetchContent(url, renderCtx.ClientHeaders, renderCtx.Host.RenderKey, renderCtx.Logger)
	if err != nil {
		return retryableFailure(types.ErrorTypeNetworkError, noOriginStatus,
			fmt.Sprintf("bypass fetch failed: %v", err)).withCause(err)
	}

	// Ahead of the transport-error and status classifications below: a failure row is built from
	// this same context and carries the attempt's headers.
	renderCtx.OriginRequestHeaders = renderCtx.ResolvedConfig.RedactRequestHeaders(bypassResp.SentHeaders)
	renderCtx.OriginResponseHeaders = bypassResp.Headers

	// An unreachable origin comes back as a synthetic 502, not an error. Report the transport
	// failure with no status code - the 502 was never sent by the origin.
	if bypassResp.TransportError != "" {
		return retryableFailure(types.ErrorTypeNetworkError, noOriginStatus,
			fmt.Sprintf("origin unreachable: %s", bypassResp.TransportError))
	}

	renderCtx.Logger.Info("Bypass fetch completed",
		zap.Int("status_code", bypassResp.StatusCode),
		zap.Int("response_size", len(bypassResp.Body)))

	// Classification replaces CanSaveBypassCache here: its Enabled/TTL checks are already done
	// above, and folding an uncacheable origin status into a skip is what hid origin outages.
	if failure := rs.classifyStatus(bypassResp.StatusCode, renderCtx.ResolvedConfig.Bypass.Cache.StatusCodes); failure != nil {
		return failure.withRedirect(orchestrator.LocationHeaderValue(bypassResp.Headers))
	}

	if existing, exists := rs.cacheCoord.LookupCache(renderCtx); exists && existing.Source == cache.SourceRender {
		return fmt.Errorf("%w: render cache already exists", ErrRecacheSkipped)
	}

	// Only HTML responses are content-processed, mirroring the live bypass serve path
	// (render_orchestrator serveBypass): non-HTML bodies (PDF, JSON, images) must not be
	// parsed as HTML, run through SEO/EdgeSEO, or fed to extraction.
	var processed *orchestrator.ProcessedContent
	if orchestrator.IsHTMLContentTypeValue(bypassResp.ContentType) {
		processed = orchestrator.ProcessContent(
			ctx, bypassResp.Body, bypassResp.StatusCode, bypassResp.Headers, url,
			false, renderCtx.Host.ID, rs.contentProcessor, renderCtx.Logger,
		)
	}

	if processed != nil && processed.Override != nil {
		return rs.saveOverrideToCache(ctx, renderCtx, processed, startTime, overrideParams{
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
		return retryableFailure(types.ErrorTypeCacheWriteFailed, bypassResp.StatusCode,
			fmt.Sprintf("failed to save bypass cache: %v", err)).withCause(err)
	}

	// Clear last_bot_hit field (lifecycle completion)
	if err := rs.metadataStore.ClearLastBotHit(ctx, renderCtx.CacheKey); err != nil {
		renderCtx.Logger.Error("Failed to clear last_bot_hit",
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

	renderCtx.Logger.Info("Bypass recache completed successfully",
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
		// Scoped so every downstream line (fetch, content processing, cache write) is
		// attributable to a precache attempt instead of looking like live traffic.
		Logger: rs.logger.With(
			zap.String("request_id", requestID),
			zap.Int("host_id", host.ID),
			zap.String("url", url),
			zap.Bool("precache", true),
		),
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

	// Precache has no incoming request, so headers set in configuration are the only headers this
	// fetch can carry to the origin.
	renderCtx.ClientHeaders = renderCtx.ResolvedConfig.ApplyRequestHeaders(nil)
	if names := renderCtx.ResolvedConfig.RequestHeaderNames(); len(names) > 0 {
		// Names only: a configured value may be a credential.
		renderCtx.Logger.Debug("Applied configured request headers",
			zap.Strings("headers", names))
	}

	return renderCtx, nil
}

// buildRenderResult converts render response to RenderServiceResult.
// Field-for-field mirror of the live path (render_orchestrator.performActualRenderWithTab) so
// cached metadata and emitted events carry the same values whichever path produced them.
func (rs *RecacheService) buildRenderResult(renderResp *types.RenderResponse) *orchestrator.RenderServiceResult {
	return &orchestrator.RenderServiceResult{
		HTML:             []byte(renderResp.HTML),
		StatusCode:       renderResp.Metrics.StatusCode,
		RedirectLocation: renderResp.Metrics.FinalURL,
		RenderTime:       renderResp.RenderTime,
		ChromeID:         renderResp.ChromeID,
		Metrics:          renderResp.Metrics,
		Headers:          renderResp.Headers,
		ErrorType:        renderResp.ErrorType,
		ErrorMessage:     renderResp.Error,
	}
}

// selectServiceAndReserveTab atomically selects a healthy render service and reserves an available tab
func (rs *RecacheService) selectServiceAndReserveTab(ctx context.Context, requestID string, logger *zap.Logger) (*registry.TabReservation, error) {
	redisCtx, cancel := context.WithTimeout(context.Background(), redisTabOperationTimeout)
	defer cancel()

	// Get selection strategy from config (default applied in config.applyDefaults())
	strategy := rs.configManager.GetConfig().Registry.SelectionStrategy

	reservation, err := rs.tabSelector.SelectAndReserve(redisCtx, requestID, strategy)
	if err != nil {
		// Saturation and an empty registry are expected states, not faults
		if errors.Is(err, registry.ErrNoServices) || errors.Is(err, registry.ErrNoCapacity) {
			logger.Warn("Service selection failed", zap.Error(err))
		} else {
			logger.Error("Service selection failed", zap.Error(err))
		}
		return nil, err
	}

	logger.Debug("Selected service and reserved tab",
		zap.String("service_id", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID),
		zap.String("address", reservation.Address),
		zap.Int("port", reservation.Port))

	return reservation, nil
}

// releaseTabReservation clears the reserved tab in Redis
func (rs *RecacheService) releaseTabReservation(ctx context.Context, reservation *registry.TabReservation, logger *zap.Logger) {
	if reservation == nil {
		return
	}

	if err := rs.tabSelector.Release(ctx, reservation); err != nil {
		logger.Error("Failed to release tab reservation",
			zap.String("service_id", reservation.ServiceID),
			zap.Int("tab_id", reservation.TabID),
			zap.Error(err))
		return
	}

	logger.Debug("Released tab reservation",
		zap.String("service_id", reservation.ServiceID),
		zap.Int("tab_id", reservation.TabID))
}

func (rs *RecacheService) SetContentProcessor(cp orchestrator.ContentProcessor) {
	rs.contentProcessor = cp
}

// SetPreRenderHook sets an optional hook that can resolve a recache without rendering it.
//
// Wire this together with RenderOrchestrator.SetPreRenderHook or not at all: see the note there
// for what a half-wired pair does to a URL's cached status.
func (rs *RecacheService) SetPreRenderHook(h orchestrator.PreRenderHook) {
	rs.preRenderHook = h
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
