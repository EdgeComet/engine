package htmlprocessor

import (
	"encoding/json"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/pkg/types"
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

func extractSEOTitle(doc *goquery.Document) string {
	text := strings.TrimSpace(doc.Find("head title").First().Text())
	return truncateRunes(text, types.MaxSEOTitleLength)
}

func extractMetaDescription(doc *goquery.Document) string {
	var result string
	doc.Find("head meta").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		name := strings.ToLower(getSelectionAttr(s, "name"))
		if name == "description" {
			content := strings.TrimSpace(getSelectionAttr(s, "content"))
			if content != "" {
				result = truncateRunes(content, types.MaxMetaDescriptionLength)
			}
			return false
		}
		return true
	})
	return result
}

func extractMetaRobots(doc *goquery.Document) []string {
	var googlebotContent string
	var robotsContent string

	doc.Find("head meta").Each(func(_ int, s *goquery.Selection) {
		name := strings.ToLower(getSelectionAttr(s, "name"))
		content := strings.TrimSpace(getSelectionAttr(s, "content"))

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
	})

	raw := googlebotContent
	if raw == "" {
		raw = robotsContent
	}
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	var directives []string
	for _, p := range parts {
		d := strings.ToLower(strings.TrimSpace(p))
		if d != "" {
			directives = append(directives, d)
		}
	}
	if len(directives) == 0 {
		return nil
	}
	return directives
}

func extractCanonicalURL(doc *goquery.Document) string {
	href, _ := doc.Find("head link[rel='canonical']").First().Attr("href")
	return strings.TrimSpace(href)
}

func extractBaseHref(doc *goquery.Document) string {
	href, _ := doc.Find("head base").First().Attr("href")
	return strings.TrimSpace(href)
}

