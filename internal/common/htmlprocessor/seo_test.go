package htmlprocessor

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func parseGoQueryDoc(t *testing.T, htmlStr string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	return doc
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
			doc := parseGoQueryDoc(t, tt.html)
			result := extractSEOTitle(doc)
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
			doc := parseGoQueryDoc(t, tt.html)
			result := extractMetaDescription(doc)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractMetaRobots(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected []string
	}{
		{
			name:     "robots tag",
			html:     `<html><head><meta name="robots" content="noindex, nofollow"></head></html>`,
			expected: []string{"noindex", "nofollow"},
		},
		{
			name:     "googlebot tag",
			html:     `<html><head><meta name="googlebot" content="noindex"></head></html>`,
			expected: []string{"noindex"},
		},
		{
			name:     "googlebot takes precedence",
			html:     `<html><head><meta name="robots" content="index"><meta name="googlebot" content="noindex"></head></html>`,
			expected: []string{"noindex"},
		},
		{
			name:     "robots used when googlebot empty",
			html:     `<html><head><meta name="googlebot" content=""><meta name="robots" content="nofollow"></head></html>`,
			expected: []string{"nofollow"},
		},
		{
			name:     "no robots or googlebot",
			html:     `<html><head><meta name="description" content="test"></head></html>`,
			expected: nil,
		},
		{
			name:     "case insensitive",
			html:     `<html><head><meta name="ROBOTS" content="noindex"></head></html>`,
			expected: []string{"noindex"},
		},
		{
			name:     "whitespace trimmed",
			html:     `<html><head><meta name="robots" content="  noindex  "></head></html>`,
			expected: []string{"noindex"},
		},
		{
			name:     "robots in body ignored",
			html:     `<html><head></head><body><meta name="robots" content="noindex"></body></html>`,
			expected: nil,
		},
		{
			name:     "parametric directives",
			html:     `<html><head><meta name="robots" content="index, follow, max-snippet:-1"></head></html>`,
			expected: []string{"index", "follow", "max-snippet:-1"},
		},
		{
			name:     "only commas and spaces",
			html:     `<html><head><meta name="robots" content="  ,  , "></head></html>`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseGoQueryDoc(t, tt.html)
			result := extractMetaRobots(doc)
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
			doc := parseGoQueryDoc(t, tt.html)
			result := extractBaseHref(doc)
			assert.Equal(t, tt.expected, result)
		})
	}
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
			doc := parseGoQueryDoc(t, tt.html)
			result := extractHeadings(doc, tt.tag, tt.maxCount)
			assert.Equal(t, tt.expected, result)
		})
	}

	t.Run("no body returns nil", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head></head></html>`)
		result := extractHeadings(doc, "h1", 5)
		assert.Nil(t, result)
	})
}

func TestExtractLinkMetrics(t *testing.T) {
	tests := []struct {
		name                   string
		html                   string
		pageURL                string
		expectTotal            int
		expectInternal         int
		expectExternal         int
		expectNofollow         int
		expectNofollowInternal int
		expectNofollowExternal int
		expectDomains          map[string]int
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
		{
			name:                   "nofollow on internal link",
			html:                   `<html><body><a href="https://example.com/page" rel="nofollow">Link</a></body></html>`,
			pageURL:                "https://example.com/",
			expectTotal:            1,
			expectInternal:         1,
			expectExternal:         0,
			expectNofollow:         1,
			expectNofollowInternal: 1,
			expectNofollowExternal: 0,
		},
		{
			name:                   "nofollow on external link",
			html:                   `<html><body><a href="https://other.com/page" rel="nofollow">Link</a></body></html>`,
			pageURL:                "https://example.com/",
			expectTotal:            1,
			expectInternal:         0,
			expectExternal:         1,
			expectNofollow:         1,
			expectNofollowInternal: 0,
			expectNofollowExternal: 1,
			expectDomains:          map[string]int{"other.com": 1},
		},
		{
			name:                   "mixed nofollow internal and external",
			html:                   `<html><body><a href="https://example.com/a" rel="nofollow">Int</a><a href="https://other.com/b" rel="nofollow">Ext</a><a href="https://example.com/c">Normal</a></body></html>`,
			pageURL:                "https://example.com/",
			expectTotal:            3,
			expectInternal:         2,
			expectExternal:         1,
			expectNofollow:         2,
			expectNofollowInternal: 1,
			expectNofollowExternal: 1,
			expectDomains:          map[string]int{"other.com": 1},
		},
		{
			name:                   "nofollow in multi-value rel attribute",
			html:                   `<html><body><a href="https://other.com/page" rel="external nofollow">Link</a></body></html>`,
			pageURL:                "https://example.com/",
			expectTotal:            1,
			expectInternal:         0,
			expectExternal:         1,
			expectNofollow:         1,
			expectNofollowInternal: 0,
			expectNofollowExternal: 1,
			expectDomains:          map[string]int{"other.com": 1},
		},
		{
			name:                   "nofollow among many rel values",
			html:                   `<html><body><a href="https://other.com/page" rel="noopener noreferrer nofollow">Link</a></body></html>`,
			pageURL:                "https://example.com/",
			expectTotal:            1,
			expectInternal:         0,
			expectExternal:         1,
			expectNofollow:         1,
			expectNofollowInternal: 0,
			expectNofollowExternal: 1,
			expectDomains:          map[string]int{"other.com": 1},
		},
		{
			name:                   "nofollow case insensitive",
			html:                   `<html><body><a href="https://example.com/a" rel="NOFOLLOW">Upper</a><a href="https://example.com/b" rel="Nofollow">Mixed</a></body></html>`,
			pageURL:                "https://example.com/",
			expectTotal:            2,
			expectInternal:         2,
			expectExternal:         0,
			expectNofollow:         2,
			expectNofollowInternal: 2,
			expectNofollowExternal: 0,
		},
		{
			name:                   "ugc and sponsored without nofollow are not counted",
			html:                   `<html><body><a href="https://other.com/a" rel="ugc">UGC</a><a href="https://other.com/b" rel="sponsored">Sponsored</a></body></html>`,
			pageURL:                "https://example.com/",
			expectTotal:            2,
			expectInternal:         0,
			expectExternal:         2,
			expectNofollow:         0,
			expectNofollowInternal: 0,
			expectNofollowExternal: 0,
			expectDomains:          map[string]int{"other.com": 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseGoQueryDoc(t, tt.html)
			seo := &types.PageSEO{}
			extractLinkMetrics(doc, tt.pageURL, tt.pageURL, seo)

			assert.Equal(t, tt.expectTotal, seo.LinksTotal, "LinksTotal mismatch")
			assert.Equal(t, tt.expectInternal, seo.LinksInternal, "LinksInternal mismatch")
			assert.Equal(t, tt.expectExternal, seo.LinksExternal, "LinksExternal mismatch")
			assert.Equal(t, tt.expectNofollow, seo.LinksNofollow, "LinksNofollow mismatch")
			assert.Equal(t, tt.expectNofollowInternal, seo.LinksNofollowInternal, "LinksNofollowInternal mismatch")
			assert.Equal(t, tt.expectNofollowExternal, seo.LinksNofollowExternal, "LinksNofollowExternal mismatch")
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
			doc := parseGoQueryDoc(t, tt.html)
			seo := &types.PageSEO{}
			extractImageMetrics(doc, tt.pageURL, tt.pageURL, seo)

			assert.Equal(t, tt.expectTotal, seo.ImagesTotal, "ImagesTotal mismatch")
			assert.Equal(t, tt.expectInternal, seo.ImagesInternal, "ImagesInternal mismatch")
			assert.Equal(t, tt.expectExternal, seo.ImagesExternal, "ImagesExternal mismatch")
		})
	}
}

func TestExtractImageMetrics_AltText(t *testing.T) {
	tests := []struct {
		name             string
		html             string
		expectTotal      int
		expectWithAlt    int
		expectWithoutAlt int
	}{
		{
			name:             "all images have alt",
			html:             `<html><body><img src="/a.jpg" alt="First"><img src="/b.jpg" alt="Second"><img src="/c.jpg" alt="Third"></body></html>`,
			expectTotal:      3,
			expectWithAlt:    3,
			expectWithoutAlt: 0,
		},
		{
			name:             "all images missing alt",
			html:             `<html><body><img src="/a.jpg"><img src="/b.jpg"><img src="/c.jpg"></body></html>`,
			expectTotal:      3,
			expectWithAlt:    0,
			expectWithoutAlt: 3,
		},
		{
			name:             "image with empty alt",
			html:             `<html><body><img src="/a.jpg" alt=""></body></html>`,
			expectTotal:      1,
			expectWithAlt:    0,
			expectWithoutAlt: 1,
		},
		{
			name:             "mixed alt presence",
			html:             `<html><body><img src="/a.jpg" alt="First"><img src="/b.jpg" alt="Second"><img src="/c.jpg"><img src="/d.jpg" alt=""></body></html>`,
			expectTotal:      4,
			expectWithAlt:    2,
			expectWithoutAlt: 2,
		},
		{
			name:             "skipped images not counted",
			html:             `<html><body><img src="data:image/png;base64,abc" alt="icon"><img src="" alt="missing"><img src="/real.jpg" alt="real"></body></html>`,
			expectTotal:      1,
			expectWithAlt:    1,
			expectWithoutAlt: 0,
		},
		{
			name:             "no images",
			html:             `<html><body></body></html>`,
			expectTotal:      0,
			expectWithAlt:    0,
			expectWithoutAlt: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseGoQueryDoc(t, tt.html)
			seo := &types.PageSEO{}
			extractImageMetrics(doc, "https://example.com/", "https://example.com/", seo)

			assert.Equal(t, tt.expectTotal, seo.ImagesTotal, "ImagesTotal mismatch")
			assert.Equal(t, tt.expectWithAlt, seo.ImagesWithAlt, "ImagesWithAlt mismatch")
			assert.Equal(t, tt.expectWithoutAlt, seo.ImagesWithoutAlt, "ImagesWithoutAlt mismatch")
		})
	}

	// Explicit invariant test for mixed case
	t.Run("invariant holds", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><body><img src="/a.jpg" alt="First"><img src="/b.jpg" alt="Second"><img src="/c.jpg"><img src="/d.jpg" alt=""></body></html>`)
		seo := &types.PageSEO{}
		extractImageMetrics(doc, "https://example.com/", "https://example.com/", seo)

		assert.Equal(t, seo.ImagesTotal, seo.ImagesWithAlt+seo.ImagesWithoutAlt, "invariant: ImagesWithAlt + ImagesWithoutAlt == ImagesTotal")
	})
}

