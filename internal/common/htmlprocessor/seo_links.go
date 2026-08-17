package htmlprocessor

import (
	"net/url"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/pkg/types"
	"golang.org/x/net/html"
)

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

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"

	// hrefFragmentPrefix marks a same-page anchor, which has no target of its own.
	hrefFragmentPrefix = "#"

	// stripHrefChars are dropped from anywhere in an href before it is resolved: the URL
	// spec has browsers and crawlers remove them, so a value wrapped across source lines
	// must resolve exactly like its single-line form.
	stripHrefChars = "\t\n\r"
)

// linkSchemes is the allowlist a URL must satisfy to be part of the page's link graph.
// One rule covers javascript:, mailto:, tel: and the app-scheme long tail (data:, ftp:,
// whatsapp:): their opaque bodies are not link targets and would otherwise reach the
// external-domain counts, where "whatsapp://send" reads as the domain "send".
var linkSchemes = map[string]bool{schemeHTTP: true, schemeHTTPS: true}

// isWebLink applies linkSchemes to a parsed URL. A reference that declares no scheme
// inherits the document's, so it passes here and is judged again after resolution.
func isWebLink(u *url.URL) bool {
	return u.Scheme == "" || linkSchemes[u.Scheme]
}

// declaresNonWebLink applies linkSchemes to an unresolved reference.
func declaresNonWebLink(ref string) bool {
	parsed, err := url.Parse(ref)
	return err == nil && !isWebLink(parsed)
}

// sanitizeHref trims an href and removes the characters browsers strip before parsing.
// An untrimmed " /about " otherwise yields a target whose hash never joins the page it
// points at, and a raw newline would break the domPathKeySep invariant downstream.
func sanitizeHref(href string) string {
	if strings.ContainsAny(href, stripHrefChars) {
		var b strings.Builder
		b.Grow(len(href))
		for i := 0; i < len(href); i++ {
			if strings.IndexByte(stripHrefChars, href[i]) < 0 {
				b.WriteByte(href[i])
			}
		}
		href = b.String()
	}
	return strings.TrimSpace(href)
}

