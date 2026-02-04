package htmlprocessor

import (
	"strings"
	"testing"

	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/html"
)

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "string within limit",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "string at exact limit",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "string exceeds limit ASCII",
			input:    "hello world",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "unicode within limit",
			input:    "日本語テスト",
			maxLen:   10,
			expected: "日本語テスト",
		},
		{
			name:     "unicode exceeds limit",
			input:    "日本語テスト",
			maxLen:   3,
			expected: "日本語",
		},
		{
			name:     "mixed ASCII and unicode",
			input:    "hello日本語world",
			maxLen:   8,
			expected: "hello日本語",
		},
		{
			name:     "emoji handling",
			input:    "test🎉emoji🎊here",
			maxLen:   6,
			expected: "test🎉e",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateRunes(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollapseWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no whitespace",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "leading whitespace",
			input:    "   hello",
			expected: "hello",
		},
		{
			name:     "trailing whitespace",
			input:    "hello   ",
			expected: "hello",
		},
		{
			name:     "internal single space",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "internal multiple spaces",
			input:    "hello    world",
			expected: "hello world",
		},
		{
			name:     "tabs and newlines",
			input:    "hello\t\n\r  world",
			expected: "hello world",
		},
		{
			name:     "all whitespace types",
			input:    "  \t hello \n\r  world  \t ",
			expected: "hello world",
		},
		{
			name:     "only whitespace",
			input:    "   \t\n  ",
			expected: "",
		},
		{
			name:     "multiple words",
			input:    "  one   two   three  ",
			expected: "one two three",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collapseWhitespace(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTopNDomains(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]int
		n        int
		expected map[string]int
	}{
		{
			name:     "nil map",
			input:    nil,
			n:        5,
			expected: nil,
		},
		{
			name:     "empty map",
			input:    map[string]int{},
			n:        5,
			expected: map[string]int{},
		},
		{
			name:     "fewer than N domains",
			input:    map[string]int{"a.com": 5, "b.com": 3},
			n:        5,
			expected: map[string]int{"a.com": 5, "b.com": 3},
		},
		{
			name:     "exactly N domains",
			input:    map[string]int{"a.com": 5, "b.com": 3, "c.com": 1},
			n:        3,
			expected: map[string]int{"a.com": 5, "b.com": 3, "c.com": 1},
		},
		{
			name:     "more than N domains - by count",
			input:    map[string]int{"a.com": 1, "b.com": 5, "c.com": 3, "d.com": 2},
			n:        2,
			expected: map[string]int{"b.com": 5, "c.com": 3},
		},
		{
			name:     "tie breaking alphabetically",
			input:    map[string]int{"zebra.com": 5, "alpha.com": 5, "beta.com": 5, "gamma.com": 3},
			n:        2,
			expected: map[string]int{"alpha.com": 5, "beta.com": 5},
		},
		{
			name:     "complex tie breaking",
			input:    map[string]int{"z.com": 10, "a.com": 5, "b.com": 5, "c.com": 5, "d.com": 1},
			n:        3,
			expected: map[string]int{"z.com": 10, "a.com": 5, "b.com": 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := topNDomains(tt.input, tt.n)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper to parse HTML and find head element
func parseAndFindHead(t *testing.T, htmlStr string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	return findElement(doc, "head")
}

func TestExtractSEOTitle(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "basic title",
			html:     `<html><head><title>Hello World</title></head></html>`,
			expected: "Hello World",
		},
		{
			name:     "title with whitespace",
			html:     `<html><head><title>  Hello World  </title></head></html>`,
			expected: "Hello World",
		},
		{
			name:     "no title tag",
			html:     `<html><head></head></html>`,
			expected: "",
		},
		{
			name:     "empty title",
			html:     `<html><head><title></title></head></html>`,
			expected: "",
		},
		{
			name:     "no head",
			html:     `<html><body><title>Ignored</title></body></html>`,
			expected: "",
		},
		{
			name:     "unicode title",
			html:     `<html><head><title>日本語タイトル</title></head></html>`,
			expected: "日本語タイトル",
		},
		{
			name:     "title truncation at 500 chars",
			html:     `<html><head><title>` + strings.Repeat("a", 600) + `</title></head></html>`,
			expected: strings.Repeat("a", 500),
		},
		{
			name:     "title at exactly 500 chars",
			html:     `<html><head><title>` + strings.Repeat("b", 500) + `</title></head></html>`,
			expected: strings.Repeat("b", 500),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head := parseAndFindHead(t, tt.html)
			result := extractSEOTitle(head)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractMetaDescription(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "basic description",
			html:     `<html><head><meta name="description" content="This is a description"></head></html>`,
			expected: "This is a description",
		},
		{
			name:     "case insensitive name",
			html:     `<html><head><meta name="Description" content="Test"></head></html>`,
			expected: "Test",
		},
		{
			name:     "no description",
			html:     `<html><head><meta name="keywords" content="test"></head></html>`,
			expected: "",
		},
		{
			name:     "empty content",
			html:     `<html><head><meta name="description" content=""></head></html>`,
			expected: "",
		},
		{
			name:     "whitespace only content",
			html:     `<html><head><meta name="description" content="   "></head></html>`,
			expected: "",
		},
		{
			name:     "content with leading/trailing whitespace",
			html:     `<html><head><meta name="description" content="  Hello World  "></head></html>`,
			expected: "Hello World",
		},
		{
			name:     "first description wins",
			html:     `<html><head><meta name="description" content="First"><meta name="description" content="Second"></head></html>`,
			expected: "First",
		},
		{
			name:     "truncation at 1000 chars",
			html:     `<html><head><meta name="description" content="` + strings.Repeat("x", 1100) + `"></head></html>`,
			expected: strings.Repeat("x", 1000),
		},
		{
			name:     "unicode description",
			html:     `<html><head><meta name="description" content="日本語の説明文"></head></html>`,
			expected: "日本語の説明文",
		},
		{
			name:     "description in body ignored",
			html:     `<html><head></head><body><meta name="description" content="Ignored"></body></html>`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head := parseAndFindHead(t, tt.html)
			result := extractMetaDescription(head)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractMetaRobots(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "robots tag",
			html:     `<html><head><meta name="robots" content="noindex, nofollow"></head></html>`,
			expected: "noindex, nofollow",
		},
		{
			name:     "googlebot tag",
			html:     `<html><head><meta name="googlebot" content="noindex"></head></html>`,
			expected: "noindex",
		},
		{
			name:     "googlebot takes precedence",
			html:     `<html><head><meta name="robots" content="index"><meta name="googlebot" content="noindex"></head></html>`,
			expected: "noindex",
		},
		{
			name:     "robots used when googlebot empty",
			html:     `<html><head><meta name="googlebot" content=""><meta name="robots" content="nofollow"></head></html>`,
			expected: "nofollow",
		},
		{
			name:     "no robots or googlebot",
			html:     `<html><head><meta name="description" content="test"></head></html>`,
			expected: "",
		},
		{
			name:     "case insensitive",
			html:     `<html><head><meta name="ROBOTS" content="noindex"></head></html>`,
			expected: "noindex",
		},
		{
			name:     "whitespace trimmed",
			html:     `<html><head><meta name="robots" content="  noindex  "></head></html>`,
			expected: "noindex",
		},
		{
			name:     "robots in body ignored",
			html:     `<html><head></head><body><meta name="robots" content="noindex"></body></html>`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head := parseAndFindHead(t, tt.html)
			result := extractMetaRobots(head)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractBaseHref(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "basic base href",
			html:     `<html><head><base href="https://example.com/"></head></html>`,
			expected: "https://example.com/",
		},
		{
			name:     "no base tag",
			html:     `<html><head></head></html>`,
			expected: "",
		},
		{
			name:     "empty href",
			html:     `<html><head><base href=""></head></html>`,
			expected: "",
		},
		{
			name:     "relative base href",
			html:     `<html><head><base href="/subdir/"></head></html>`,
			expected: "/subdir/",
		},
		{
			name:     "whitespace trimmed",
			html:     `<html><head><base href="  https://example.com/  "></head></html>`,
			expected: "https://example.com/",
		},
		{
			name:     "no head",
			html:     `<html><body></body></html>`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head := parseAndFindHead(t, tt.html)
			result := extractBaseHref(head)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper to parse HTML and find body element
func parseAndFindBody(t *testing.T, htmlStr string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	return findElement(doc, "body")
}

func TestExtractHeadings(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		tag      string
		maxCount int
		expected []string
	}{
		{
			name:     "single h1",
			html:     `<html><body><h1>Main Title</h1></body></html>`,
			tag:      "h1",
			maxCount: 5,
			expected: []string{"Main Title"},
		},
		{
			name:     "multiple h1s limited to 5",
			html:     `<html><body><h1>One</h1><h1>Two</h1><h1>Three</h1><h1>Four</h1><h1>Five</h1><h1>Six</h1></body></html>`,
			tag:      "h1",
			maxCount: 5,
			expected: []string{"One", "Two", "Three", "Four", "Five"},
		},
		{
			name:     "skip empty headings",
			html:     `<html><body><h1></h1><h1>First Real</h1><h1>   </h1><h1>Second Real</h1></body></html>`,
			tag:      "h1",
			maxCount: 5,
			expected: []string{"First Real", "Second Real"},
		},
		{
			name:     "whitespace collapsed",
			html:     `<html><body><h1>Hello    World</h1></body></html>`,
			tag:      "h1",
			maxCount: 5,
			expected: []string{"Hello World"},
		},
		{
			name:     "nested elements text extracted",
			html:     `<html><body><h1>Hello <span>Nested</span> World</h1></body></html>`,
			tag:      "h1",
			maxCount: 5,
			expected: []string{"Hello Nested World"},
		},
		{
			name:     "h2 extraction",
			html:     `<html><body><h2>Subtitle One</h2><h2>Subtitle Two</h2></body></html>`,
			tag:      "h2",
			maxCount: 5,
			expected: []string{"Subtitle One", "Subtitle Two"},
		},
		{
			name:     "h3 extraction",
			html:     `<html><body><h3>Section A</h3><h3>Section B</h3></body></html>`,
			tag:      "h3",
			maxCount: 5,
			expected: []string{"Section A", "Section B"},
		},
		{
			name:     "no headings",
			html:     `<html><body><p>No headings here</p></body></html>`,
			tag:      "h1",
			maxCount: 5,
			expected: nil,
		},
		{
			name:     "all empty headings returns nil",
			html:     `<html><body><h1></h1><h1>   </h1></body></html>`,
			tag:      "h1",
			maxCount: 5,
			expected: nil,
		},
		{
			name:     "truncation at 500 chars",
			html:     `<html><body><h1>` + strings.Repeat("a", 600) + `</h1></body></html>`,
			tag:      "h1",
			maxCount: 5,
			expected: []string{strings.Repeat("a", 500)},
		},
		{
			name:     "deeply nested heading",
			html:     `<html><body><div><section><article><h1>Nested Deep</h1></article></section></div></body></html>`,
			tag:      "h1",
			maxCount: 5,
			expected: []string{"Nested Deep"},
		},
		{
			name:     "case insensitive tag",
			html:     `<html><body><H1>Uppercase Tag</H1></body></html>`,
			tag:      "h1",
			maxCount: 5,
			expected: []string{"Uppercase Tag"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := parseAndFindBody(t, tt.html)
			result := extractHeadings(body, tt.tag, tt.maxCount)
			assert.Equal(t, tt.expected, result)
		})
	}

	// Explicit nil body test
	t.Run("nil body returns nil", func(t *testing.T) {
		result := extractHeadings(nil, "h1", 5)
		assert.Nil(t, result)
	})
}

func TestExtractLinkMetrics(t *testing.T) {
	tests := []struct {
		name           string
		html           string
		pageURL        string
		expectTotal    int
		expectInternal int
		expectExternal int
		expectDomains  map[string]int
	}{
		{
			name:           "internal links same host",
			html:           `<html><body><a href="https://example.com/page1">Link 1</a><a href="https://example.com/page2">Link 2</a></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    2,
			expectInternal: 2,
			expectExternal: 0,
		},
		{
			name:           "external links",
			html:           `<html><body><a href="https://other.com/page">External</a></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    1,
			expectInternal: 0,
			expectExternal: 1,
			expectDomains:  map[string]int{"other.com": 1},
		},
		{
			name:           "relative URLs are internal",
			html:           `<html><body><a href="/page">Relative</a><a href="page2">Also Relative</a></body></html>`,
			pageURL:        "https://example.com/dir/",
			expectTotal:    2,
			expectInternal: 2,
			expectExternal: 0,
		},
		{
			name:           "skip javascript links",
			html:           `<html><body><a href="javascript:void(0)">JS</a><a href="https://example.com/page">Real</a></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    1,
			expectInternal: 1,
			expectExternal: 0,
		},
		{
			name:           "skip mailto links",
			html:           `<html><body><a href="mailto:test@example.com">Email</a></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    0,
			expectInternal: 0,
			expectExternal: 0,
		},
		{
			name:           "skip tel links",
			html:           `<html><body><a href="tel:+1234567890">Phone</a></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    0,
			expectInternal: 0,
			expectExternal: 0,
		},
		{
			name:           "skip fragment-only links",
			html:           `<html><body><a href="#">Top</a><a href="#section">Section</a></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    0,
			expectInternal: 0,
			expectExternal: 0,
		},
		{
			name:           "skip empty href",
			html:           `<html><body><a href="">Empty</a><a>No href</a></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    0,
			expectInternal: 0,
			expectExternal: 0,
		},
		{
			name:           "subdomain is internal",
			html:           `<html><body><a href="https://cdn.example.com/asset">CDN</a><a href="https://www.example.com/page">WWW</a></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    2,
			expectInternal: 2,
			expectExternal: 0,
		},
		{
			name:           "protocol-relative URL",
			html:           `<html><body><a href="//other.com/page">Protocol Relative</a></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    1,
			expectInternal: 0,
			expectExternal: 1,
			expectDomains:  map[string]int{"other.com": 1},
		},
		{
			name:           "multiple external domains",
			html:           `<html><body><a href="https://a.com">A</a><a href="https://b.com">B</a><a href="https://a.com/2">A2</a></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    3,
			expectInternal: 0,
			expectExternal: 3,
			expectDomains:  map[string]int{"a.com": 2, "b.com": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := parseAndFindBody(t, tt.html)
			seo := &types.PageSEO{}
			extractLinkMetrics(body, "", tt.pageURL, seo)

			assert.Equal(t, tt.expectTotal, seo.LinksTotal, "LinksTotal mismatch")
			assert.Equal(t, tt.expectInternal, seo.LinksInternal, "LinksInternal mismatch")
			assert.Equal(t, tt.expectExternal, seo.LinksExternal, "LinksExternal mismatch")
			if tt.expectDomains != nil {
				assert.Equal(t, tt.expectDomains, seo.ExternalDomains, "ExternalDomains mismatch")
			}
		})
	}
}

func TestExtractImageMetrics(t *testing.T) {
	tests := []struct {
		name           string
		html           string
		pageURL        string
		expectTotal    int
		expectInternal int
		expectExternal int
	}{
		{
			name:           "internal images",
			html:           `<html><body><img src="https://example.com/img.png"><img src="/local.jpg"></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    2,
			expectInternal: 2,
			expectExternal: 0,
		},
		{
			name:           "external images",
			html:           `<html><body><img src="https://cdn.other.com/img.png"></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    1,
			expectInternal: 0,
			expectExternal: 1,
		},
		{
			name:           "skip data URLs",
			html:           `<html><body><img src="data:image/png;base64,ABC123"><img src="https://example.com/real.png"></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    1,
			expectInternal: 1,
			expectExternal: 0,
		},
		{
			name:           "skip blob URLs",
			html:           `<html><body><img src="blob:https://example.com/uuid"></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    0,
			expectInternal: 0,
			expectExternal: 0,
		},
		{
			name:           "skip empty src",
			html:           `<html><body><img src=""><img></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    0,
			expectInternal: 0,
			expectExternal: 0,
		},
		{
			name:           "subdomain CDN is internal",
			html:           `<html><body><img src="https://cdn.example.com/img.png"></body></html>`,
			pageURL:        "https://example.com/",
			expectTotal:    1,
			expectInternal: 1,
			expectExternal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := parseAndFindBody(t, tt.html)
			seo := &types.PageSEO{}
			extractImageMetrics(body, "", tt.pageURL, seo)

			assert.Equal(t, tt.expectTotal, seo.ImagesTotal, "ImagesTotal mismatch")
			assert.Equal(t, tt.expectInternal, seo.ImagesInternal, "ImagesInternal mismatch")
			assert.Equal(t, tt.expectExternal, seo.ImagesExternal, "ImagesExternal mismatch")
		})
	}
}

func TestLinkMetricsWithBaseTag(t *testing.T) {
	html := `<html><body><a href="page.html">Relative Link</a></body></html>`
	body := parseAndFindBody(t, html)
	seo := &types.PageSEO{}

	extractLinkMetrics(body, "https://cdn.example.com/base/", "https://example.com/", seo)

	// Link should resolve against base href, which is on cdn.example.com (internal via subdomain)
	assert.Equal(t, 1, seo.LinksTotal)
	assert.Equal(t, 1, seo.LinksInternal)
}

// Helper to parse full HTML document
func parseDocument(t *testing.T, htmlStr string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	return doc
}

func TestExtractHreflang(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		pageURL  string
		expected []types.HreflangEntry
	}{
		{
			name: "multiple hreflang entries",
			html: `<html><head>
                <link rel="alternate" hreflang="en" href="https://example.com/en">
                <link rel="alternate" hreflang="de" href="https://example.com/de">
                <link rel="alternate" hreflang="x-default" href="https://example.com/">
            </head></html>`,
			pageURL: "https://example.com/",
			expected: []types.HreflangEntry{
				{Lang: "en", URL: "https://example.com/en"},
				{Lang: "de", URL: "https://example.com/de"},
				{Lang: "x-default", URL: "https://example.com/"},
			},
		},
		{
			name:     "no hreflang",
			html:     `<html><head><link rel="canonical" href="https://example.com/"></head></html>`,
			pageURL:  "https://example.com/",
			expected: nil,
		},
		{
			name: "skip missing hreflang attribute",
			html: `<html><head>
                <link rel="alternate" href="https://example.com/page">
            </head></html>`,
			pageURL:  "https://example.com/",
			expected: nil,
		},
		{
			name: "skip empty hreflang",
			html: `<html><head>
                <link rel="alternate" hreflang="" href="https://example.com/page">
            </head></html>`,
			pageURL:  "https://example.com/",
			expected: nil,
		},
		{
			name: "skip empty href",
			html: `<html><head>
                <link rel="alternate" hreflang="en" href="">
            </head></html>`,
			pageURL:  "https://example.com/",
			expected: nil,
		},
		{
			name: "relative URL resolved",
			html: `<html><head>
                <link rel="alternate" hreflang="en" href="/en/page">
            </head></html>`,
			pageURL: "https://example.com/",
			expected: []types.HreflangEntry{
				{Lang: "en", URL: "https://example.com/en/page"},
			},
		},
		{
			name: "URL truncation at 2000 chars",
			html: `<html><head>
                <link rel="alternate" hreflang="en" href="https://example.com/` + strings.Repeat("a", 2100) + `">
            </head></html>`,
			pageURL: "https://example.com/",
			expected: []types.HreflangEntry{
				{Lang: "en", URL: truncateRunes("https://example.com/"+strings.Repeat("a", 2100), 2000)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head := parseAndFindHead(t, tt.html)
			result := extractHreflang(head, tt.pageURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractStructuredDataTypes(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected []string
	}{
		{
			name:     "single type",
			html:     `<html><head><script type="application/ld+json">{"@type": "Product"}</script></head></html>`,
			expected: []string{"Product"},
		},
		{
			name:     "array type",
			html:     `<html><head><script type="application/ld+json">{"@type": ["Product", "Thing"]}</script></head></html>`,
			expected: []string{"Product", "Thing"},
		},
		{
			name: "with @graph",
			html: `<html><head><script type="application/ld+json">{
                "@graph": [
                    {"@type": "WebSite"},
                    {"@type": "Organization"}
                ]
            }</script></head></html>`,
			expected: []string{"Organization", "WebSite"},
		},
		{
			name: "nested objects",
			html: `<html><head><script type="application/ld+json">{
                "@type": "Product",
                "offers": {"@type": "Offer"},
                "brand": {"@type": "Brand"}
            }</script></head></html>`,
			expected: []string{"Brand", "Offer", "Product"},
		},
		{
			name: "multiple JSON-LD blocks",
			html: `<html><head>
                <script type="application/ld+json">{"@type": "Product"}</script>
                <script type="application/ld+json">{"@type": "BreadcrumbList"}</script>
            </head></html>`,
			expected: []string{"BreadcrumbList", "Product"},
		},
		{
			name: "JSON-LD in body",
			html: `<html><head></head><body>
                <script type="application/ld+json">{"@type": "Article"}</script>
            </body></html>`,
			expected: []string{"Article"},
		},
		{
			name: "deduplicated types",
			html: `<html><head>
                <script type="application/ld+json">{"@type": "Product"}</script>
                <script type="application/ld+json">{"@type": "Product"}</script>
            </head></html>`,
			expected: []string{"Product"},
		},
		{
			name:     "malformed JSON ignored",
			html:     `<html><head><script type="application/ld+json">{invalid json}</script></head></html>`,
			expected: nil,
		},
		{
			name:     "no JSON-LD",
			html:     `<html><head><script>console.log("test")</script></head></html>`,
			expected: nil,
		},
		{
			name:     "case preserved",
			html:     `<html><head><script type="application/ld+json">{"@type": "LocalBusiness"}</script></head></html>`,
			expected: []string{"LocalBusiness"},
		},
		{
			name:     "empty @type ignored",
			html:     `<html><head><script type="application/ld+json">{"@type": ""}</script></head></html>`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseDocument(t, tt.html)
			result := extractStructuredDataTypes(doc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPageSEO_Integration(t *testing.T) {
	htmlContent := `<!DOCTYPE html>
<html>
<head>
    <title>Test Product - Best Deals</title>
    <meta name="description" content="Find the best deals on test products.">
    <meta name="robots" content="index, follow">
    <link rel="canonical" href="https://example.com/products/test">
    <link rel="alternate" hreflang="en" href="https://example.com/en/products/test">
    <link rel="alternate" hreflang="de" href="https://example.de/produkte/test">
    <script type="application/ld+json">
    {
        "@type": "Product",
        "name": "Test Product",
        "offers": {"@type": "Offer", "price": "99.99"}
    }
    </script>
</head>
<body>
    <h1>Test Product</h1>
    <h2>Features</h2>
    <h2>Reviews</h2>
    <h3>Dimensions</h3>

    <a href="/other-product">Related Product</a>
    <a href="https://example.com/category">Category</a>
    <a href="https://external.com/review">External Review</a>
    <a href="https://partner.com/link">Partner</a>

    <img src="/images/product.jpg" alt="Product">
    <img src="https://cdn.example.com/images/large.jpg" alt="Large">
    <img src="https://external-cdn.com/image.png" alt="External">
</body>
</html>`

	doc, err := ParseWithDOM([]byte(htmlContent))
	assert.NoError(t, err)

	seo := doc.ExtractPageSEO(200, "https://example.com/products/test")

	// Basic metadata
	assert.Equal(t, "Test Product - Best Deals", seo.Title)
	assert.Equal(t, types.IndexStatusIndexable, seo.IndexStatus)
	assert.Equal(t, "Find the best deals on test products.", seo.MetaDescription)
	assert.Equal(t, "index, follow", seo.MetaRobots)
	assert.Equal(t, "https://example.com/products/test", seo.CanonicalURL)

	// Headings
	assert.Equal(t, []string{"Test Product"}, seo.H1s)
	assert.Equal(t, []string{"Features", "Reviews"}, seo.H2s)
	assert.Equal(t, []string{"Dimensions"}, seo.H3s)

	// Links
	assert.Equal(t, 4, seo.LinksTotal)
	assert.Equal(t, 2, seo.LinksInternal)
	assert.Equal(t, 2, seo.LinksExternal)
	assert.Contains(t, seo.ExternalDomains, "external.com")
	assert.Contains(t, seo.ExternalDomains, "partner.com")

	// Images
	assert.Equal(t, 3, seo.ImagesTotal)
	assert.Equal(t, 2, seo.ImagesInternal)
	assert.Equal(t, 1, seo.ImagesExternal)

	// Hreflang
	assert.Len(t, seo.Hreflang, 2)
	assert.Equal(t, "en", seo.Hreflang[0].Lang)
	assert.Equal(t, "de", seo.Hreflang[1].Lang)

	// Structured data
	assert.Contains(t, seo.StructuredDataTypes, "Product")
	assert.Contains(t, seo.StructuredDataTypes, "Offer")
}

func TestExtractPageSEO_NonIndexable(t *testing.T) {
	tests := []struct {
		name           string
		html           string
		statusCode     int
		pageURL        string
		expectedStatus types.IndexStatus
	}{
		{
			name:           "non-200 status code",
			html:           `<html><head><title>404 Not Found</title></head></html>`,
			statusCode:     404,
			pageURL:        "https://example.com/missing",
			expectedStatus: types.IndexStatusNon200,
		},
		{
			name:           "blocked by meta robots",
			html:           `<html><head><meta name="robots" content="noindex"></head></html>`,
			statusCode:     200,
			pageURL:        "https://example.com/blocked",
			expectedStatus: types.IndexStatusBlockedByMeta,
		},
		{
			name:           "non-canonical",
			html:           `<html><head><link rel="canonical" href="https://example.com/other"></head></html>`,
			statusCode:     200,
			pageURL:        "https://example.com/this-page",
			expectedStatus: types.IndexStatusNonCanonical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseWithDOM([]byte(tt.html))
			assert.NoError(t, err)
			seo := doc.ExtractPageSEO(tt.statusCode, tt.pageURL)
			assert.Equal(t, tt.expectedStatus, seo.IndexStatus)
		})
	}
}

func TestExtractPageSEO_EmptyDocument(t *testing.T) {
	doc, err := ParseWithDOM([]byte(`<html></html>`))
	assert.NoError(t, err)

	seo := doc.ExtractPageSEO(200, "https://example.com/")

	assert.Equal(t, "", seo.Title)
	assert.Equal(t, types.IndexStatusIndexable, seo.IndexStatus)
	assert.Equal(t, "", seo.MetaDescription)
	assert.Nil(t, seo.H1s)
	assert.Equal(t, 0, seo.LinksTotal)
	assert.Equal(t, 0, seo.ImagesTotal)
}