func TestExtractBodyWords(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected []string
	}{
		{
			name:     "simple text",
			html:     `<body><p>hello world</p></body>`,
			expected: []string{"hello", "world"},
		},
		{
			name:     "strips nav",
			html:     `<body><nav>skip this</nav><p>keep this</p></body>`,
			expected: []string{"keep", "this"},
		},
		{
			name:     "strips all boilerplate elements",
			html:     `<body><nav>a</nav><header>b</header><footer>c</footer><aside>d</aside><form>e</form><script>f</script><style>g</style><noscript>h</noscript><p>visible</p></body>`,
			expected: []string{"visible"},
		},
		{
			name:     "nested content preserved",
			html:     `<body><div><p><span>deep text</span></p></div></body>`,
			expected: []string{"deep", "text"},
		},
		{
			name:     "nested stripped element",
			html:     `<body><nav><div><p>hidden</p></div></nav><p>visible</p></body>`,
			expected: []string{"visible"},
		},
		{
			name:     "lowercases words",
			html:     `<body>Hello World</body>`,
			expected: []string{"hello", "world"},
		},
		{
			name:     "normalizes whitespace",
			html:     "<body>  word1   word2\n\tword3  </body>",
			expected: []string{"word1", "word2", "word3"},
		},
		{
			name:     "empty body",
			html:     `<body></body>`,
			expected: nil,
		},
		{
			name:     "body with only stripped elements",
			html:     `<body><nav>text</nav><script>code</script></body>`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseGoQueryDoc(t, tt.html)
			result := extractBodyWords(doc)
			assert.Equal(t, tt.expected, result)
		})
	}

	t.Run("no body", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head></head></html>`)
		result := extractBodyWords(doc)
		assert.Nil(t, result)
	})
}

func TestLinkMetricsWithBaseTag(t *testing.T) {
	htmlStr := `<html><body><a href="page.html">Relative Link</a></body></html>`
	doc := parseGoQueryDoc(t, htmlStr)
	seo := &types.PageSEO{}

	extractLinkMetrics(doc, "https://cdn.example.com/base/", "https://example.com/", seo)

	// Link should resolve against base href, which is on cdn.example.com (internal via subdomain)
	assert.Equal(t, 1, seo.LinksTotal)
	assert.Equal(t, 1, seo.LinksInternal)
}

// TestExtractPageSEO_HonorsBaseHref: canonical/hreflang/breadcrumb resolve against
// <base href> (.../sub/), not the page URL (.../other/). Body link already did.
// page-dir (/other/) != base-dir (/sub/) is mandatory; without it the test is vacuous.
func TestExtractPageSEO_HonorsBaseHref(t *testing.T) {
	const pageURL = "https://site.com/other/page"
	html := `<html><head>
		<base href="https://site.com/sub/">
		<link rel="canonical" href="canon">
		<link rel="alternate" hreflang="en" href="https://site.com/other/page">
		<link rel="alternate" hreflang="de" href="de">
		<script type="application/ld+json">
		{"@context":"https://schema.org","@type":"BreadcrumbList","itemListElement":[
			{"@type":"ListItem","position":1,"name":"Home","item":{"@id":"crumb"}}
		]}
		</script>
	</head><body><a href="link">Body Link</a></body></html>`

	doc, err := ParseWithDOM([]byte(html))
	require.NoError(t, err)
	seo := doc.ExtractPageSEO(200, pageURL)

	assert.Equal(t, "https://site.com/sub/canon", seo.CanonicalURL)
	require.Len(t, seo.Hreflang, 2)
	assert.Equal(t, "https://site.com/other/page", seo.Hreflang[0].URL) // absolute, ignores base
	assert.Equal(t, "https://site.com/sub/de", seo.Hreflang[1].URL)     // relative, via base
	assert.Equal(t, "en", seo.HreflangSelf)                             // identity, still page URL
	require.Len(t, seo.Breadcrumbs, 1)
	assert.Equal(t, "https://site.com/sub/crumb", seo.Breadcrumbs[0].URL)
	require.Len(t, seo.PageLinks, 1)
	assert.Equal(t, "https://site.com/sub/link", seo.PageLinks[0].Target)
}

// TestExtractPageSEO_NoBaseHref: regression guard. No <base> -> every relative SEO
// URL resolves against the page (.../other/), pinned exactly.
func TestExtractPageSEO_NoBaseHref(t *testing.T) {
	const pageURL = "https://site.com/other/page"
	html := `<html><head>
		<link rel="canonical" href="canon">
		<link rel="alternate" hreflang="de" href="de">
		<script type="application/ld+json">
		{"@context":"https://schema.org","@type":"BreadcrumbList","itemListElement":[
			{"@type":"ListItem","position":1,"name":"Home","item":{"@id":"crumb"}}
		]}
		</script>
	</head><body><a href="link">Body Link</a></body></html>`

	doc, err := ParseWithDOM([]byte(html))
	require.NoError(t, err)
	seo := doc.ExtractPageSEO(200, pageURL)

	assert.Equal(t, "https://site.com/other/canon", seo.CanonicalURL)
	require.Len(t, seo.Hreflang, 1)
	assert.Equal(t, "https://site.com/other/de", seo.Hreflang[0].URL)
	require.Len(t, seo.Breadcrumbs, 1)
	assert.Equal(t, "https://site.com/other/crumb", seo.Breadcrumbs[0].URL)
	require.Len(t, seo.PageLinks, 1)
	assert.Equal(t, "https://site.com/other/link", seo.PageLinks[0].Target)
}

// TestEffectiveBaseURL: unit coverage for the helper, including the edge cases the
// old inline block silently handled (empty/whitespace href -> page URL).
func TestEffectiveBaseURL(t *testing.T) {
	const pageURL = "https://site.com/other/page"
	tests := []struct {
		name string
		tag  string // <base> tag markup, "" = no tag
		want string
	}{
		{"no base tag", "", pageURL},
		{"empty href", `<base href="">`, pageURL},
		{"whitespace href", `<base href="   ">`, pageURL},
		{"absolute base", `<base href="https://site.com/sub/">`, "https://site.com/sub/"},
		{"relative base", `<base href="/sub/">`, "https://site.com/sub/"},
		{"protocol-relative base", `<base href="//cdn.site.com/x/">`, "https://cdn.site.com/x/"},
		{"cross-origin base", `<base href="https://cdn.example.net/a/">`, "https://cdn.example.net/a/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseGoQueryDoc(t, "<html><head>"+tt.tag+"</head><body></body></html>")
			assert.Equal(t, tt.want, effectiveBaseURL(doc, pageURL))
		})
	}
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
			doc := parseGoQueryDoc(t, tt.html)
			result := extractHreflang(doc, tt.pageURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractHreflangSelf(t *testing.T) {
	tests := []struct {
		name     string
		entries  []types.HreflangEntry
		pageURL  string
		expected string
	}{
		{
			name: "self-referencing entry found",
			entries: []types.HreflangEntry{
				{Lang: "en", URL: "https://example.com/page"},
				{Lang: "de", URL: "https://example.de/seite"},
			},
			pageURL:  "https://example.com/page",
			expected: "en",
		},
		{
			name: "case-insensitive match",
			entries: []types.HreflangEntry{
				{Lang: "en", URL: "https://Example.COM/Page"},
			},
			pageURL:  "https://example.com/page",
			expected: "en",
		},
		{
			name: "no match",
			entries: []types.HreflangEntry{
				{Lang: "en", URL: "https://example.com/other"},
				{Lang: "de", URL: "https://example.de/page"},
			},
			pageURL:  "https://example.com/page",
			expected: "",
		},
		{
			name:     "empty entries",
			entries:  nil,
			pageURL:  "https://example.com/page",
			expected: "",
		},
		{
			name: "self is not first entry",
			entries: []types.HreflangEntry{
				{Lang: "de", URL: "https://example.de/seite"},
				{Lang: "en", URL: "https://example.com/page"},
				{Lang: "fr", URL: "https://example.fr/page"},
			},
			pageURL:  "https://example.com/page",
			expected: "en",
		},
		{
			name: "x-default self-referencing",
			entries: []types.HreflangEntry{
				{Lang: "x-default", URL: "https://example.com/page"},
			},
			pageURL:  "https://example.com/page",
			expected: "x-default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractHreflangSelf(tt.entries, tt.pageURL)
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
			doc := parseGoQueryDoc(t, tt.html)
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
    <link rel="alternate" hreflang="en" href="https://example.com/products/test">
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
    <nav><a href="/">Home</a><a href="/products">Products</a></nav>
    <h1>Test Product</h1>
    <h2>Features</h2>
    <h2>Reviews</h2>
    <h3>Dimensions</h3>

    <p>This is a detailed product description with enough words to generate a meaningful content fingerprint for near-duplicate detection and content quality analysis.</p>

    <a href="/other-product">Related Product</a>
    <a href="https://example.com/category">Category</a>
    <a href="https://external.com/review">External Review</a>
    <a href="https://partner.com/link">Partner</a>

    <img src="/images/product.jpg" alt="Product">
    <img src="https://cdn.example.com/images/large.jpg" alt="Large">
    <img src="https://external-cdn.com/image.png" alt="External">
    <img src="/images/icon.png">

    <footer><p>Copyright 2024 Example Corp</p></footer>
</body>
</html>`

	doc, err := ParseWithDOM([]byte(htmlContent))
	assert.NoError(t, err)

	seo := doc.ExtractPageSEO(200, "https://example.com/products/test")

	// Basic metadata
	assert.Equal(t, "Test Product - Best Deals", seo.Title)
	assert.Equal(t, types.IndexStatusIndexable, seo.IndexStatus)
	assert.Equal(t, "Find the best deals on test products.", seo.MetaDescription)
	assert.Equal(t, []string{"index", "follow"}, seo.MetaRobots)
	assert.Equal(t, "https://example.com/products/test", seo.CanonicalURL)

	// Headings
	assert.Equal(t, []string{"Test Product"}, seo.H1s)
	assert.Equal(t, []string{"Features", "Reviews"}, seo.H2s)
	assert.Equal(t, []string{"Dimensions"}, seo.H3s)

	// Links (nav links are counted by extractLinkMetrics)
	assert.Equal(t, 6, seo.LinksTotal)
	assert.Equal(t, 4, seo.LinksInternal)
	assert.Equal(t, 2, seo.LinksExternal)
	assert.Contains(t, seo.ExternalDomains, "external.com")
	assert.Contains(t, seo.ExternalDomains, "partner.com")

	// Images (3 with alt + 1 without alt)
	assert.Equal(t, 4, seo.ImagesTotal)
	assert.Equal(t, 3, seo.ImagesInternal)
	assert.Equal(t, 1, seo.ImagesExternal)
	assert.Equal(t, 3, seo.ImagesWithAlt)
	assert.Equal(t, 1, seo.ImagesWithoutAlt)
	assert.Equal(t, seo.ImagesTotal, seo.ImagesWithAlt+seo.ImagesWithoutAlt)

	// Word count (body text minus nav/footer boilerplate)
	assert.Equal(t, 33, seo.WordCount)

	// Hreflang
	assert.Len(t, seo.Hreflang, 2)
	assert.Equal(t, "en", seo.Hreflang[0].Lang)
	assert.Equal(t, "de", seo.Hreflang[1].Lang)

	// HreflangSelf (en entry URL matches pageURL)
	assert.Equal(t, "en", seo.HreflangSelf)

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

func TestExtractSEOTitle_GoQuery(t *testing.T) {
	t.Run("basic title", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head><title>My SEO Title</title></head></html>`)
		result := extractSEOTitle(doc)
		assert.Equal(t, "My SEO Title", result)
	})

	t.Run("title exceeding 500 runes truncated", func(t *testing.T) {
		longTitle := strings.Repeat("x", 600)
		doc := parseGoQueryDoc(t, `<html><head><title>`+longTitle+`</title></head></html>`)
		result := extractSEOTitle(doc)
		assert.Equal(t, strings.Repeat("x", 500), result)
	})

	t.Run("no title returns empty", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head></head></html>`)
		result := extractSEOTitle(doc)
		assert.Equal(t, "", result)
	})
}

