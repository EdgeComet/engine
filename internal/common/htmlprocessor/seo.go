package htmlprocessor

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/pkg/types"
	"golang.org/x/net/html"
)

// truncateRunes truncates a string to maxLen runes (not bytes).
// Returns the original string if it's already within the limit.
func truncateRunes(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen])
}

// collapseWhitespace trims leading/trailing whitespace and collapses
// internal whitespace sequences to single spaces.
func collapseWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// topNDomains returns the top N domains by count from a map.
// Ties are broken alphabetically by domain name.
func topNDomains(counts map[string]int, n int) map[string]int {
	if len(counts) <= n {
		return counts
	}

	type domainCount struct {
		domain string
		count  int
	}
	pairs := make([]domainCount, 0, len(counts))
	for domain, count := range counts {
		pairs = append(pairs, domainCount{domain, count})
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].domain < pairs[j].domain
	})

	result := make(map[string]int, n)
	for i := 0; i < n && i < len(pairs); i++ {
		result[pairs[i].domain] = pairs[i].count
	}
	return result
}

// extractSEOTitle extracts page title with SEO-specific 500-char limit.
// Does NOT reuse Title() which has 200-char limit.
func extractSEOTitle(head *html.Node) string {
	if head == nil {
		return ""
	}
	title := findElementInParent(head, "title")
	if title == nil {
		return ""
	}
	text := strings.TrimSpace(getTextContent(title))
	return truncateRunes(text, types.MaxSEOTitleLength)
}

// extractMetaDescription extracts the meta description content.
// Searches only in <head>, returns first match, max 1000 chars.
func extractMetaDescription(head *html.Node) string {
	if head == nil {
		return ""
	}
	metas := findAllElementsInParent(head, "meta")
	for _, meta := range metas {
		name := strings.ToLower(getAttr(meta, "name"))
		if name == "description" {
			content := strings.TrimSpace(getAttr(meta, "content"))
			if content == "" {
				return ""
			}
			return truncateRunes(content, types.MaxMetaDescriptionLength)
		}
	}
	return ""
}

// extractMetaRobots extracts the robots directive string.
// Priority: If any googlebot tag has non-empty content, use googlebot; otherwise use robots.
func extractMetaRobots(head *html.Node) string {
	if head == nil {
		return ""
	}
	metas := findAllElementsInParent(head, "meta")

	var googlebotContent string
	var robotsContent string

	for _, meta := range metas {
		name := strings.ToLower(getAttr(meta, "name"))
		content := strings.TrimSpace(getAttr(meta, "content"))

		switch name {
		case "googlebot":
			if content != "" && googlebotContent == "" {
				googlebotContent = content
			}
		case "robots":
			if content != "" && robotsContent == "" {
				robotsContent = content
			}
		}
	}

	// Googlebot takes precedence if it has content
	if googlebotContent != "" {
		return googlebotContent
	}
	return robotsContent
}

// extractBaseHref extracts the base href URL for relative URL resolution.
func extractBaseHref(head *html.Node) string {
	if head == nil {
		return ""
	}
	base := findElementInParent(head, "base")
	if base == nil {
		return ""
	}
	return strings.TrimSpace(getAttr(base, "href"))
}

// extractHeadings extracts heading text content from body.
// Returns first maxCount non-empty headings, with whitespace collapsed and text truncated.
func extractHeadings(body *html.Node, tag string, maxCount int) []string {
	if body == nil {
		return nil
	}

	elements := findAllElementsInParent(body, tag)
	if len(elements) == 0 {
		return nil
	}

	var results []string
	for _, elem := range elements {
		if len(results) >= maxCount {
			break
		}

		text := collapseWhitespace(getTextContent(elem))
		if text == "" {
			continue
		}

		text = truncateRunes(text, types.MaxHeadingLength)
		results = append(results, text)
	}

	if len(results) == 0 {
		return nil
	}
	return results
}

// shouldSkipLink returns true if the href should be excluded from link metrics.
func shouldSkipLink(href string) bool {
	if href == "" {
		return true
	}
	href = strings.TrimSpace(href)
	if href == "" {
		return true
	}
	// Fragment-only links
	if strings.HasPrefix(href, "#") {
		return true
	}
	hrefLower := strings.ToLower(href)
	// Protocol exclusions
	if strings.HasPrefix(hrefLower, "javascript:") ||
		strings.HasPrefix(hrefLower, "mailto:") ||
		strings.HasPrefix(hrefLower, "tel:") {
		return true
	}
	return false
}

