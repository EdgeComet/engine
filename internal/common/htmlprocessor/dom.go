package htmlprocessor

import (
	"bytes"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/edgecomet/engine/pkg/types"
	"golang.org/x/net/html"
)

var blockingDirectivePattern = regexp.MustCompile(`(?i)\b(noindex|none)\b`)

// domDocument implements Document interface using golang.org/x/net/html DOM parsing.
type domDocument struct {
	root *html.Node
}

// ParseWithDOM parses HTML bytes into a Document using DOM parsing.
func ParseWithDOM(htmlBytes []byte) (Document, error) {
	root, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return nil, err
	}
	return &domDocument{root: root}, nil
}

// findElement recursively searches for the first element with matching tag name (case-insensitive).
// Returns nil if not found.
func findElement(node *html.Node, tag string) *html.Node {
	if node == nil {
		return nil
	}
	return findElementLower(node, strings.ToLower(tag))
}

// findElementLower is the internal recursive helper that operates on pre-lowercased tag.
func findElementLower(node *html.Node, lowerTag string) *html.Node {
	if node.Type == html.ElementNode && strings.ToLower(node.Data) == lowerTag {
		return node
	}

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		if found := findElementLower(c, lowerTag); found != nil {
			return found
		}
	}
	return nil
}

// findElementInParent searches recursively within parent's subtree for matching element.
// Returns first matching element or nil if not found.
func findElementInParent(parent *html.Node, tag string) *html.Node {
	if parent == nil {
		return nil
	}
	lowerTag := strings.ToLower(tag)

	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		if found := findElementLower(c, lowerTag); found != nil {
			return found
		}
	}
	return nil
}

// findAllElementsInParent returns all matching elements within parent.
func findAllElementsInParent(parent *html.Node, tag string) []*html.Node {
	if parent == nil {
		return nil
	}
	tag = strings.ToLower(tag)
	var results []*html.Node

	var search func(*html.Node)
	search = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.ToLower(n.Data) == tag {
			results = append(results, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			search(c)
		}
	}

	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		search(c)
	}
	return results
}

// getAttr returns attribute value for given name (case-insensitive comparison).
// Returns empty string if not found.
func getAttr(node *html.Node, name string) string {
	if node == nil {
		return ""
	}
	name = strings.ToLower(name)
	for _, attr := range node.Attr {
		if strings.ToLower(attr.Key) == name {
			return attr.Val
		}
	}
	return ""
}

// getTextContent recursively extracts all text content from node and descendants.
func getTextContent(node *html.Node) string {
	if node == nil {
		return ""
	}

	var sb strings.Builder
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(node)
	return sb.String()
}

// containsBlockingDirective checks if content contains "noindex" or "none" directives.
func containsBlockingDirective(content string) bool {
	return blockingDirectivePattern.MatchString(content)
}

// isBlockedByMeta checks if the page is blocked by meta robots tags.
// Googlebot-specific tags take precedence over generic robots tags.
func isBlockedByMeta(head *html.Node) bool {
	metas := findAllElementsInParent(head, "meta")
	if len(metas) == 0 {
		return false
	}

	var googlebotContents []string
	var robotsContents []string

	for _, meta := range metas {
		name := strings.ToLower(getAttr(meta, "name"))
		content := getAttr(meta, "content")

		switch name {
		case "googlebot":
			googlebotContents = append(googlebotContents, content)
		case "robots":
			robotsContents = append(robotsContents, content)
		}
	}

	// If any googlebot tag has non-empty content, use only googlebot tags
	var contentsToCheck []string
	hasNonEmptyGooglebot := false
	for _, c := range googlebotContents {
		if strings.TrimSpace(c) != "" {
			hasNonEmptyGooglebot = true
			break
		}
	}

	if hasNonEmptyGooglebot {
		contentsToCheck = googlebotContents
	} else {
		contentsToCheck = robotsContents
	}

	for _, content := range contentsToCheck {
		if containsBlockingDirective(content) {
			return true
		}
	}

	return false
}