func TestExtractMetaDescription_GoQuery(t *testing.T) {
	t.Run("basic description", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head><meta name="description" content="A description"></head></html>`)
		result := extractMetaDescription(doc)
		assert.Equal(t, "A description", result)
	})

	t.Run("empty content returns empty", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head><meta name="description" content=""></head></html>`)
		result := extractMetaDescription(doc)
		assert.Equal(t, "", result)
	})

	t.Run("content exceeding 1000 chars truncated", func(t *testing.T) {
		longDesc := strings.Repeat("d", 1100)
		doc := parseGoQueryDoc(t, `<html><head><meta name="description" content="`+longDesc+`"></head></html>`)
		result := extractMetaDescription(doc)
		assert.Equal(t, strings.Repeat("d", 1000), result)
	})

	t.Run("no description meta returns empty", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head><meta name="keywords" content="test"></head></html>`)
		result := extractMetaDescription(doc)
		assert.Equal(t, "", result)
	})
}

func TestExtractCanonicalURL_GoQuery(t *testing.T) {
	t.Run("basic canonical", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head><link rel="canonical" href="https://example.com/page"></head></html>`)
		result := extractCanonicalURL(doc)
		assert.Equal(t, "https://example.com/page", result)
	})

	t.Run("no canonical returns empty", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head></head></html>`)
		result := extractCanonicalURL(doc)
		assert.Equal(t, "", result)
	})

	t.Run("whitespace in href trimmed", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head><link rel="canonical" href="  https://example.com/page  "></head></html>`)
		result := extractCanonicalURL(doc)
		assert.Equal(t, "https://example.com/page", result)
	})
}