func extractHeadings(doc *goquery.Document, tag string, maxCount int) []string {
	var results []string
	doc.Find("body " + tag).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if len(results) >= maxCount {
			return false
		}
		text := collapseWhitespace(s.Text())
		if text != "" {
			results = append(results, truncateRunes(text, types.MaxHeadingLength))
		}
		return true
	})
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
func extractLinkMetrics(doc *goquery.Document, baseHref, pageURL string, seo *types.PageSEO) {
	if seo == nil {
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

	externalDomains := make(map[string]int)

	doc.Find("body a").Each(func(_ int, s *goquery.Selection) {
		href := getSelectionAttr(s, "href")
		if shouldSkipLink(href) {
			return
		}

		seo.LinksTotal++

		rel := strings.ToLower(getSelectionAttr(s, "rel"))
		isNofollow := strings.Contains(rel, "nofollow")

		resolved := resolveURL(href, effectiveBase)
		parsed, err := url.Parse(resolved)
		if err != nil {
			seo.LinksExternal++
			if isNofollow {
				seo.LinksNofollow++
				seo.LinksNofollowExternal++
			}
			return
		}

		linkHost := parsed.Host
		isInternal := linkHost == "" || urlutil.IsSameOrigin(pageOrigin, linkHost)

		if isInternal {
			seo.LinksInternal++
		} else {
			seo.LinksExternal++
			hostname := urlutil.ExtractHostname(linkHost)
			if hostname != "" {
				externalDomains[hostname]++
			}
		}

		if isNofollow {
			seo.LinksNofollow++
			if isInternal {
				seo.LinksNofollowInternal++
			} else {
				seo.LinksNofollowExternal++
			}
		}
	})

	if len(externalDomains) > 0 {
		seo.ExternalDomains = topNDomains(externalDomains, types.MaxExternalDomains)
	}
}

// extractImageMetrics populates image metrics in the PageSEO struct.
func extractImageMetrics(doc *goquery.Document, baseHref, pageURL string, seo *types.PageSEO) {
	if seo == nil {
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

	doc.Find("body img").Each(func(_ int, s *goquery.Selection) {
		src := getSelectionAttr(s, "src")
		if shouldSkipImageSrc(src) {
			return
		}

		seo.ImagesTotal++

		alt := getSelectionAttr(s, "alt")
		if alt != "" {
			seo.ImagesWithAlt++
		} else {
			seo.ImagesWithoutAlt++
		}

		resolved := resolveURL(src, effectiveBase)
		parsed, err := url.Parse(resolved)
		if err != nil {
			seo.ImagesExternal++
			return
		}

		imgHost := parsed.Host
		if imgHost == "" {
			seo.ImagesInternal++
			return
		}

		if urlutil.IsSameOrigin(pageOrigin, imgHost) {
			seo.ImagesInternal++
		} else {
			seo.ImagesExternal++
		}
	})
}

func extractBodyWords(doc *goquery.Document) []string {
	body := doc.Find("body")
	if body.Length() == 0 {
		return nil
	}
	clone := body.Clone()
	clone.Find("nav, header, footer, aside, form, script, style, noscript").Remove()
	raw := clone.Text()
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	for i, w := range fields {
		fields[i] = strings.ToLower(w)
	}
	return fields
}

// extractHreflang extracts hreflang alternate links from head.
func extractHreflang(doc *goquery.Document, pageURL string) []types.HreflangEntry {
	var entries []types.HreflangEntry
	doc.Find("head link[rel='alternate']").Each(func(_ int, s *goquery.Selection) {
		hreflang := strings.TrimSpace(getSelectionAttr(s, "hreflang"))
		if hreflang == "" {
			return
		}
		href := strings.TrimSpace(getSelectionAttr(s, "href"))
		if href == "" {
			return
		}
		resolved := resolveCanonicalURL(href, pageURL)
		if resolved == "" {
			resolved = href
		}
		resolved = truncateRunes(resolved, types.MaxHreflangURLLength)
		entries = append(entries, types.HreflangEntry{Lang: hreflang, URL: resolved})
	})
	if len(entries) == 0 {
		return nil
	}
	return entries
}

func extractHreflangSelf(entries []types.HreflangEntry, pageURL string) string {
	if len(entries) == 0 {
		return ""
	}
	pageURLLower := strings.ToLower(pageURL)
	for _, entry := range entries {
		if strings.ToLower(entry.URL) == pageURLLower {
			return entry.Lang
		}
	}
	return ""
}

// extractStructuredDataTypes extracts @type values from JSON-LD scripts.
func extractStructuredDataTypes(doc *goquery.Document) []string {
	typeSet := make(map[string]struct{})
	doc.Find("script[type='application/ld+json']").Each(func(_ int, s *goquery.Selection) {
		content := s.Text()
		if len(content) > types.MaxJSONLDSize {
			return
		}
		extractTypesFromJSON([]byte(content), typeSet, 0)
	})
	if len(typeSet) == 0 {
		return nil
	}
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

// extractBreadcrumbs locates the first BreadcrumbList in JSON-LD scripts
// (document order) and returns its items as ordered BreadcrumbEntry slice.
// All input is treated as untrusted; the function returns nil on any malformed
// or unrecognized input and never panics on hostile JSON.
func extractBreadcrumbs(doc *goquery.Document, pageURL string) (result []types.BreadcrumbEntry) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
		}
	}()

	var list map[string]interface{}
	doc.Find("script[type='application/ld+json']").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		content := s.Text()
		if len(content) > types.MaxJSONLDSize {
			return true
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			return true
		}
		list = findFirstBreadcrumbList(parsed, 0)
		return list == nil
	})

	if list == nil {
		return nil
	}
	return buildBreadcrumbEntries(list, pageURL)
}

// findFirstBreadcrumbList walks JSON-LD in declaration order looking for an
// object with @type "BreadcrumbList". Recursion is limited to arrays and the
// @graph wrapper to keep traversal deterministic (Go map iteration is not
// ordered, so we deliberately do not recurse through arbitrary object keys).
func findFirstBreadcrumbList(v interface{}, depth int) map[string]interface{} {
	if depth > types.MaxJSONLDRecursionDepth {
		return nil
	}
	switch val := v.(type) {
	case map[string]interface{}:
		if isBreadcrumbListType(val["@type"]) {
			return val
		}
		if g, ok := val["@graph"]; ok {
			return findFirstBreadcrumbList(g, depth+1)
		}
	case []interface{}:
		for _, item := range val {
			if found := findFirstBreadcrumbList(item, depth+1); found != nil {
				return found
			}
		}
	}
	return nil
}

