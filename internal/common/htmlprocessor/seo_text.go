package htmlprocessor

import (
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
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