func TestExtractHeadings_GoQuery(t *testing.T) {
	t.Run("multiple h1s limited by maxCount", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><body><h1>One</h1><h1>Two</h1><h1>Three</h1><h1>Four</h1></body></html>`)
		result := extractHeadings(doc, "h1", 2)
		assert.Equal(t, []string{"One", "Two"}, result)
	})

	t.Run("empty heading skipped", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><body><h1>   </h1><h1>Real</h1></body></html>`)
		result := extractHeadings(doc, "h1", 5)
		assert.Equal(t, []string{"Real"}, result)
	})

	t.Run("no body returns nil", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head></head></html>`)
		result := extractHeadings(doc, "h1", 5)
		assert.Nil(t, result)
	})
}

func TestExtractBodyWords_GoQuery(t *testing.T) {
	t.Run("strips nav content", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><body><p>Hello World</p><nav>Skip This</nav></body></html>`)
		result := extractBodyWords(doc)
		assert.Contains(t, result, "hello")
		assert.Contains(t, result, "world")
		assert.NotContains(t, result, "skip")
		assert.NotContains(t, result, "this")
	})

	t.Run("empty body returns nil", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><body></body></html>`)
		result := extractBodyWords(doc)
		assert.Nil(t, result)
	})
}

func TestExtractMetaRobots_GoQuery(t *testing.T) {
	t.Run("googlebot takes precedence over robots", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head><meta name="robots" content="noindex"><meta name="googlebot" content="index, follow"></head></html>`)
		result := extractMetaRobots(doc)
		assert.Equal(t, []string{"index", "follow"}, result)
	})

	t.Run("robots used when no googlebot", func(t *testing.T) {
		doc := parseGoQueryDoc(t, `<html><head><meta name="robots" content="noindex, nofollow"></head></html>`)
		result := extractMetaRobots(doc)
		assert.Equal(t, []string{"noindex", "nofollow"}, result)
	})
}