// isBreadcrumbListType reports whether @type names BreadcrumbList exactly.
// Case-sensitive (schema.org's canonical form).
func isBreadcrumbListType(v interface{}) bool {
	switch val := v.(type) {
	case string:
		return val == "BreadcrumbList"
	case []interface{}:
		for _, item := range val {
			if s, ok := item.(string); ok && s == "BreadcrumbList" {
				return true
			}
		}
	}
	return false
}

// buildBreadcrumbEntries reads itemListElement, orders by position, normalizes,
// filters non-navigational URLs, drops items without a URL, and caps at 5.
func buildBreadcrumbEntries(list map[string]interface{}, pageURL string) []types.BreadcrumbEntry {
	raw, ok := list["itemListElement"].([]interface{})
	if !ok || len(raw) == 0 {
		return nil
	}

	type pending struct {
		item     map[string]interface{}
		position int
		hasPos   bool
		declIdx  int
	}
	pendings := make([]pending, 0, len(raw))
	for i, e := range raw {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		pos, hasPos := readBreadcrumbPosition(m["position"])
		pendings = append(pendings, pending{item: m, position: pos, hasPos: hasPos, declIdx: i})
	}

	sort.SliceStable(pendings, func(i, j int) bool {
		if pendings[i].hasPos != pendings[j].hasPos {
			return pendings[i].hasPos
		}
		if pendings[i].hasPos {
			return pendings[i].position < pendings[j].position
		}
		return pendings[i].declIdx < pendings[j].declIdx
	})

	var result []types.BreadcrumbEntry
	for _, p := range pendings {
		rawURL := readBreadcrumbURL(p.item)
		if rawURL == "" || shouldSkipLink(rawURL) {
			continue
		}
		resolved := resolveCanonicalURL(rawURL, pageURL)
		if resolved == "" {
			resolved = rawURL
		}
		name := readBreadcrumbName(p.item)
		result = append(result, types.BreadcrumbEntry{
			Name: truncateRunes(collapseWhitespace(name), types.MaxHeadingLength),
			URL:  truncateRunes(resolved, types.MaxHreflangURLLength),
		})
		if len(result) >= types.MaxBreadcrumbs {
			break
		}
	}
	return result
}

// readBreadcrumbName resolves the display name from ListItem.name then item.name.
func readBreadcrumbName(item map[string]interface{}) string {
	if s := readJSONString(item, "name"); s != "" {
		return s
	}
	if inner, ok := item["item"].(map[string]interface{}); ok {
		return readJSONString(inner, "name")
	}
	return ""
}

// readBreadcrumbURL resolves the URL in order: item-as-string, item.@id,
// item.url, item.identifier.
func readBreadcrumbURL(item map[string]interface{}) string {
	if s, ok := item["item"].(string); ok && s != "" {
		return s
	}
	inner, ok := item["item"].(map[string]interface{})
	if !ok {
		return ""
	}
	for _, key := range []string{"@id", "url", "identifier"} {
		if s := readJSONString(inner, key); s != "" {
			return s
		}
	}
	return ""
}

// readBreadcrumbPosition extracts position from float64 or numeric string.
// Returns (0, false) for any other shape, NaN, infinity, fractional, negative,
// or out-of-int32-range values.
func readBreadcrumbPosition(v interface{}) (int, bool) {
	switch val := v.(type) {
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return 0, false
		}
		if val != math.Trunc(val) {
			return 0, false
		}
		if val < 0 || val > math.MaxInt32 {
			return 0, false
		}
		return int(val), true
	case string:
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			return 0, false
		}
		n, err := strconv.Atoi(trimmed)
		if err != nil || n < 0 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// readJSONString returns the string at key, or "" if the key is absent or the
// value is not a string.
func readJSONString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func (d *domDocument) ExtractPageSEO(statusCode int, pageURL string) *types.PageSEO {
	seo := &types.PageSEO{}

	seo.Title = extractSEOTitle(d.doc)
	seo.IndexStatus = d.IndexationStatus(statusCode, pageURL)
	seo.MetaDescription = extractMetaDescription(d.doc)
	seo.MetaRobots = extractMetaRobots(d.doc)

	canonicalRaw := extractCanonicalURL(d.doc)
	if canonicalRaw != "" {
		resolved := resolveCanonicalURL(canonicalRaw, pageURL)
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
