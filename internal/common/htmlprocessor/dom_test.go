package htmlprocessor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func parseHTML(t *testing.T, htmlStr string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(htmlStr))
	require.NoError(t, err)
	return doc
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "tests", "fixtures", "htmlprocessor", name)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to load fixture: %s", name)
	return data
}

func TestFindElement(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		tag      string
		wantNil  bool
		wantData string
	}{
		{
			name:     "finds nested element",
			html:     `<html><body><div><span id="target">text</span></div></body></html>`,
			tag:      "span",
			wantData: "span",
		},
		{
			name:    "returns nil for missing element",
			html:    `<html><body><div>text</div></body></html>`,
			tag:     "span",
			wantNil: true,
		},
		{
			name:     "case insensitive search",
			html:     `<html><body><DIV>text</DIV></body></html>`,
			tag:      "div",
			wantData: "div",
		},
		{
			name:     "finds first match",
			html:     `<html><body><div id="first"></div><div id="second"></div></body></html>`,
			tag:      "div",
			wantData: "div",
		},
		{
			name:     "finds deeply nested element",
			html:     `<html><body><div><section><article><p>text</p></article></section></div></body></html>`,
			tag:      "p",
			wantData: "p",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseHTML(t, tt.html)
			result := findElement(doc, tt.tag)

			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.wantData, result.Data)
			}
		})
	}
}

func TestFindElement_NilNode(t *testing.T) {
	result := findElement(nil, "div")
	assert.Nil(t, result)
}

func TestFindElementInParent(t *testing.T) {
	htmlStr := `<html><head><title>Test</title></head><body><title>Body Title</title></body></html>`
	doc := parseHTML(t, htmlStr)

	head := findElement(doc, "head")
	require.NotNil(t, head)

	title := findElementInParent(head, "title")
	require.NotNil(t, title)
	assert.Equal(t, "Test", getTextContent(title))
}

func TestFindElementInParent_NilParent(t *testing.T) {
	result := findElementInParent(nil, "div")
	assert.Nil(t, result)
}

func TestFindAllElementsInParent(t *testing.T) {
	htmlStr := `<html><head><meta name="robots"><meta name="googlebot"><meta name="description"></head><body></body></html>`
	doc := parseHTML(t, htmlStr)

	head := findElement(doc, "head")
	require.NotNil(t, head)

	metas := findAllElementsInParent(head, "meta")
	assert.Len(t, metas, 3)
}

func TestFindAllElementsInParent_NilParent(t *testing.T) {
	result := findAllElementsInParent(nil, "div")
	assert.Nil(t, result)
}

func TestFindAllElementsInParent_NoMatch(t *testing.T) {
	htmlStr := `<html><head><title>Test</title></head></html>`
	doc := parseHTML(t, htmlStr)

	head := findElement(doc, "head")
	require.NotNil(t, head)

	result := findAllElementsInParent(head, "meta")
	assert.Empty(t, result)
}