func TestExtractLinkMetrics_GoQuery(t *testing.T) {
	htmlStr := `<html><body>
		<a href="/page1">Internal 1</a>
		<a href="/page2">Internal 2</a>
		<a href="https://other.com/a">External 1</a>
		<a href="https://other.com/b">External 2</a>
		<a href="https://spam.com" rel="nofollow">Spam</a>
		<a href="javascript:void(0)">JS Link</a>
	</body></html>`

	doc := parseGoQueryDoc(t, htmlStr)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	assert.Equal(t, 5, seo.LinksTotal)
	assert.Equal(t, 2, seo.LinksInternal)
	assert.Equal(t, 3, seo.LinksExternal)
	assert.Equal(t, 1, seo.LinksNofollow)
	assert.Equal(t, 0, seo.LinksNofollowInternal)
	assert.Equal(t, 1, seo.LinksNofollowExternal)
	assert.Equal(t, map[string]int{"other.com": 2, "spam.com": 1}, seo.ExternalDomains)
}

func TestExtractImageMetrics_GoQuery(t *testing.T) {
	htmlStr := `<html><body>
		<img src="/img.png" alt="photo">
		<img src="https://other-cdn.com/pic.jpg">
		<img src="data:image/png;base64,abc">
	</body></html>`

	doc := parseGoQueryDoc(t, htmlStr)
	seo := &types.PageSEO{}
	extractImageMetrics(doc, "https://example.com/", "https://example.com/", seo)

	assert.Equal(t, 2, seo.ImagesTotal)
	assert.Equal(t, 1, seo.ImagesInternal)
	assert.Equal(t, 1, seo.ImagesExternal)
	assert.Equal(t, 1, seo.ImagesWithAlt)
	assert.Equal(t, 1, seo.ImagesWithoutAlt)
}

func TestExtractHreflang_GoQuery(t *testing.T) {
	htmlStr := `<html><head>
		<link rel="alternate" hreflang="en" href="https://example.com/en">
		<link rel="alternate" hreflang="fr" href="https://example.com/fr">
		<link rel="alternate" href="https://example.com/rss">
	</head></html>`

	doc := parseGoQueryDoc(t, htmlStr)
	result := extractHreflang(doc, "https://example.com/en")

	assert.Len(t, result, 2)
	assert.Equal(t, "en", result[0].Lang)
	assert.Equal(t, "https://example.com/en", result[0].URL)
	assert.Equal(t, "fr", result[1].Lang)
	assert.Equal(t, "https://example.com/fr", result[1].URL)

	selfLang := extractHreflangSelf(result, "https://example.com/en")
	assert.Equal(t, "en", selfLang)
}

func TestExtractStructuredDataTypes_GoQuery(t *testing.T) {
	htmlStr := `<html><head>
		<script type="application/ld+json">{"@type":"Article","name":"Test"}</script>
		<script type="application/ld+json">{"@graph":[{"@type":"WebPage"},{"@type":"Article"}]}</script>
	</head></html>`

	doc := parseGoQueryDoc(t, htmlStr)
	result := extractStructuredDataTypes(doc)

	assert.Equal(t, []string{"Article", "WebPage"}, result)
}

