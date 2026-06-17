package htmlprocessor

import (
	"github.com/edgecomet/engine/internal/common/hash"
)

// seoURLNormalizer is the single canonical normalizer (the same one that produces
// events.url / events.url_hash). Stateless and safe to share.
var seoURLNormalizer = hash.NewURLNormalizer()

// normalizeAbsoluteURL applies the canonical URL normalizer to an already
// resolved-to-absolute URL, so the stored string matches the normalized form the
// target page's request URL receives and downstream keys line up. No per-host
// tracking-param strip patterns are applied here. On any normalization error it
// returns the input unchanged (best-effort).
func normalizeAbsoluteURL(resolved string) string {
	if resolved == "" {
		return ""
	}
	res, err := seoURLNormalizer.Normalize(resolved, nil)
	if err != nil {
		return resolved
	}
	return res.NormalizedURL
}
