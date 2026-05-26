package htmlprocessor

import (
	"bytes"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/pkg/types"
	"golang.org/x/net/html"
)

var blockingDirectivePattern = regexp.MustCompile(`(?i)\b(noindex|none)\b`)

// domDocument implements Document interface using goquery for DOM parsing.
type domDocument struct {
	doc *goquery.Document
}

// ParseWithDOM parses HTML bytes into a Document using DOM parsing.
func ParseWithDOM(htmlBytes []byte) (Document, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBytes))
	if err != nil {
		return nil, err
	}
	return &domDocument{doc: doc}, nil
}

func (d *domDocument) GoQueryDoc() *goquery.Document {
	return d.doc
}

// containsBlockingDirective checks if content contains "noindex" or "none" directives.
func containsBlockingDirective(content string) bool {
	return blockingDirectivePattern.MatchString(content)
}

// resolveCanonicalURL resolves a canonical URL against a base URL.
// Returns empty string if canonical is empty.
// Returns canonical as-is if parsing fails (fail-safe).
func resolveCanonicalURL(canonical, baseURL string) string {
	if canonical == "" {
		return ""
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return canonical
	}

	ref, err := url.Parse(canonical)
	if err != nil {
		return canonical
	}

	return base.ResolveReference(ref).String()
}

// executableScriptTypes defines MIME types that indicate executable JavaScript.
var executableScriptTypes = map[string]bool{
	"text/javascript":        true,
	"module":                 true,
	"application/javascript": true,
}

func getSelectionAttr(s *goquery.Selection, name string) string {
	val, _ := s.Attr(name)
	return val
}

func (d *domDocument) Title() string {
	text := strings.TrimSpace(d.doc.Find("head title").First().Text())
	runes := []rune(text)
	if len(runes) > maxTitleLength {
		return string(runes[:maxTitleLength])
	}
	return text
}

func (d *domDocument) IndexationStatus(statusCode int, finalURL string) types.IndexStatus {
	if statusCode != http.StatusOK {
		return types.IndexStatusNon200
	}

	// Check meta robots blocking
	var googlebotContents []string
	var robotsContents []string

	d.doc.Find("head meta").Each(func(_ int, s *goquery.Selection) {
		name := strings.ToLower(getSelectionAttr(s, "name"))
		content := getSelectionAttr(s, "content")

		switch name {
		case "googlebot":
			googlebotContents = append(googlebotContents, content)
		case "robots":
			robotsContents = append(robotsContents, content)
		}
	})

	hasNonEmptyGooglebot := false
	for _, c := range googlebotContents {
		if strings.TrimSpace(c) != "" {
			hasNonEmptyGooglebot = true
			break
		}
	}

	var contentsToCheck []string
	if hasNonEmptyGooglebot {
		contentsToCheck = googlebotContents
	} else {
		contentsToCheck = robotsContents
	}

	for _, content := range contentsToCheck {
		if containsBlockingDirective(content) {
			return types.IndexStatusBlockedByMeta
		}
	}

	// Check canonical
	canonical := strings.TrimSpace(getSelectionAttr(d.doc.Find("head link[rel='canonical']").First(), "href"))
	if canonical != "" {
		resolved := resolveCanonicalURL(canonical, finalURL)
		if resolved != finalURL {
			return types.IndexStatusNonCanonical
		}
	}

	return types.IndexStatusIndexable
}

func (d *domDocument) CleanScripts() bool {
	removed := 0

	d.doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		scriptType := strings.ToLower(strings.TrimSpace(getSelectionAttr(s, "type")))
		if scriptType == "" || executableScriptTypes[scriptType] {
			s.Remove()
			removed++
		}
	})

	d.doc.Find("link").Each(func(_ int, s *goquery.Selection) {
		rel := strings.ToLower(getSelectionAttr(s, "rel"))
		switch rel {
		case "import", "modulepreload":
			s.Remove()
			removed++
		case "preload":
			if strings.ToLower(getSelectionAttr(s, "as")) == "script" {
				s.Remove()
				removed++
			}
		}
	})

	return removed > 0
}

// CleanFragmentScripts removes executable <script> elements and script-bearing
// <link> elements from sel and its descendants, reusing the executableScriptTypes
// classification so non-executable types (application/ld+json, application/json,
// text/template, importmap, ...) are PRESERVED. It mirrors the script/link logic of
// CleanScripts but scoped to a fragment, never the whole document. The caller passes
// a container selection: both the container's matching descendants (via Find) and the
// container's own roots (via Filter) are considered, so a fragment whose root IS a
// <script> is handled. Returns true if anything was removed.
func CleanFragmentScripts(sel *goquery.Selection) bool {
	removed := 0

	scripts := sel.Find("script").AddSelection(sel.Filter("script"))
	scripts.Each(func(_ int, s *goquery.Selection) {
		scriptType := strings.ToLower(strings.TrimSpace(getSelectionAttr(s, "type")))
		if scriptType == "" || executableScriptTypes[scriptType] {
			s.Remove()
			removed++
		}
	})

	links := sel.Find("link").AddSelection(sel.Filter("link"))
	links.Each(func(_ int, s *goquery.Selection) {
		rel := strings.ToLower(getSelectionAttr(s, "rel"))
		switch rel {
		case "import", "modulepreload":
			s.Remove()
			removed++
		case "preload":
			if strings.ToLower(getSelectionAttr(s, "as")) == "script" {
				s.Remove()
				removed++
			}
		}
	})

	return removed > 0
}

func (d *domDocument) HTML() []byte {
	var buf bytes.Buffer
	if len(d.doc.Nodes) == 0 {
		return nil
	}
	if err := html.Render(&buf, d.doc.Nodes[0]); err != nil {
		return nil
	}
	return buf.Bytes()
}