func TestExtractPageSEO_FullIntegration(t *testing.T) {
	htmlContent := `<!DOCTYPE html>
<html>
<head>
    <title>Integration Test Page</title>
    <meta name="description" content="A full integration test page.">
    <meta name="robots" content="index, follow">
    <link rel="canonical" href="https://example.com/">
    <link rel="alternate" hreflang="en" href="https://example.com/">
    <link rel="alternate" hreflang="es" href="https://example.com/es">
    <script type="application/ld+json">{"@type":"WebPage","name":"Test"}</script>
    <script type="application/ld+json">{"@type":"Organization","name":"Example"}</script>
</head>
<body>
    <h1>Welcome to Integration Test</h1>
    <h2>Section One</h2>
    <h2>Section Two</h2>
    <h3>Subsection A</h3>

    <p>This is the main content area with some words for counting purposes in the test.</p>

    <a href="/about">About</a>
    <a href="/contact">Contact</a>
    <a href="https://external.com/page">External Link</a>
    <a href="https://partner.org/info" rel="nofollow">Partner</a>

    <img src="/logo.png" alt="Logo">
    <img src="https://cdn.example.com/banner.jpg" alt="Banner">
    <img src="https://other-cdn.com/photo.png">
</body>
</html>`

	doc, err := ParseWithDOM([]byte(htmlContent))
	assert.NoError(t, err)

	seo := doc.ExtractPageSEO(200, "https://example.com/")

	assert.Equal(t, "Integration Test Page", seo.Title)
	assert.Equal(t, types.IndexStatusIndexable, seo.IndexStatus)
	assert.Equal(t, "A full integration test page.", seo.MetaDescription)
	assert.Equal(t, []string{"index", "follow"}, seo.MetaRobots)
	assert.Equal(t, "https://example.com/", seo.CanonicalURL)

	assert.Equal(t, []string{"Welcome to Integration Test"}, seo.H1s)
	assert.Equal(t, []string{"Section One", "Section Two"}, seo.H2s)
	assert.Equal(t, []string{"Subsection A"}, seo.H3s)

	assert.Equal(t, 4, seo.LinksTotal)
	assert.Equal(t, 2, seo.LinksInternal)
	assert.Equal(t, 2, seo.LinksExternal)
	assert.Equal(t, 1, seo.LinksNofollow)
	assert.Equal(t, 0, seo.LinksNofollowInternal)
	assert.Equal(t, 1, seo.LinksNofollowExternal)
	assert.Contains(t, seo.ExternalDomains, "external.com")
	assert.Contains(t, seo.ExternalDomains, "partner.org")

	assert.Equal(t, 3, seo.ImagesTotal)
	assert.Equal(t, 2, seo.ImagesInternal)
	assert.Equal(t, 1, seo.ImagesExternal)
	assert.Equal(t, 2, seo.ImagesWithAlt)
	assert.Equal(t, 1, seo.ImagesWithoutAlt)

	assert.True(t, seo.WordCount > 0)

	assert.Len(t, seo.Hreflang, 2)
	assert.Equal(t, "en", seo.Hreflang[0].Lang)
	assert.Equal(t, "https://example.com/", seo.Hreflang[0].URL)
	assert.Equal(t, "es", seo.Hreflang[1].Lang)
	assert.Equal(t, "https://example.com/es", seo.Hreflang[1].URL)
	assert.Equal(t, "en", seo.HreflangSelf)

	assert.Equal(t, []string{"Organization", "WebPage"}, seo.StructuredDataTypes)
}

func wrapBreadcrumbScript(jsonBody string) string {
	return `<html><head><script type="application/ld+json">` + jsonBody + `</script></head></html>`
}

func TestExtractBreadcrumbs_HappyPath(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		pageURL  string
		expected []types.BreadcrumbEntry
	}{
		{
			name: "google simple form (name on ListItem, item as string)",
			html: wrapBreadcrumbScript(`{
                "@type":"BreadcrumbList",
                "itemListElement":[
                    {"@type":"ListItem","position":1,"name":"Home","item":"https://e.com/"},
                    {"@type":"ListItem","position":2,"name":"Guides","item":"https://e.com/guides"},
                    {"@type":"ListItem","position":3,"name":"Web Perf","item":"https://e.com/guides/web"}
                ]
            }`),
			pageURL: "https://e.com/guides/web",
			expected: []types.BreadcrumbEntry{
				{Name: "Home", URL: "https://e.com/"},
				{Name: "Guides", URL: "https://e.com/guides"},
				{Name: "Web Perf", URL: "https://e.com/guides/web"},
			},
		},
		{
			name: "google full form (item is object with @id and name)",
			html: wrapBreadcrumbScript(`{
                "@type":"BreadcrumbList",
                "itemListElement":[
                    {"@type":"ListItem","position":1,"item":{"@id":"https://e.com/","name":"Home"}},
                    {"@type":"ListItem","position":2,"item":{"@id":"https://e.com/guides","name":"Guides"}}
                ]
            }`),
			pageURL: "https://e.com/",
			expected: []types.BreadcrumbEntry{
				{Name: "Home", URL: "https://e.com/"},
				{Name: "Guides", URL: "https://e.com/guides"},
			},
		},
		{
			name: "hybrid form (name on ListItem, item is object with @id only)",
			html: wrapBreadcrumbScript(`{
                "@type":"BreadcrumbList",
                "itemListElement":[
                    {"position":1,"name":"Home","item":{"@id":"https://e.com/"}},
                    {"position":2,"name":"Guides","item":{"@id":"https://e.com/guides"}}
                ]
            }`),
			pageURL: "https://e.com/",
			expected: []types.BreadcrumbEntry{
				{Name: "Home", URL: "https://e.com/"},
				{Name: "Guides", URL: "https://e.com/guides"},
			},
		},
		{
			name: "legacy item.url",
			html: wrapBreadcrumbScript(`{
                "@type":"BreadcrumbList",
                "itemListElement":[
                    {"position":1,"item":{"url":"https://e.com/","name":"Home"}},
                    {"position":2,"item":{"url":"https://e.com/guides","name":"Guides"}}
                ]
            }`),
			pageURL: "https://e.com/",
			expected: []types.BreadcrumbEntry{
				{Name: "Home", URL: "https://e.com/"},
				{Name: "Guides", URL: "https://e.com/guides"},
			},
		},
		{
			name: "item.identifier fallback",
			html: wrapBreadcrumbScript(`{
                "@type":"BreadcrumbList",
                "itemListElement":[
                    {"position":1,"name":"Home","item":{"identifier":"https://e.com/"}}
                ]
            }`),
			pageURL: "https://e.com/",
			expected: []types.BreadcrumbEntry{
				{Name: "Home", URL: "https://e.com/"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseGoQueryDoc(t, tt.html)
			result := extractBreadcrumbs(doc, tt.pageURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractBreadcrumbs_TrailSelection(t *testing.T) {
	t.Run("two BreadcrumbLists in separate script tags - first wins", func(t *testing.T) {
		htmlStr := `<html><head>
            <script type="application/ld+json">{
                "@type":"BreadcrumbList",
                "itemListElement":[{"position":1,"name":"A","item":"https://e.com/a"}]
            }</script>
            <script type="application/ld+json">{
                "@type":"BreadcrumbList",
                "itemListElement":[{"position":1,"name":"B","item":"https://e.com/b"}]
            }</script>
        </head></html>`
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{{Name: "A", URL: "https://e.com/a"}}, result)
	})

	t.Run("two BreadcrumbLists in one array - first array element wins", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`[
            {"@type":"BreadcrumbList","itemListElement":[{"position":1,"name":"First","item":"https://e.com/1"}]},
            {"@type":"BreadcrumbList","itemListElement":[{"position":1,"name":"Second","item":"https://e.com/2"}]}
        ]`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{{Name: "First", URL: "https://e.com/1"}}, result)
	})

	t.Run("BreadcrumbList inside @graph", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@graph":[
                {"@type":"WebSite","name":"x"},
                {"@type":"BreadcrumbList","itemListElement":[
                    {"position":1,"name":"Home","item":"https://e.com/"}
                ]}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{{Name: "Home", URL: "https://e.com/"}}, result)
	})

	t.Run("@type as array containing BreadcrumbList", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":["BreadcrumbList","ItemList"],
            "itemListElement":[{"position":1,"name":"Home","item":"https://e.com/"}]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{{Name: "Home", URL: "https://e.com/"}}, result)
	})
}