func TestGetAttr(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		attrName string
		want     string
	}{
		{
			name:     "gets attribute value",
			html:     `<html><body><div id="test-id">text</div></body></html>`,
			attrName: "id",
			want:     "test-id",
		},
		{
			name:     "case insensitive attribute name",
			html:     `<html><body><div ID="test-id">text</div></body></html>`,
			attrName: "id",
			want:     "test-id",
		},
		{
			name:     "returns empty for missing attribute",
			html:     `<html><body><div>text</div></body></html>`,
			attrName: "id",
			want:     "",
		},
		{
			name:     "handles content attribute",
			html:     `<html><head><meta name="robots" content="noindex"></head></html>`,
			attrName: "content",
			want:     "noindex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseHTML(t, tt.html)
			var target *html.Node
			if tt.attrName == "content" {
				target = findElement(doc, "meta")
			} else {
				target = findElement(doc, "div")
			}
			require.NotNil(t, target)

			result := getAttr(target, tt.attrName)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestGetAttr_NilNode(t *testing.T) {
	result := getAttr(nil, "id")
	assert.Equal(t, "", result)
}

func TestGetTextContent(t *testing.T) {
	tests := []struct {
		name string
		html string
		tag  string
		want string
	}{
		{
			name: "extracts simple text",
			html: `<html><body><p>Hello World</p></body></html>`,
			tag:  "p",
			want: "Hello World",
		},
		{
			name: "extracts text from nested tags",
			html: `<html><body><p>Hello <span>World</span></p></body></html>`,
			tag:  "p",
			want: "Hello World",
		},
		{
			name: "extracts text from deeply nested tags",
			html: `<html><body><div>A<span>B<em>C</em>D</span>E</div></body></html>`,
			tag:  "div",
			want: "ABCDE",
		},
		{
			name: "extracts text from title",
			html: `<html><head><title>Hello World Test</title></head></html>`,
			tag:  "title",
			want: "Hello World Test",
		},
		{
			name: "returns empty for empty element",
			html: `<html><body><p></p></body></html>`,
			tag:  "p",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseHTML(t, tt.html)
			target := findElement(doc, tt.tag)
			require.NotNil(t, target)

			result := getTextContent(target)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestGetTextContent_NilNode(t *testing.T) {
	result := getTextContent(nil)
	assert.Equal(t, "", result)
}

func TestContainsBlockingDirective(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"noindex blocks", "noindex", true},
		{"none blocks", "none", true},
		{"noindex with follow blocks", "noindex, follow", true},
		{"index follow does not block", "index, follow", false},
		{"all does not block", "all", false},
		{"empty does not block", "", false},
		{"noindex uppercase blocks", "NOINDEX", true},
		{"none uppercase blocks", "NONE", true},
		{"noindex mixed case blocks", "NoIndex", true},
		{"noindex with extra spaces blocks", "noindex , nofollow", true},
		{"noindexfoo does not block", "noindexfoo", false},
		{"foonoindex does not block", "foonoindex", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsBlockingDirective(tt.content)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsBlockedByMeta(t *testing.T) {
	tests := []struct {
		name string
		html string
		want bool
	}{
		{
			name: "no meta robots not blocked",
			html: `<html><head><title>Test</title></head></html>`,
			want: false,
		},
		{
			name: "robots index follow not blocked",
			html: `<html><head><meta name="robots" content="index, follow"></head></html>`,
			want: false,
		},
		{
			name: "robots noindex blocked",
			html: `<html><head><meta name="robots" content="noindex"></head></html>`,
			want: true,
		},
		{
			name: "robots none blocked",
			html: `<html><head><meta name="robots" content="none"></head></html>`,
			want: true,
		},
		{
			name: "googlebot index overrides robots noindex",
			html: `<html><head><meta name="googlebot" content="index"><meta name="robots" content="noindex"></head></html>`,
			want: false,
		},
		{
			name: "googlebot noindex blocked",
			html: `<html><head><meta name="googlebot" content="noindex"></head></html>`,
			want: true,
		},
		{
			name: "empty googlebot falls back to robots noindex",
			html: `<html><head><meta name="googlebot" content=""><meta name="robots" content="noindex"></head></html>`,
			want: true,
		},
		{
			name: "case insensitive name ROBOTS blocked",
			html: `<html><head><meta name="ROBOTS" content="noindex"></head></html>`,
			want: true,
		},
		{
			name: "case insensitive content NOINDEX blocked",
			html: `<html><head><meta name="robots" content="NOINDEX"></head></html>`,
			want: true,
		},
		{
			name: "noindex with extra spaces blocked",
			html: `<html><head><meta name="robots" content="noindex , nofollow"></head></html>`,
			want: true,
		},
		{
			name: "whitespace only googlebot falls back to robots",
			html: `<html><head><meta name="googlebot" content="   "><meta name="robots" content="noindex"></head></html>`,
			want: true,
		},
		{
			name: "googlebot all overrides robots noindex",
			html: `<html><head><meta name="googlebot" content="all"><meta name="robots" content="noindex"></head></html>`,
			want: false,
		},
		{
			name: "multiple googlebot tags first noindex blocks",
			html: `<html><head><meta name="googlebot" content="noindex"><meta name="googlebot" content="index"></head></html>`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseHTML(t, tt.html)
			head := findElement(doc, "head")
			require.NotNil(t, head)

			result := isBlockedByMeta(head)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsBlockedByMeta_MetaOutsideHead(t *testing.T) {
	htmlStr := `<html><head><title>Test</title></head><body><meta name="robots" content="noindex"></body></html>`
	doc := parseHTML(t, htmlStr)
	head := findElement(doc, "head")
	require.NotNil(t, head)

	result := isBlockedByMeta(head)
	assert.False(t, result, "meta outside head should be ignored")
}

func TestExtractCanonicalURL(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "extracts canonical href",
			html: `<html><head><link rel="canonical" href="https://example.com/page"></head></html>`,
			want: "https://example.com/page",
		},
		{
			name: "no canonical tag returns empty",
			html: `<html><head><title>Test</title></head></html>`,
			want: "",
		},
		{
			name: "case insensitive rel attribute",
			html: `<html><head><link REL="CANONICAL" href="https://example.com/page"></head></html>`,
			want: "https://example.com/page",
		},
		{
			name: "first canonical used when multiple exist",
			html: `<html><head><link rel="canonical" href="https://first.com"><link rel="canonical" href="https://second.com"></head></html>`,
			want: "https://first.com",
		},
		{
			name: "empty href returns empty",
			html: `<html><head><link rel="canonical" href=""></head></html>`,
			want: "",
		},
		{
			name: "other link types ignored",
			html: `<html><head><link rel="stylesheet" href="style.css"><link rel="canonical" href="https://example.com"></head></html>`,
			want: "https://example.com",
		},
		{
			name: "whitespace in href trimmed",
			html: `<html><head><link rel="canonical" href="  https://example.com/page  "></head></html>`,
			want: "https://example.com/page",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseHTML(t, tt.html)
			head := findElement(doc, "head")
			require.NotNil(t, head)

			result := extractCanonicalURL(head)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestResolveCanonicalURL(t *testing.T) {
	tests := []struct {
		name      string
		canonical string
		baseURL   string
		want      string
	}{
		{
			name:      "empty canonical returns empty",
			canonical: "",
			baseURL:   "https://example.com/page",
			want:      "",
		},
		{
			name:      "absolute URL unchanged",
			canonical: "https://example.com/page",
			baseURL:   "https://example.com/other",
			want:      "https://example.com/page",
		},
		{
			name:      "relative URL resolved",
			canonical: "/page",
			baseURL:   "https://example.com/other",
			want:      "https://example.com/page",
		},
		{
			name:      "protocol-relative URL resolved",
			canonical: "//example.com/page",
			baseURL:   "https://other.com/",
			want:      "https://example.com/page",
		},
		{
			name:      "relative path resolved",
			canonical: "page",
			baseURL:   "https://example.com/dir/other",
			want:      "https://example.com/dir/page",
		},
		{
			name:      "fragment preserved",
			canonical: "https://example.com/page#section",
			baseURL:   "https://example.com/",
			want:      "https://example.com/page#section",
		},
		{
			name:      "query string preserved",
			canonical: "https://example.com/page?q=1",
			baseURL:   "https://example.com/",
			want:      "https://example.com/page?q=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveCanonicalURL(tt.canonical, tt.baseURL)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsCanonical(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		finalURL string
		want     bool
	}{
		{
			name:     "no canonical tag passes",
			html:     `<html><head><title>Test</title></head></html>`,
			finalURL: "https://example.com/page",
			want:     true,
		},
		{
			name:     "canonical matches exactly passes",
			html:     `<html><head><link rel="canonical" href="https://example.com/page"></head></html>`,
			finalURL: "https://example.com/page",
			want:     true,
		},
		{
			name:     "canonical differs fails",
			html:     `<html><head><link rel="canonical" href="https://example.com/other"></head></html>`,
			finalURL: "https://example.com/page",
			want:     false,
		},
		{
			name:     "relative canonical resolved and matches",
			html:     `<html><head><link rel="canonical" href="/page"></head></html>`,
			finalURL: "https://example.com/page",
			want:     true,
		},
		{
			name:     "relative canonical resolved and differs",
			html:     `<html><head><link rel="canonical" href="/other"></head></html>`,
			finalURL: "https://example.com/page",
			want:     false,
		},
		{
			name:     "empty href passes",
			html:     `<html><head><link rel="canonical" href=""></head></html>`,
			finalURL: "https://example.com/page",
			want:     true,
		},
		{
			name:     "canonical with trailing slash differs from without",
			html:     `<html><head><link rel="canonical" href="https://example.com/page/"></head></html>`,
			finalURL: "https://example.com/page",
			want:     false,
		},
		{
			name:     "protocol-relative canonical resolved",
			html:     `<html><head><link rel="canonical" href="//example.com/page"></head></html>`,
			finalURL: "https://example.com/page",
			want:     true,
		},
		{
			name:     "canonical with different protocol fails",
			html:     `<html><head><link rel="canonical" href="http://example.com/page"></head></html>`,
			finalURL: "https://example.com/page",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseHTML(t, tt.html)
			head := findElement(doc, "head")
			require.NotNil(t, head)

			result := isCanonical(head, tt.finalURL)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsExecutableScript(t *testing.T) {
	tests := []struct {
		name string
		html string
		want bool
	}{
		{
			name: "no type attribute is executable",
			html: `<html><head><script>code</script></head></html>`,
			want: true,
		},
		{
			name: "text/javascript is executable",
			html: `<html><head><script type="text/javascript">code</script></head></html>`,
			want: true,
		},
		{
			name: "module is executable",
			html: `<html><head><script type="module">code</script></head></html>`,
			want: true,
		},
		{
			name: "application/javascript is executable",
			html: `<html><head><script type="application/javascript">code</script></head></html>`,
			want: true,
		},
		{
			name: "application/ld+json is NOT executable",
			html: `<html><head><script type="application/ld+json">{}</script></head></html>`,
			want: false,
		},
		{
			name: "application/json is NOT executable",
			html: `<html><head><script type="application/json">{}</script></head></html>`,
			want: false,
		},
		{
			name: "text/template is NOT executable",
			html: `<html><head><script type="text/template"><div></div></script></head></html>`,
			want: false,
		},
		{
			name: "text/x-template is NOT executable",
			html: `<html><head><script type="text/x-template"><div></div></script></head></html>`,
			want: false,
		},
		{
			name: "importmap is NOT executable",
			html: `<html><head><script type="importmap">{}</script></head></html>`,
			want: false,
		},
		{
			name: "empty string type is executable",
			html: `<html><head><script type="">code</script></head></html>`,
			want: true,
		},
		{
			name: "whitespace only type is executable",
			html: `<html><head><script type="  ">code</script></head></html>`,
			want: true,
		},
		{
			name: "text/x-custom is NOT executable",
			html: `<html><head><script type="text/x-custom">code</script></head></html>`,
			want: false,
		},
		{
			name: "uppercase TEXT/JAVASCRIPT is executable",
			html: `<html><head><script type="TEXT/JAVASCRIPT">code</script></head></html>`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseHTML(t, tt.html)
			script := findElement(doc, "script")
			require.NotNil(t, script)

			result := isExecutableScript(script)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsExecutableScript_NilNode(t *testing.T) {
	result := isExecutableScript(nil)
	assert.False(t, result)
}

func TestIsExecutableScript_NonScriptElement(t *testing.T) {
	doc := parseHTML(t, `<html><head><div>text</div></head></html>`)
	div := findElement(doc, "div")
	require.NotNil(t, div)

	result := isExecutableScript(div)
	assert.False(t, result)
}

func TestIsScriptRelatedLink(t *testing.T) {
	tests := []struct {
		name string
		html string
		want bool
	}{
		{
			name: "rel=import is related",
			html: `<html><head><link rel="import" href="/component.html"></head></html>`,
			want: true,
		},
		{
			name: "rel=modulepreload is related",
			html: `<html><head><link rel="modulepreload" href="/module.js"></head></html>`,
			want: true,
		},
		{
			name: "rel=preload as=script is related",
			html: `<html><head><link rel="preload" as="script" href="/app.js"></head></html>`,
			want: true,
		},
		{
			name: "rel=preload as=style is NOT related",
			html: `<html><head><link rel="preload" as="style" href="/app.css"></head></html>`,
			want: false,
		},
		{
			name: "rel=stylesheet is NOT related",
			html: `<html><head><link rel="stylesheet" href="/app.css"></head></html>`,
			want: false,
		},
		{
			name: "rel=canonical is NOT related",
			html: `<html><head><link rel="canonical" href="https://example.com"></head></html>`,
			want: false,
		},
		{
			name: "rel=preload as=SCRIPT uppercase is related",
			html: `<html><head><link rel="preload" as="SCRIPT" href="/app.js"></head></html>`,
			want: true,
		},
		{
			name: "rel=IMPORT uppercase is related",
			html: `<html><head><link rel="IMPORT" href="/component.html"></head></html>`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseHTML(t, tt.html)
			link := findElement(doc, "link")
			require.NotNil(t, link)

			result := isScriptRelatedLink(link)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsScriptRelatedLink_NilNode(t *testing.T) {
	result := isScriptRelatedLink(nil)
	assert.False(t, result)
}

func TestIsScriptRelatedLink_NonLinkElement(t *testing.T) {
	doc := parseHTML(t, `<html><head><div>text</div></head></html>`)
	div := findElement(doc, "div")
	require.NotNil(t, div)

	result := isScriptRelatedLink(div)
	assert.False(t, result)
}

func TestCleanScripts(t *testing.T) {
	t.Run("removes executable scripts preserves JSON-LD", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<script type="application/ld+json">{"@type": "WebPage"}</script>
			<script>console.log('remove me');</script>
		</head><body></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.True(t, removed)

		// Verify JSON-LD is preserved by checking we can still find it
		domDoc := doc.(*domDocument)
		scripts := findAllElementsInParent(domDoc.root, "script")
		assert.Len(t, scripts, 1)
		assert.Equal(t, "application/ld+json", getAttr(scripts[0], "type"))
	})

	t.Run("returns false when no scripts", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head><title>Test</title></head><body><p>Content</p></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.False(t, removed)
	})

	t.Run("returns false when only JSON-LD", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<script type="application/ld+json">{"@type": "WebPage"}</script>
		</head><body></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.False(t, removed)
	})

	t.Run("removes link imports", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<link rel="import" href="/component.html">
			<link rel="stylesheet" href="/style.css">
		</head><body></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.True(t, removed)

		// Verify only stylesheet link remains
		domDoc := doc.(*domDocument)
		links := findAllElementsInParent(domDoc.root, "link")
		assert.Len(t, links, 1)
		assert.Equal(t, "stylesheet", getAttr(links[0], "rel"))
	})

	t.Run("removes modulepreload links", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<link rel="modulepreload" href="/module.js">
		</head><body></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.True(t, removed)

		domDoc := doc.(*domDocument)
		links := findAllElementsInParent(domDoc.root, "link")
		assert.Len(t, links, 0)
	})

	t.Run("removes preload as=script links", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<link rel="preload" as="script" href="/app.js">
			<link rel="preload" as="style" href="/app.css">
		</head><body></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.True(t, removed)

		// Verify only style preload remains
		domDoc := doc.(*domDocument)
		links := findAllElementsInParent(domDoc.root, "link")
		assert.Len(t, links, 1)
		assert.Equal(t, "style", getAttr(links[0], "as"))
	})

	t.Run("preserves template scripts", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head></head><body>
			<script type="text/template"><div>Template</div></script>
			<script type="text/x-template"><div>X-Template</div></script>
		</body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.False(t, removed)

		domDoc := doc.(*domDocument)
		scripts := findAllElementsInParent(domDoc.root, "script")
		assert.Len(t, scripts, 2)
	})

	t.Run("mixed scripts some removed some preserved", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<script type="application/ld+json">{"@type": "WebPage"}</script>
			<script>console.log('executable');</script>
			<script type="module">import './app.js';</script>
			<link rel="modulepreload" href="/module.js">
		</head><body>
			<script type="text/template"><div>Template</div></script>
		</body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.True(t, removed)

		// Verify: JSON-LD and template preserved, executable and modulepreload removed
		domDoc := doc.(*domDocument)
		scripts := findAllElementsInParent(domDoc.root, "script")
		assert.Len(t, scripts, 2, "should have JSON-LD and template scripts")

		types := make([]string, 0, len(scripts))
		for _, s := range scripts {
			types = append(types, getAttr(s, "type"))
		}
		assert.Contains(t, types, "application/ld+json")
		assert.Contains(t, types, "text/template")

		links := findAllElementsInParent(domDoc.root, "link")
		assert.Len(t, links, 0, "modulepreload link should be removed")
	})

	t.Run("removes executable scripts from body", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head></head><body>
			<p>Before</p>
			<script>console.log('inline in body');</script>
			<p>After</p>
			<script type="module">import './app.js';</script>
		</body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.True(t, removed)

		domDoc := doc.(*domDocument)
		scripts := findAllElementsInParent(domDoc.root, "script")
		assert.Len(t, scripts, 0, "all executable scripts in body should be removed")

		// Verify content paragraphs are preserved
		paragraphs := findAllElementsInParent(domDoc.root, "p")
		assert.Len(t, paragraphs, 2)
	})

	t.Run("removes external scripts", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<script src="/vendor/jquery.min.js"></script>
			<script src="/app.js" type="module"></script>
			<script src="/analytics.js" type="text/javascript"></script>
		</head><body></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.True(t, removed)

		domDoc := doc.(*domDocument)
		scripts := findAllElementsInParent(domDoc.root, "script")
		assert.Len(t, scripts, 0, "all external scripts should be removed")
	})
}

func TestHTML(t *testing.T) {
	t.Run("returns equivalent HTML", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head><title>Test</title></head><body><p>Content</p></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		result := doc.HTML()
		require.NotNil(t, result)

		// Result should contain key elements
		resultStr := string(result)
		assert.Contains(t, resultStr, "<title>Test</title>")
		assert.Contains(t, resultStr, "<p>Content</p>")
	})

	t.Run("returns HTML without scripts after CleanScripts", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<script>console.log('remove');</script>
			<title>Test</title>
		</head><body></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		doc.CleanScripts()
		result := doc.HTML()
		require.NotNil(t, result)

		resultStr := string(result)
		assert.Contains(t, resultStr, "<title>Test</title>")
		assert.NotContains(t, resultStr, "console.log")
	})

	t.Run("result can be re-parsed successfully", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head><title>Original</title></head><body><p>Content</p></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		result := doc.HTML()
		require.NotNil(t, result)

		// Re-parse the result
		doc2, err := ParseWithDOM(result)
		require.NoError(t, err)
		assert.Equal(t, "Original", doc2.Title())
	})
}

func TestCleanScriptsAndHTML_Integration(t *testing.T) {
	htmlStr := `<!DOCTYPE html><html><head>
		<title>Integration Test</title>
		<script type="application/ld+json">{"@type": "WebPage", "name": "Test"}</script>
		<script>console.log('executable');</script>
		<script type="module">import './app.js';</script>
		<link rel="modulepreload" href="/module.js">
	</head><body>
		<p>Content</p>
		<script type="text/template"><div>Template</div></script>
	</body></html>`

	// Step 1: Parse
	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	// Step 2: CleanScripts
	removed := doc.CleanScripts()
	assert.True(t, removed)

	// Step 3: Get HTML
	result := doc.HTML()
	require.NotNil(t, result)

	// Step 4: Re-parse
	doc2, err := ParseWithDOM(result)
	require.NoError(t, err)

	// Step 5: Verify no executable scripts
	domDoc2 := doc2.(*domDocument)
	scripts := findAllElementsInParent(domDoc2.root, "script")

	// Should have exactly 2 scripts: JSON-LD and template
	assert.Len(t, scripts, 2)

	scriptTypes := make(map[string]bool)
	for _, s := range scripts {
		scriptTypes[getAttr(s, "type")] = true
	}

	// Step 6: Verify JSON-LD preserved
	assert.True(t, scriptTypes["application/ld+json"], "JSON-LD should be preserved")
	assert.True(t, scriptTypes["text/template"], "template should be preserved")

	// Verify no executable types
	assert.False(t, scriptTypes[""], "no typeless scripts")
	assert.False(t, scriptTypes["module"], "no module scripts")
	assert.False(t, scriptTypes["text/javascript"], "no text/javascript scripts")

	// Verify modulepreload link removed
	links := findAllElementsInParent(domDoc2.root, "link")
	for _, link := range links {
		assert.NotEqual(t, "modulepreload", getAttr(link, "rel"))
	}

	// Verify title preserved
	assert.Equal(t, "Integration Test", doc2.Title())
}

func TestFixtures(t *testing.T) {
	t.Run("title_basic fixture", func(t *testing.T) {
		data := loadFixture(t, "title_basic.html")
		doc, err := ParseWithDOM(data)
		require.NoError(t, err)
		assert.Equal(t, "Basic Title Test", doc.Title())
	})

	t.Run("title_simple fixture", func(t *testing.T) {
		data := loadFixture(t, "title_simple.html")
		doc, err := ParseWithDOM(data)
		require.NoError(t, err)
		assert.Equal(t, "Hello World Test", doc.Title())
	})

	t.Run("title_unicode fixture", func(t *testing.T) {
		data := loadFixture(t, "title_unicode.html")
		doc, err := ParseWithDOM(data)
		require.NoError(t, err)
		title := doc.Title()
		assert.Contains(t, title, "日本語")
		assert.Contains(t, title, "中文")
		assert.Contains(t, title, "한국어")
	})

	t.Run("index_clean fixture", func(t *testing.T) {
		data := loadFixture(t, "index_clean.html")
		doc, err := ParseWithDOM(data)
		require.NoError(t, err)

		assert.Equal(t, "Indexable Page", doc.Title())
		status := doc.IndexationStatus(200, "https://example.com/page")
		assert.Equal(t, types.IndexStatusIndexable, status)
	})

	t.Run("index_robots_noindex fixture", func(t *testing.T) {
		data := loadFixture(t, "index_robots_noindex.html")
		doc, err := ParseWithDOM(data)
		require.NoError(t, err)

		assert.Equal(t, "Blocked Page", doc.Title())
		status := doc.IndexationStatus(200, "https://example.com/page")
		assert.Equal(t, types.IndexStatusBlockedByMeta, status)
	})

	t.Run("scripts_mixed fixture", func(t *testing.T) {
		data := loadFixture(t, "scripts_mixed.html")
		doc, err := ParseWithDOM(data)
		require.NoError(t, err)

		assert.Equal(t, "Mixed Scripts", doc.Title())

		removed := doc.CleanScripts()
		assert.True(t, removed)

		result := doc.HTML()
		require.NotNil(t, result)

		// Re-parse and verify
		doc2, err := ParseWithDOM(result)
		require.NoError(t, err)

		domDoc2 := doc2.(*domDocument)
		scripts := findAllElementsInParent(domDoc2.root, "script")

		// Should have JSON-LD and template preserved
		assert.Len(t, scripts, 2)

		scriptTypes := make(map[string]bool)
		for _, s := range scripts {
			scriptTypes[getAttr(s, "type")] = true
		}
		assert.True(t, scriptTypes["application/ld+json"])
		assert.True(t, scriptTypes["text/template"])
	})

	t.Run("malformed_unclosed fixture parses gracefully", func(t *testing.T) {
		data := loadFixture(t, "malformed_unclosed.html")
		doc, err := ParseWithDOM(data)
		require.NoError(t, err, "parser should handle malformed HTML")
		assert.Equal(t, "Malformed Page", doc.Title())
	})

	t.Run("empty fixture parses gracefully", func(t *testing.T) {
		data := loadFixture(t, "empty.html")
		doc, err := ParseWithDOM(data)
		require.NoError(t, err, "parser should handle empty HTML")
		assert.Equal(t, "", doc.Title())
		assert.Equal(t, types.IndexStatusIndexable, doc.IndexationStatus(200, "https://example.com"))
	})

	t.Run("no_head fixture returns empty title", func(t *testing.T) {
		data := loadFixture(t, "no_head.html")
		doc, err := ParseWithDOM(data)
		require.NoError(t, err)
		assert.Equal(t, "", doc.Title())
		assert.Equal(t, types.IndexStatusIndexable, doc.IndexationStatus(200, "https://example.com"))
	})
}