// shouldSkipLink returns true if the href should be excluded from link metrics.
// Callers pass an href already run through sanitizeHref.
func shouldSkipLink(href string) bool {
	if href == "" || strings.HasPrefix(href, hrefFragmentPrefix) {
		return true
	}
	return declaresNonWebLink(href)
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

// effectiveBaseURL is the base for resolving the document's relative URLs:
// <base href> resolved against the page URL, else the page URL itself.
func effectiveBaseURL(doc *goquery.Document, pageURL string) string {
	if bh := extractBaseHref(doc); bh != "" {
		return resolveURL(bh, pageURL)
	}
	return pageURL
}

// extractLinkMetrics populates link metrics in the PageSEO struct.
// Extracts from body only, resolves relative URLs against base, classifies internal/external.
func extractLinkMetrics(doc *goquery.Document, base, pageURL string, seo *types.PageSEO) {
	if seo == nil {
		return
	}

	pageOrigin := ""
	if parsed, err := url.Parse(pageURL); err == nil {
		pageOrigin = parsed.Host
	}

	externalDomains := make(map[string]int)
	links := newLinkAccumulator()
	paths := newDOMPathBuilder()

	doc.Find("body a").Each(func(_ int, s *goquery.Selection) {
		href := sanitizeHref(getSelectionAttr(s, "href"))
		if shouldSkipLink(href) {
			return
		}

		resolved := resolveURL(href, base)
		parsed, parseErr := url.Parse(resolved)
		// Resolution decides the final scheme: a relative href under a non-web
		// <base href> is not a link either.
		if parseErr == nil && !isWebLink(parsed) {
			return
		}

		seo.LinksTotal++

		rel := strings.ToLower(getSelectionAttr(s, "rel"))
		isNofollow := strings.Contains(rel, "nofollow")

		isInternal := false
		if parseErr != nil {
			seo.LinksExternal++
			if isNofollow {
				seo.LinksNofollow++
				seo.LinksNofollowExternal++
			}
		} else {
			linkHost := parsed.Host
			isInternal = linkHost == "" || urlutil.IsSameOrigin(pageOrigin, linkHost)

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
		}

		// Per-link capture: additive, never affects the aggregate counts above.
		// Target stored as a normalized absolute string.
		target := captureTarget(resolved)
		if target == "" {
			return
		}
		// Consulting the page budget before the ancestor walk keeps a link-heavy page
		// from paying for placement paths that are dropped on arrival.
		if links.dropsNewTarget(target) {
			return
		}
		links.add(types.PageLink{
			Target:     target,
			Anchor:     truncateRunes(collapseWhitespace(BreakAwareText(s)), types.MaxAnchorLength),
			IsInternal: isInternal,
			Nofollow:   isNofollow,
			Sponsored:  strings.Contains(rel, "sponsored"),
			UGC:        strings.Contains(rel, "ugc"),
			IsImage:    s.Find("img").Length() > 0,
			DomPath:    paths.build(s),
		})
	})

	if len(externalDomains) > 0 {
		seo.ExternalDomains = topNDomains(externalDomains, types.MaxExternalDomains)
	}

	seo.PageLinks = links.result()
	seo.PageLinksTruncated = links.truncated
}

// domPathStep separators. The classifier treats #/./[]  as the only structural
// delimiters, so every token component is reduced to this charset.
const (
	domPathJoin             = ">"
	domPathKeySep           = "\n" // never present in a URL or a sanitized step
	domPathTruncationMarker = "..."
	domPathIDPrefix         = "#"
	domPathClassPrefix      = "."
	domPathRolePrefix       = "[role="
	domPathRoleSuffix       = "]"
)

// semanticListTags stay in the path even without id/class/role: they are stable
// placement signals (menus, article link lists, footers) that survive class churn.
var semanticListTags = map[string]bool{
	"header":  true,
	"nav":     true,
	"main":    true,
	"footer":  true,
	"aside":   true,
	"article": true,
	"section": true,
	"form":    true,
	"ul":      true,
	"ol":      true,
}

// domStepInfo is one element's contribution to a placement path.
type domStepInfo struct {
	step        string
	significant bool
}

// domPathBuilder renders placement paths for a single extraction pass. A page's anchors
// share their ancestors, so every element is rendered once and reused by each anchor
// below it: the pass costs O(elements) instead of O(anchors x depth).
type domPathBuilder struct {
	steps map[*html.Node]domStepInfo
}

func newDOMPathBuilder() *domPathBuilder {
	return &domPathBuilder{steps: make(map[*html.Node]domStepInfo)}
}

// build produces the normalized significant-ancestor path for a captured <a>, ordered
// outermost -> innermost and ending at the <a> itself. Pure and deterministic: the same
// DOM always yields the same slice.
func (b *domPathBuilder) build(s *goquery.Selection) []string {
	if len(s.Nodes) == 0 {
		return nil
	}
	anchor := s.Nodes[0]

	var reversed []string
	for n := anchor; n != nil && n.Type == html.ElementNode && n.Data != "body"; n = n.Parent {
		info := b.step(n)
		if n == anchor || info.significant {
			reversed = append(reversed, info.step)
		}
	}
	if len(reversed) == 0 {
		return nil
	}

	path := make([]string, len(reversed))
	for i, step := range reversed {
		path[len(reversed)-1-i] = step
	}
	return capDomPathDepth(path)
}

func (b *domPathBuilder) step(n *html.Node) domStepInfo {
	if info, ok := b.steps[n]; ok {
		return info
	}
	info := domStep(n)
	b.steps[n] = info
	return info
}

// domStep renders one element as its step string and reports whether the step is
// significant (kept for a non-anchor ancestor): it carries an id, a role, a class
// token, or is a semantic/list tag.
func domStep(n *html.Node) domStepInfo {
	tag := sanitizeDomToken(n.Data)
	id := sanitizeDomToken(nodeAttr(n, "id"))
	// A role attribute takes a list of values; only the first one names the placement,
	// so "navigation banner" must not collapse into an unmatchable "navigationbanner".
	role := sanitizeDomToken(firstAttrToken(nodeAttr(n, "role")))
	classes := domClassTokens(nodeAttr(n, "class"))

	// A bare element is its tag: significance is decided before assembly, so the generic
	// containers that dominate a deep page skip step building entirely.
	if id == "" && role == "" && len(classes) == 0 {
		return domStepInfo{step: capDomToken(tag), significant: semanticListTags[tag]}
	}
	return domStepInfo{step: domStepString(tag, id, role, classes), significant: true}
}

// domStepString assembles a step within the step-length budget, adding each component
// whole or not at all. A component severed mid-token stops matching the placement rule
// it was captured for, and a class cut at a token boundary can match a SHORTER rule
// token instead through the rule matcher's end anchor. Components are ASCII after
// sanitizeDomToken, so byte length is the rune length the budget is expressed in.
func domStepString(tag, id, role string, classes []string) string {
	var b strings.Builder
	tag = capDomToken(tag)
	b.WriteString(tag)
	budget := types.MaxDomPathStepLength - len(tag)

	appendPart := func(prefix, token, suffix string) {
		size := len(prefix) + len(token) + len(suffix)
		if size > budget {
			return
		}
		b.WriteString(prefix)
		b.WriteString(token)
		b.WriteString(suffix)
		budget -= size
	}

	if id != "" {
		appendPart(domPathIDPrefix, id, "")
	}
	for _, c := range classes {
		appendPart(domPathClassPrefix, c, "")
	}
	if role != "" {
		appendPart(domPathRolePrefix, role, domPathRoleSuffix)
	}
	return b.String()
}

func capDomToken(token string) string {
	if len(token) > types.MaxDomPathStepLength {
		return token[:types.MaxDomPathStepLength]
	}
	return token
}

// firstAttrToken returns the first whitespace-separated value of a space-delimited
// attribute.
func firstAttrToken(value string) string {
	if fields := strings.Fields(value); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// domClassTokens sanitizes, sorts, and caps the class attribute's tokens.
func domClassTokens(class string) []string {
	if class == "" {
		return nil
	}
	fields := strings.Fields(class)
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := sanitizeDomToken(f); t != "" {
			tokens = append(tokens, t)
		}
	}
	if len(tokens) == 0 {
		return nil
	}
	sort.Strings(tokens)
	deduped := tokens[:1]
	for _, t := range tokens[1:] {
		if t != deduped[len(deduped)-1] {
			deduped = append(deduped, t)
		}
	}
	if len(deduped) > types.MaxDomPathClasses {
		deduped = deduped[:types.MaxDomPathClasses]
	}
	return deduped
}

// sanitizeDomToken lowercases and keeps only [a-z0-9_-], so a step's only delimiters
// stay the structural #/./[]  and captured tokens align with the rule match charset.
func sanitizeDomToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func nodeAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// capDomPathDepth middle-truncates an over-deep path, keeping the outermost-zone and
// innermost-widget steps with a literal marker between them.
func capDomPathDepth(path []string) []string {
	if len(path) <= types.MaxDomPathSteps {
		return path
	}
	out := make([]string, 0, types.MaxDomPathSteps)
	out = append(out, path[:types.DomPathHeadSteps]...)
	out = append(out, domPathTruncationMarker)
	out = append(out, path[len(path)-types.DomPathTailSteps:]...)
	return out
}

// captureTarget is the stored form of a link target: the canonical normalizer applied
// to an already-resolved absolute URL. Empty when normalization fails - a raw href kept
// as a target hashes to a key no page row can ever match, so the edge is dropped rather
// than persisted wrong. Deliberately stricter than normalizeAbsoluteURL, whose
// best-effort passthrough suits display fields but not a join key.
func captureTarget(resolved string) string {
	res, err := seoURLNormalizer.Normalize(resolved, nil)
	if err != nil {
		return ""
	}
	return res.NormalizedURL
}

// linkAccumulator dedupes captured links by (target, dom_path) within a page,
// preserving first-seen order, ORing flags on collision and keeping the first
// non-empty anchor. Each target keeps at most MaxPlacementsPerTarget distinct
// placements (DOM order); further placements are dropped silently. New placement
// variants beyond MaxPageLinks are dropped and truncated is set. Targets arrive
// normalized and non-empty.
type linkAccumulator struct {
	order      []string
	byKey      map[string]*types.PageLink
	placements map[string]int
	truncated  bool
}

func newLinkAccumulator() *linkAccumulator {
	return &linkAccumulator{
		byKey:      make(map[string]*types.PageLink),
		placements: make(map[string]int),
	}
}

// dropsNewTarget reports whether a first placement for target would be dropped by the
// page budget, letting the caller skip the ancestor walk that only feeds the dropped
// key. Recording the truncation here keeps the signal independent of that walk. A
// target that already holds placements still needs its path built: the path decides
// whether the link merges into an existing key.
func (a *linkAccumulator) dropsNewTarget(target string) bool {
	if len(a.order) < types.MaxPageLinks || a.placements[target] > 0 {
		return false
	}
	a.truncated = true
	return true
}

func (a *linkAccumulator) add(link types.PageLink) {
	key := link.Target + domPathKeySep + strings.Join(link.DomPath, domPathJoin)
	if existing, ok := a.byKey[key]; ok {
		existing.IsInternal = existing.IsInternal || link.IsInternal
		existing.Nofollow = existing.Nofollow || link.Nofollow
		existing.Sponsored = existing.Sponsored || link.Sponsored
		existing.UGC = existing.UGC || link.UGC
		existing.IsImage = existing.IsImage || link.IsImage
		if existing.Anchor == "" && link.Anchor != "" {
			existing.Anchor = link.Anchor
		}
		return
	}
	if a.placements[link.Target] >= types.MaxPlacementsPerTarget {
		return
	}
	if len(a.order) >= types.MaxPageLinks {
		a.truncated = true
		return
	}
	stored := link
	a.byKey[key] = &stored
	a.order = append(a.order, key)
	a.placements[link.Target]++
}

func (a *linkAccumulator) result() []types.PageLink {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]types.PageLink, 0, len(a.order))
	for _, key := range a.order {
		out = append(out, *a.byKey[key])
	}
	return out
}

// extractImageMetrics populates image metrics in the PageSEO struct.
func extractImageMetrics(doc *goquery.Document, base, pageURL string, seo *types.PageSEO) {
	if seo == nil {
		return
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

		resolved := resolveURL(src, base)
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