func TestExtractBreadcrumbs_OrderingAndDropping(t *testing.T) {
	t.Run("position out of declaration order", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":2,"name":"Guides","item":"https://e.com/guides"},
                {"position":1,"name":"Home","item":"https://e.com/"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{
			{Name: "Home", URL: "https://e.com/"},
			{Name: "Guides", URL: "https://e.com/guides"},
		}, result)
	})

	t.Run("missing positions appended after positioned", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":2,"name":"Guides","item":"https://e.com/guides"},
                {"position":1,"name":"Home","item":"https://e.com/"},
                {"name":"Orphan","item":"https://e.com/orphan"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{
			{Name: "Home", URL: "https://e.com/"},
			{Name: "Guides", URL: "https://e.com/guides"},
			{Name: "Orphan", URL: "https://e.com/orphan"},
		}, result)
	})

	t.Run("sparse positions 1, 2, 5", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"A","item":"https://e.com/a"},
                {"position":2,"name":"B","item":"https://e.com/b"},
                {"position":5,"name":"C","item":"https://e.com/c"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Len(t, result, 3)
		assert.Equal(t, "A", result[0].Name)
		assert.Equal(t, "B", result[1].Name)
		assert.Equal(t, "C", result[2].Name)
	})

	t.Run("position as numeric string", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":"2","name":"Second","item":"https://e.com/2"},
                {"position":"1","name":"First","item":"https://e.com/1"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, "First", result[0].Name)
		assert.Equal(t, "Second", result[1].Name)
	})

	t.Run("last item without URL is dropped", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"Home","item":"https://e.com/"},
                {"position":2,"name":"Guides","item":"https://e.com/guides"},
                {"position":3,"name":"Current"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/guides/current")
		assert.Equal(t, []types.BreadcrumbEntry{
			{Name: "Home", URL: "https://e.com/"},
			{Name: "Guides", URL: "https://e.com/guides"},
		}, result)
	})

	t.Run("mid-item without URL is dropped, deeper items shift up", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"Home","item":"https://e.com/"},
                {"position":2,"name":"NoURL"},
                {"position":3,"name":"Guides","item":"https://e.com/guides"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{
			{Name: "Home", URL: "https://e.com/"},
			{Name: "Guides", URL: "https://e.com/guides"},
		}, result)
	})

	t.Run("javascript URL dropped", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"Home","item":"https://e.com/"},
                {"position":2,"name":"Skip","item":"javascript:void(0)"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{{Name: "Home", URL: "https://e.com/"}}, result)
	})

	t.Run("mailto URL dropped", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"Home","item":"https://e.com/"},
                {"position":2,"name":"Email","item":"mailto:foo@example.com"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{{Name: "Home", URL: "https://e.com/"}}, result)
	})

	t.Run("fragment-only URL dropped", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"Home","item":"https://e.com/"},
                {"position":2,"name":"Frag","item":"#section"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{{Name: "Home", URL: "https://e.com/"}}, result)
	})

	t.Run("all items dropped returns nil slice (not empty)", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"No URL"},
                {"position":2,"name":"Also no URL"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Nil(t, result, "must be nil (not empty slice) for omitempty to fire")
	})
}

func TestExtractBreadcrumbs_Normalization(t *testing.T) {
	t.Run("relative URL resolved against pageURL", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"Home","item":"/"},
                {"position":2,"name":"Guides","item":"/guides"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/guides/current")
		assert.Equal(t, []types.BreadcrumbEntry{
			{Name: "Home", URL: "https://e.com/"},
			{Name: "Guides", URL: "https://e.com/guides"},
		}, result)
	})

	t.Run("more than 5 items - first 5 kept (root-side)", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"Home","item":"https://e.com/1"},
                {"position":2,"name":"Cat","item":"https://e.com/2"},
                {"position":3,"name":"Sub","item":"https://e.com/3"},
                {"position":4,"name":"Group","item":"https://e.com/4"},
                {"position":5,"name":"Variant","item":"https://e.com/5"},
                {"position":6,"name":"Detail","item":"https://e.com/6"},
                {"position":7,"name":"Extra","item":"https://e.com/7"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Len(t, result, 5)
		assert.Equal(t, "Home", result[0].Name)
		assert.Equal(t, "Variant", result[4].Name)
	})

	t.Run("whitespace and long name normalized", func(t *testing.T) {
		longName := strings.Repeat("a", 600)
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"  Home  \n Page  ","item":"https://e.com/"},
                {"position":2,"name":"` + longName + `","item":"https://e.com/2"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, "Home Page", result[0].Name)
		assert.Equal(t, strings.Repeat("a", 500), result[1].Name)
	})

	t.Run("long URL truncated", func(t *testing.T) {
		longTail := strings.Repeat("a", 2100)
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"Home","item":"https://e.com/` + longTail + `"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Len(t, result, 1)
		assert.Equal(t, 2000, utf8.RuneCountInString(result[0].URL))
	})
}

