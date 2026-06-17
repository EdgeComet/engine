package htmlprocessor

import (
	"github.com/edgecomet/engine/pkg/types"
)

// ExtractPageSEO populates a PageSEO struct by running every per-field
// extractor against the parsed document. Each extractor lives in its own file:
// seo_text.go, seo_links.go, seo_hreflang.go, seo_jsonld.go, seo_breadcrumbs.go.
func (d *domDocument) ExtractPageSEO(statusCode int, pageURL string) *types.PageSEO {
	seo := &types.PageSEO{}

	seo.Title = extractSEOTitle(d.doc)
	seo.IndexStatus = d.IndexationStatus(statusCode, pageURL)
	seo.MetaDescription = extractMetaDescription(d.doc)
	seo.MetaRobots = extractMetaRobots(d.doc)

	canonicalRaw := extractCanonicalURL(d.doc)
	if canonicalRaw != "" {
		resolved := normalizeAbsoluteURL(resolveCanonicalURL(canonicalRaw, pageURL))
		seo.CanonicalURL = truncateRunes(resolved, types.MaxCanonicalURLLength)
	}

	seo.H1s = extractHeadings(d.doc, "h1", types.MaxHeadingsPerLevel)
	seo.H2s = extractHeadings(d.doc, "h2", types.MaxHeadingsPerLevel)
	seo.H3s = extractHeadings(d.doc, "h3", types.MaxHeadingsPerLevel)

	baseHref := extractBaseHref(d.doc)
	extractLinkMetrics(d.doc, baseHref, pageURL, seo)
	extractImageMetrics(d.doc, baseHref, pageURL, seo)

	words := extractBodyWords(d.doc)
	if len(words) > 0 {
		seo.WordCount = len(words)
	}

	seo.Hreflang = extractHreflang(d.doc, pageURL)
	seo.HreflangSelf = extractHreflangSelf(seo.Hreflang, pageURL)
	seo.StructuredDataTypes = extractStructuredDataTypes(d.doc)
	seo.Breadcrumbs = extractBreadcrumbs(d.doc, pageURL)

	return seo
}