// shouldSkipImageSrc returns true if the src should be excluded from image metrics.
func shouldSkipImageSrc(src string) bool {
	if src == "" {
		return true
	}
	src = strings.TrimSpace(src)
	if src == "" {
		return true
	}
	srcLower := strings.ToLower(src)
	if strings.HasPrefix(srcLower, "data:") ||
		strings.HasPrefix(srcLower, "blob:") {
		return true
	}
	return false
}

// resolveURL resolves a URL against base, with fallback to original.
func resolveURL(href, baseURL string) string {
	resolved := resolveCanonicalURL(href, baseURL)
	if resolved == "" {
		return href
	}
	return resolved
}

// extractLinkMetrics populates link metrics in the PageSEO struct.
// Extracts from body only, handles base tag, classifies internal/external.
func extractLinkMetrics(body *html.Node, baseHref, pageURL string, seo *types.PageSEO) {
	if body == nil || seo == nil {
		return
	}

	// Determine effective base URL for resolution
	effectiveBase := pageURL
	if baseHref != "" {
		// Resolve base href against page URL first
		effectiveBase = resolveURL(baseHref, pageURL)
	}

	// Parse page URL to get origin for comparison
	pageOrigin := ""
	if parsed, err := url.Parse(pageURL); err == nil {
		pageOrigin = parsed.Host
	}

	links := findAllElementsInParent(body, "a")
	externalDomains := make(map[string]int)

	for _, link := range links {
		href := getAttr(link, "href")
		if shouldSkipLink(href) {
			continue
		}

		seo.LinksTotal++

		// Resolve the URL
		resolved := resolveURL(href, effectiveBase)
		parsed, err := url.Parse(resolved)
		if err != nil {
			// Can't parse, count as external
			seo.LinksExternal++
			continue
		}

		linkHost := parsed.Host
		if linkHost == "" {
			// Relative URL without host, treat as internal
			seo.LinksInternal++
			continue
		}

		if urlutil.IsSameOrigin(pageOrigin, linkHost) {
			seo.LinksInternal++
		} else {
			seo.LinksExternal++
			hostname := urlutil.ExtractHostname(linkHost)
			if hostname != "" {
				externalDomains[hostname]++
			}
		}
	}

	// Limit external domains to top N
	if len(externalDomains) > 0 {
		seo.ExternalDomains = topNDomains(externalDomains, types.MaxExternalDomains)
	}
}

// extractImageMetrics populates image metrics in the PageSEO struct.
func extractImageMetrics(body *html.Node, baseHref, pageURL string, seo *types.PageSEO) {
	if body == nil || seo == nil {
		return
	}

	effectiveBase := pageURL
	if baseHref != "" {
		effectiveBase = resolveURL(baseHref, pageURL)
	}

	pageOrigin := ""
	if parsed, err := url.Parse(pageURL); err == nil {
		pageOrigin = parsed.Host
	}

	images := findAllElementsInParent(body, "img")

	for _, img := range images {
		src := getAttr(img, "src")
		if shouldSkipImageSrc(src) {
			continue
		}

		seo.ImagesTotal++

		resolved := resolveURL(src, effectiveBase)
		parsed, err := url.Parse(resolved)
		if err != nil {
			seo.ImagesExternal++
			continue
		}

		imgHost := parsed.Host
		if imgHost == "" {
			seo.ImagesInternal++
			continue
		}

		if urlutil.IsSameOrigin(pageOrigin, imgHost) {
			seo.ImagesInternal++
		} else {
			seo.ImagesExternal++
		}
	}
}

// extractHreflang extracts hreflang alternate links from head.
func extractHreflang(head *html.Node, pageURL string) []types.HreflangEntry {
	if head == nil {
		return nil
	}

	links := findAllElementsInParent(head, "link")
	var entries []types.HreflangEntry

	for _, link := range links {
		rel := strings.ToLower(getAttr(link, "rel"))
		if rel != "alternate" {
			continue
		}

		hreflang := strings.TrimSpace(getAttr(link, "hreflang"))
		if hreflang == "" {
			continue
		}

		href := strings.TrimSpace(getAttr(link, "href"))
		if href == "" {
			continue
		}

		// Resolve URL and truncate
		resolved := resolveCanonicalURL(href, pageURL)
		if resolved == "" {
			resolved = href
		}
		resolved = truncateRunes(resolved, types.MaxHreflangURLLength)

		entries = append(entries, types.HreflangEntry{
			Lang: hreflang,
			URL:  resolved,
		})
	}

	if len(entries) == 0 {
		return nil
	}
	return entries
}

