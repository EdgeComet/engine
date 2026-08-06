package htmlprocessor

import (
	"github.com/edgecomet/engine/pkg/types"
)

// ExtractPageSEO populates a PageSEO struct by running every per-field
// extractor against the parsed document. Each extractor lives in its own file:
// seo_text.go, seo_links.go, seo_hreflang.go, seo_jsonld.go, seo_breadcrumbs.go,
// seo_dates.go.
func (d *domDocument) ExtractPageSEO(statusCode int, pageURL string) *types.PageSEO {
	seo := &types.PageSEO{}

	// One source of base truth: <base href> resolved against the page URL, else the
	// page URL. Threaded to every extractor that RESOLVES a relative URL; identity
	// checks (IndexationStatus, HreflangSelf) keep the page URL.
	effectiveBase := effectiveBaseURL(d.doc, pageURL)

	seo.Title = extractSEOTitle(d.doc)
	seo.IndexStatus = d.IndexationStatus(statusCode, pageURL)
	seo.MetaDescription = extractMetaDescription(d.doc)
	seo.MetaRobots = extractMetaRobots(d.doc)

	canonicalRaw := extractCanonicalURL(d.doc)
	if canonicalRaw != "" {
		resolved := normalizeAbsoluteURL(resolveCanonicalURL(canonicalRaw, effectiveBase))
		seo.CanonicalURL = truncateRunes(resolved, types.MaxCanonicalURLLength)
	}

	seo.H1s = extractHeadings(d.doc, "h1", types.MaxHeadingsPerLevel)
	seo.H2s = extractHeadings(d.doc, "h2", types.MaxHeadingsPerLevel)
	seo.H3s = extractHeadings(d.doc, "h3", types.MaxHeadingsPerLevel)

	extractLinkMetrics(d.doc, effectiveBase, pageURL, seo)
	extractImageMetrics(d.doc, effectiveBase, pageURL, seo)

	words := extractBodyWords(d.doc)
	if len(words) > 0 {
		seo.WordCount = len(words)
		seo.PageMinHash = computePageMinHash(words)
	}

	seo.Hreflang = extractHreflang(d.doc, effectiveBase)
	seo.HreflangSelf = extractHreflangSelf(seo.Hreflang, pageURL)

	// One parse of the document's JSON-LD, shared by every consumer below.
	jsonLDBlocks := collectJSONLDBlocks(d.doc)
	seo.StructuredDataTypes = extractStructuredDataTypes(jsonLDBlocks)
	seo.Breadcrumbs = extractBreadcrumbs(jsonLDBlocks, effectiveBase)
	seo.Dates = extractDates(d.doc, jsonLDBlocks)

	return seo
}
