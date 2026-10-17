package orchestrator

import (
	"net/url"

	"github.com/edgecomet/engine/internal/common/hash"
	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/internal/edge/edgectx"
)

// ignoreCachedRedirect reports whether a cached entry must be treated as absent for this request:
// it is a redirect back to this request's own cache key, produced by a different spelling.
// Legacy requests never ignore an entry. An entry whose spelling equals the request's is a
// genuine loop and is served as cached.
func ignoreCachedRedirect(entry *cache.CacheMetadata, rc *edgectx.RenderContext) bool {
	if !rc.FetchExact || !isRedirectStatusCode(entry.StatusCode) {
		return false
	}
	location := LocationHeaderValue(entry.Headers)
	if location == "" || entry.EffectiveFetchURL() == rc.FetchURL {
		return false
	}
	return locationMapsToRequestKey(location, rc)
}

// locationMapsToRequestKey resolves location against the request's fetch URL and reports whether
// it normalizes to the request's url hash. The request's strip patterns stand in for the
// destination's own URL rules: a wrong answer costs one refetch or serves the entry as before.
func locationMapsToRequestKey(location string, rc *edgectx.RenderContext) bool {
	base, err := url.Parse(rc.FetchURL)
	if err != nil {
		return false
	}
	ref, err := url.Parse(location)
	if err != nil {
		return false
	}

	normalizer := hash.NewURLNormalizer()
	result, err := normalizer.Normalize(base.ResolveReference(ref).String(), rc.ResolvedConfig.TrackingParams.StripPatterns())
	if err != nil {
		return false
	}
	return normalizer.Hash(result.NormalizedURL) == rc.URLHash
}
