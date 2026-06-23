package htmlprocessor

import (
	"net/url"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/pkg/types"
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

	doc.Find("body a").Each(func(_ int, s *goquery.Selection) {
		href := getSelectionAttr(s, "href")
		if shouldSkipLink(href) {
			return
		}

		seo.LinksTotal++

		rel := strings.ToLower(getSelectionAttr(s, "rel"))
		isNofollow := strings.Contains(rel, "nofollow")

		resolved := resolveURL(href, base)
		parsed, parseErr := url.Parse(resolved)

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
		links.add(types.PageLink{
			Target:     normalizeAbsoluteURL(resolved),
			Anchor:     truncateRunes(collapseWhitespace(s.Text()), types.MaxAnchorLength),
			IsInternal: isInternal,
			Nofollow:   isNofollow,
			Sponsored:  strings.Contains(rel, "sponsored"),
			UGC:        strings.Contains(rel, "ugc"),
			IsImage:    s.Find("img").Length() > 0,
		})
	})

	if len(externalDomains) > 0 {
		seo.ExternalDomains = topNDomains(externalDomains, types.MaxExternalDomains)
	}

	seo.PageLinks = links.result()
	seo.PageLinksTruncated = links.truncated
}

// linkAccumulator dedupes captured links by normalized target within a page,
// preserving first-seen order, ORing flags on collision and keeping the first
// non-empty anchor. New distinct targets beyond MaxPageLinks are dropped (existing
// targets still merge) and truncated is set.
type linkAccumulator struct {
	order     []string
	byTarget  map[string]*types.PageLink
	truncated bool
}

func newLinkAccumulator() *linkAccumulator {
	return &linkAccumulator{byTarget: make(map[string]*types.PageLink)}
}

func (a *linkAccumulator) add(link types.PageLink) {
	if link.Target == "" {
		return
	}
	if existing, ok := a.byTarget[link.Target]; ok {
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
	if len(a.order) >= types.MaxPageLinks {
		a.truncated = true
		return
	}
	stored := link
	a.byTarget[link.Target] = &stored
	a.order = append(a.order, link.Target)
}

func (a *linkAccumulator) result() []types.PageLink {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]types.PageLink, 0, len(a.order))
	for _, target := range a.order {
		out = append(out, *a.byTarget[target])
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
