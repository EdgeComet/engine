package htmlprocessor

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/pkg/types"
)

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
		resolved = truncateRunes(normalizeAbsoluteURL(resolved), types.MaxHreflangURLLength)
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
	// hreflang URLs are normalized at extraction, so normalize the page URL too for
	// a like-for-like self comparison.
	pageURLLower := strings.ToLower(normalizeAbsoluteURL(pageURL))
	for _, entry := range entries {
		if strings.ToLower(entry.URL) == pageURLLower {
			return entry.Lang
		}
	}
	return ""
}
