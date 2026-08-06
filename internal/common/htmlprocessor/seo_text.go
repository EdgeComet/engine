package htmlprocessor

import (
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

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

// bodyBoilerplateTags are the subtrees whose text never counts as body content.
// A set rather than a CSS selector because the walk below skips them in place, and
// membership matches what a type selector tests: element node, lowercased tag name.
//
// FROZEN: the membership of this set is part of the fingerprint format.
var bodyBoilerplateTags = map[string]struct{}{
	"nav":      {},
	"header":   {},
	"footer":   {},
	"aside":    {},
	"form":     {},
	"script":   {},
	"style":    {},
	"noscript": {},
}

// extractBodyWords returns the visible body text as lowercased whitespace-separated
// tokens, with boilerplate subtrees excluded.
//
// FROZEN: this token stream feeds the page content fingerprint, so the function is
// part of a stored format, not just a word counter. Changing the excluded tag set,
// the whitespace splitting or the lowercasing silently makes fingerprints computed
// after the change incomparable with every fingerprint computed before it - pages
// would appear to have changed on the deployment date alone, with no error and no way
// to tell affected comparisons from real content changes. Treat any behavioral change
// here as a breaking change to the fingerprint format. How the text is collected is
// free to change; what comes out is not.
func extractBodyWords(doc *goquery.Document) []string {
	body := doc.Find("body")
	if body.Length() == 0 {
		return nil
	}

	var text strings.Builder
	for _, node := range body.Nodes {
		appendBodyText(&text, node)
	}

	fields := strings.Fields(text.String())
	if len(fields) == 0 {
		return nil
	}
	for i, w := range fields {
		fields[i] = strings.ToLower(w)
	}
	return fields
}

// appendBodyText walks the subtree depth first and appends every text node that does
// not sit inside a boilerplate subtree. Skipping in place costs one pass and no copy;
// deleting the same subtrees from a clone of the body would first duplicate the whole
// node tree, which on a markup-dense page roughly doubles the memory the document
// occupies while extraction runs.
//
// Collecting into one buffer and splitting afterwards is what fuses words across
// element boundaries that carry no whitespace, so "a<b>c</b>d" is the single token
// "acd". That is frozen: tokenizing per text node would split those and move the
// fingerprint of every page containing inline markup mid-word.
func appendBodyText(text *strings.Builder, n *html.Node) {
	if n.Type == html.ElementNode {
		if _, skip := bodyBoilerplateTags[n.Data]; skip {
			return
		}
	}
	if n.Type == html.TextNode {
		text.WriteString(n.Data)
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		appendBodyText(text, child)
	}
}