// extractStructuredDataTypes extracts @type values from JSON-LD scripts.
func extractStructuredDataTypes(root *html.Node) []string {
	if root == nil {
		return nil
	}

	scripts := findAllElementsInParent(root, "script")
	typeSet := make(map[string]struct{})

	for _, script := range scripts {
		scriptType := strings.ToLower(strings.TrimSpace(getAttr(script, "type")))
		if scriptType != "application/ld+json" {
			continue
		}

		content := getTextContent(script)
		if len(content) > types.MaxJSONLDSize {
			// Skip oversized JSON-LD blocks
			continue
		}

		extractTypesFromJSON([]byte(content), typeSet, 0)
	}

	if len(typeSet) == 0 {
		return nil
	}

	// Convert set to sorted slice for deterministic output
	result := make([]string, 0, len(typeSet))
	for t := range typeSet {
		result = append(result, t)
	}
	sort.Strings(result)
	return result
}

// extractTypesFromJSON recursively extracts @type values from JSON.
func extractTypesFromJSON(data []byte, typeSet map[string]struct{}, depth int) {
	if depth > types.MaxJSONLDRecursionDepth {
		return
	}

	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return
	}

	extractTypesFromValue(obj, typeSet, depth)
}

// extractTypesFromValue recursively processes JSON values for @type.
func extractTypesFromValue(v interface{}, typeSet map[string]struct{}, depth int) {
	if depth > types.MaxJSONLDRecursionDepth {
		return
	}

	switch val := v.(type) {
	case map[string]interface{}:
		// Check for @type in this object
		if typeVal, ok := val["@type"]; ok {
			addType(typeVal, typeSet)
		}
		// Check for @graph array
		if graphVal, ok := val["@graph"]; ok {
			extractTypesFromValue(graphVal, typeSet, depth+1)
		}
		// Recurse into all values
		for _, child := range val {
			extractTypesFromValue(child, typeSet, depth+1)
		}
	case []interface{}:
		for _, item := range val {
			extractTypesFromValue(item, typeSet, depth+1)
		}
	}
}

// addType adds @type value(s) to the set. Handles both string and array types.
func addType(v interface{}, typeSet map[string]struct{}) {
	switch val := v.(type) {
	case string:
		if val != "" {
			typeSet[val] = struct{}{}
		}
	case []interface{}:
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				typeSet[s] = struct{}{}
			}
		}
	}
}

// ExtractPageSEO extracts comprehensive SEO metadata from the document.
func (d *domDocument) ExtractPageSEO(statusCode int, pageURL string) *types.PageSEO {
	seo := &types.PageSEO{}

	head := findElement(d.root, "head")
	body := findElement(d.root, "body")

	// Basic metadata
	seo.Title = extractSEOTitle(head)
	seo.IndexStatus = d.IndexationStatus(statusCode, pageURL)

	// Meta tags
	seo.MetaDescription = extractMetaDescription(head)
	seo.MetaRobots = extractMetaRobots(head)

	// Canonical URL - reuse existing extraction and resolution
	canonicalRaw := extractCanonicalURL(head)
	if canonicalRaw != "" {
		resolved := resolveCanonicalURL(canonicalRaw, pageURL)
		seo.CanonicalURL = truncateRunes(resolved, types.MaxCanonicalURLLength)
	}

	// Headings
	seo.H1s = extractHeadings(body, "h1", types.MaxHeadingsPerLevel)
	seo.H2s = extractHeadings(body, "h2", types.MaxHeadingsPerLevel)
	seo.H3s = extractHeadings(body, "h3", types.MaxHeadingsPerLevel)

	// Get base href for URL resolution
	baseHref := extractBaseHref(head)

	// Links and images
	extractLinkMetrics(body, baseHref, pageURL, seo)
	extractImageMetrics(body, baseHref, pageURL, seo)

	// International SEO
	seo.Hreflang = extractHreflang(head, pageURL)

	// Structured data
	seo.StructuredDataTypes = extractStructuredDataTypes(d.root)

	return seo
}