func TestExtractBreadcrumbs_Robustness(t *testing.T) {
	cases := []struct {
		name string
		html string
	}{
		{"malformed JSON", wrapBreadcrumbScript(`{invalid json}`)},
		{"truncated JSON", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":[{"name":"H`)},
		{"binary garbage", wrapBreadcrumbScript("\x00\x01\xff\xfe\x7fbinarygarbage\x00\x01")},
		{"BOM before JSON", wrapBreadcrumbScript("\ufeff{\"@type\":\"BreadcrumbList\"}")},
		{"itemListElement missing", wrapBreadcrumbScript(`{"@type":"BreadcrumbList"}`)},
		{"itemListElement as object", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":{"a":1}}`)},
		{"itemListElement as string", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":"oops"}`)},
		{"itemListElement as number", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":42}`)},
		{"itemListElement as null", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":null}`)},
		{"itemListElement empty array", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":[]}`)},
		{"@type as number", wrapBreadcrumbScript(`{"@type":42,"itemListElement":[{"name":"x","item":"/"}]}`)},
		{"@type as null", wrapBreadcrumbScript(`{"@type":null,"itemListElement":[{"name":"x","item":"/"}]}`)},
		{"position as object", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":[{"position":{"@value":1},"name":"Home","item":"https://e.com/"}]}`)},
		{"position as bool", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":[{"position":true,"name":"Home","item":"https://e.com/"}]}`)},
		{"position negative", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":[{"position":-1,"name":"Home","item":"https://e.com/"}]}`)},
		{"position fractional", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":[{"position":1.5,"name":"Home","item":"https://e.com/"}]}`)},
		{"position e300", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":[{"position":1e300,"name":"Home","item":"https://e.com/"}]}`)},
		{"position abc string", wrapBreadcrumbScript(`{"@type":"BreadcrumbList","itemListElement":[{"position":"abc","name":"Home","item":"https://e.com/"}]}`)},
		{"empty document", `<html></html>`},
		{"no JSON-LD scripts", `<html><head><script>console.log("x")</script></head></html>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := parseGoQueryDoc(t, tc.html)
			result := extractBreadcrumbs(doc, "https://e.com/")
			for _, e := range result {
				assert.NotEmpty(t, e.URL, "any returned entry must have non-empty URL")
			}
		})
	}
}

func TestExtractBreadcrumbs_RobustnessExtras(t *testing.T) {
	t.Run("items in itemListElement that are non-objects are skipped", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                null,
                "string-item",
                42,
                {"position":1,"name":"Home","item":"https://e.com/"}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{{Name: "Home", URL: "https://e.com/"}}, result)
	})

	t.Run("name with wrong types falls back to item.name", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":42,"item":{"@id":"https://e.com/","name":"Home"}}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Equal(t, []types.BreadcrumbEntry{{Name: "Home", URL: "https://e.com/"}}, result)
	})

	t.Run("item with wrong types is skipped through fallback chain", func(t *testing.T) {
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[
                {"position":1,"name":"Home","item":42}
            ]
        }`)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Nil(t, result)
	})

	t.Run("script body exceeding MaxJSONLDSize is skipped", func(t *testing.T) {
		padding := strings.Repeat(" ", types.MaxJSONLDSize)
		htmlStr := wrapBreadcrumbScript(`{
            "@type":"BreadcrumbList",
            "itemListElement":[{"position":1,"name":"Home","item":"https://e.com/"}]
        }` + padding)
		result := extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		assert.Nil(t, result)
	})

	t.Run("deeply nested @graph past recursion depth", func(t *testing.T) {
		var b strings.Builder
		closes := 0
		for i := 0; i < types.MaxJSONLDRecursionDepth+5; i++ {
			b.WriteString(`{"@graph":`)
			closes++
		}
		b.WriteString(`{"@type":"BreadcrumbList","itemListElement":[{"position":1,"name":"Home","item":"https://e.com/"}]}`)
		for i := 0; i < closes; i++ {
			b.WriteString(`}`)
		}
		htmlStr := wrapBreadcrumbScript(b.String())
		assert.NotPanics(t, func() {
			_ = extractBreadcrumbs(parseGoQueryDoc(t, htmlStr), "https://e.com/")
		})
	})
}

func FuzzExtractBreadcrumbs(f *testing.F) {
	f.Add([]byte(`{"@type":"BreadcrumbList","itemListElement":[{"position":1,"name":"H","item":"/"}]}`))
	f.Add([]byte(`[{"@type":"BreadcrumbList"}]`))
	f.Add([]byte(`{"@graph":[{"@type":"BreadcrumbList"}]}`))
	f.Add([]byte(`{"@type":"BreadcrumbList","itemListElement":null}`))
	f.Add([]byte(`{invalid`))
	f.Add([]byte("\x00\x01\xff"))
	f.Fuzz(func(t *testing.T, body []byte) {
		html := `<html><head><script type="application/ld+json">` + string(body) + `</script></head></html>`
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			return
		}
		_ = extractBreadcrumbs(doc, "https://example.com/")
	})
}

func TestExtractPageSEO_BreadcrumbsIntegration(t *testing.T) {
	htmlContent := `<!DOCTYPE html>
<html>
<head>
    <title>Test</title>
    <script type="application/ld+json">{
        "@type":"BreadcrumbList",
        "itemListElement":[
            {"@type":"ListItem","position":1,"name":"Home","item":"https://example.com/"},
            {"@type":"ListItem","position":2,"name":"Guides","item":"https://example.com/guides"},
            {"@type":"ListItem","position":3,"name":"Current"}
        ]
    }</script>
</head>
<body><h1>Test</h1></body>
</html>`

	doc, err := ParseWithDOM([]byte(htmlContent))
	assert.NoError(t, err)
	seo := doc.ExtractPageSEO(200, "https://example.com/guides/current")

	assert.Equal(t, []types.BreadcrumbEntry{
		{Name: "Home", URL: "https://example.com/"},
		{Name: "Guides", URL: "https://example.com/guides"},
	}, seo.Breadcrumbs)
}