// extractCanonicalURL finds the first <link rel="canonical" href="..."> within head.
// Returns the href value (trimmed) or empty string if not found.
func extractCanonicalURL(head *html.Node) string {
	links := findAllElementsInParent(head, "link")
	for _, link := range links {
		rel := strings.ToLower(getAttr(link, "rel"))
		if rel == "canonical" {
			return strings.TrimSpace(getAttr(link, "href"))
		}
	}
	return ""
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

// isCanonical checks if the page's canonical URL matches the final URL.
// Returns true if no canonical tag exists or if canonical matches finalURL.
func isCanonical(head *html.Node, finalURL string) bool {
	canonical := extractCanonicalURL(head)
	if canonical == "" {
		return true
	}

	resolved := resolveCanonicalURL(canonical, finalURL)
	return resolved == finalURL
}

// executableScriptTypes defines MIME types that indicate executable JavaScript.
var executableScriptTypes = map[string]bool{
	"text/javascript":        true,
	"module":                 true,
	"application/javascript": true,
}

// isExecutableScript checks if a node is an executable script element.
// Returns true for <script> tags with no type, empty type, or executable types.
func isExecutableScript(node *html.Node) bool {
	if node == nil || node.Type != html.ElementNode {
		return false
	}
	if strings.ToLower(node.Data) != "script" {
		return false
	}

	scriptType := strings.ToLower(strings.TrimSpace(getAttr(node, "type")))

	// Empty or whitespace-only type means executable
	if scriptType == "" {
		return true
	}

	return executableScriptTypes[scriptType]
}

// isScriptRelatedLink checks if a link element is script-related and should be removed.
// Returns true for import, modulepreload, or preload with as="script".
func isScriptRelatedLink(node *html.Node) bool {
	if node == nil || node.Type != html.ElementNode {
		return false
	}
	if strings.ToLower(node.Data) != "link" {
		return false
	}

	rel := strings.ToLower(getAttr(node, "rel"))

	switch rel {
	case "import", "modulepreload":
		return true
	case "preload":
		return strings.ToLower(getAttr(node, "as")) == "script"
	}

	return false
}

func (d *domDocument) Title() string {
	head := findElement(d.root, "head")
	if head == nil {
		return ""
	}

	title := findElementInParent(head, "title")
	if title == nil {
		return ""
	}

	text := strings.TrimSpace(getTextContent(title))

	runes := []rune(text)
	if len(runes) > maxTitleLength {
		return string(runes[:maxTitleLength])
	}
	return text
}

func (d *domDocument) IndexationStatus(statusCode int, finalURL string) types.IndexStatus {
	// Priority 1: Non-200 status code
	if statusCode != http.StatusOK {
		return types.IndexStatusNon200
	}

	head := findElement(d.root, "head")

	// Priority 2: Blocked by meta robots
	if head != nil && isBlockedByMeta(head) {
		return types.IndexStatusBlockedByMeta
	}

	// Priority 3: Non-canonical
	if head != nil && !isCanonical(head, finalURL) {
		return types.IndexStatusNonCanonical
	}

	// Priority 4: Indexable
	return types.IndexStatusIndexable
}

func (d *domDocument) CleanScripts() bool {
	var toRemove []*html.Node

	var collect func(*html.Node)
	collect = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if isExecutableScript(n) || isScriptRelatedLink(n) {
				toRemove = append(toRemove, n)
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(d.root)

	for _, node := range toRemove {
		if node.Parent != nil {
			node.Parent.RemoveChild(node)
		}
	}

	return len(toRemove) > 0
}

func (d *domDocument) HTML() []byte {
	var buf bytes.Buffer
	if err := html.Render(&buf, d.root); err != nil {
		return nil
	}
	return buf.Bytes()
}
