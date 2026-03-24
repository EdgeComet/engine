package htmlprocessor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "tests", "fixtures", "htmlprocessor", name)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to load fixture: %s", name)
	return data
}

func TestGoQueryFind(t *testing.T) {
	tests := []struct {
		name      string
		html      string
		selector  string
		wantCount int
	}{
		{
			name:      "finds nested element",
			html:      `<html><body><div><span id="target">text</span></div></body></html>`,
			selector:  "span",
			wantCount: 1,
		},
		{
			name:      "returns zero for missing element",
			html:      `<html><body><div>text</div></body></html>`,
			selector:  "span",
			wantCount: 0,
		},
		{
			name:      "finds first match among multiple",
			html:      `<html><body><div id="first"></div><div id="second"></div></body></html>`,
			selector:  "div",
			wantCount: 2,
		},
		{
			name:      "finds deeply nested element",
			html:      `<html><body><div><section><article><p>text</p></article></section></div></body></html>`,
			selector:  "p",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseWithDOM([]byte(tt.html))
			require.NoError(t, err)

			result := doc.GoQueryDoc().Find(tt.selector)
			assert.Equal(t, tt.wantCount, result.Length())
		})
	}
}

func TestGoQueryFindInParent(t *testing.T) {
	htmlStr := `<html><head><title>Test</title></head><body><title>Body Title</title></body></html>`
	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	headTitle := doc.GoQueryDoc().Find("head title").First().Text()
	assert.Equal(t, "Test", headTitle)
}

func TestGoQueryFindAllInParent(t *testing.T) {
	htmlStr := `<html><head><meta name="robots"><meta name="googlebot"><meta name="description"></head><body></body></html>`
	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	metas := doc.GoQueryDoc().Find("head meta")
	assert.Equal(t, 3, metas.Length())
}

func TestGoQueryFindAllInParent_NoMatch(t *testing.T) {
	htmlStr := `<html><head><title>Test</title></head></html>`
	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	result := doc.GoQueryDoc().Find("head meta")
	assert.Equal(t, 0, result.Length())
}

func TestGoQueryAttr(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		selector string
		attrName string
		want     string
	}{
		{
			name:     "gets attribute value",
			html:     `<html><body><div id="test-id">text</div></body></html>`,
			selector: "div",
			attrName: "id",
			want:     "test-id",
		},
		{
			name:     "returns empty for missing attribute",
			html:     `<html><body><div>text</div></body></html>`,
			selector: "div",
			attrName: "id",
			want:     "",
		},
		{
			name:     "handles content attribute on meta",
			html:     `<html><head><meta name="robots" content="noindex"></head></html>`,
			selector: "meta",
			attrName: "content",
			want:     "noindex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseWithDOM([]byte(tt.html))
			require.NoError(t, err)

			result := getSelectionAttr(doc.GoQueryDoc().Find(tt.selector).First(), tt.attrName)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestGoQueryText(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		selector string
		want     string
	}{
		{
			name:     "extracts simple text",
			html:     `<html><body><p>Hello World</p></body></html>`,
			selector: "p",
			want:     "Hello World",
		},
		{
			name:     "extracts text from nested tags",
			html:     `<html><body><p>Hello <span>World</span></p></body></html>`,
			selector: "p",
			want:     "Hello World",
		},
		{
			name:     "extracts text from deeply nested tags",
			html:     `<html><body><div>A<span>B<em>C</em>D</span>E</div></body></html>`,
			selector: "div",
			want:     "ABCDE",
		},
		{
			name:     "extracts text from title",
			html:     `<html><head><title>Hello World Test</title></head></html>`,
			selector: "title",
			want:     "Hello World Test",
		},
		{
			name:     "returns empty for empty element",
			html:     `<html><body><p></p></body></html>`,
			selector: "p",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseWithDOM([]byte(tt.html))
			require.NoError(t, err)

			result := doc.GoQueryDoc().Find(tt.selector).First().Text()
			assert.Equal(t, tt.want, result)
		})
	}
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

func TestIndexationStatus_BlockedByMeta(t *testing.T) {
	tests := []struct {
		name string
		html string
		want types.IndexStatus
	}{
		{
			name: "no meta robots not blocked",
			html: `<html><head><title>Test</title></head></html>`,
			want: types.IndexStatusIndexable,
		},
		{
			name: "robots index follow not blocked",
			html: `<html><head><meta name="robots" content="index, follow"></head></html>`,
			want: types.IndexStatusIndexable,
		},
		{
			name: "robots noindex blocked",
			html: `<html><head><meta name="robots" content="noindex"></head></html>`,
			want: types.IndexStatusBlockedByMeta,
		},
		{
			name: "robots none blocked",
			html: `<html><head><meta name="robots" content="none"></head></html>`,
			want: types.IndexStatusBlockedByMeta,
		},
		{
			name: "googlebot index overrides robots noindex",
			html: `<html><head><meta name="googlebot" content="index"><meta name="robots" content="noindex"></head></html>`,
			want: types.IndexStatusIndexable,
		},
		{
			name: "googlebot noindex blocked",
			html: `<html><head><meta name="googlebot" content="noindex"></head></html>`,
			want: types.IndexStatusBlockedByMeta,
		},
		{
			name: "empty googlebot falls back to robots noindex",
			html: `<html><head><meta name="googlebot" content=""><meta name="robots" content="noindex"></head></html>`,
			want: types.IndexStatusBlockedByMeta,
		},
		{
			name: "case insensitive content NOINDEX blocked",
			html: `<html><head><meta name="robots" content="NOINDEX"></head></html>`,
			want: types.IndexStatusBlockedByMeta,
		},
		{
			name: "noindex with extra spaces blocked",
			html: `<html><head><meta name="robots" content="noindex , nofollow"></head></html>`,
			want: types.IndexStatusBlockedByMeta,
		},
		{
			name: "whitespace only googlebot falls back to robots",
			html: `<html><head><meta name="googlebot" content="   "><meta name="robots" content="noindex"></head></html>`,
			want: types.IndexStatusBlockedByMeta,
		},
		{
			name: "googlebot all overrides robots noindex",
			html: `<html><head><meta name="googlebot" content="all"><meta name="robots" content="noindex"></head></html>`,
			want: types.IndexStatusIndexable,
		},
		{
			name: "multiple googlebot tags first noindex blocks",
			html: `<html><head><meta name="googlebot" content="noindex"><meta name="googlebot" content="index"></head></html>`,
			want: types.IndexStatusBlockedByMeta,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseWithDOM([]byte(tt.html))
			require.NoError(t, err)

			result := doc.IndexationStatus(200, "https://example.com/page")
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIndexationStatus_MetaOutsideHead(t *testing.T) {
	htmlStr := `<html><head><title>Test</title></head><body><meta name="robots" content="noindex"></body></html>`
	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	result := doc.IndexationStatus(200, "https://example.com/page")
	assert.Equal(t, types.IndexStatusIndexable, result, "meta outside head should be ignored")
}

func TestCanonicalURLExtraction_GoQuery(t *testing.T) {
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
			doc, err := ParseWithDOM([]byte(tt.html))
			require.NoError(t, err)

			result := strings.TrimSpace(getSelectionAttr(doc.GoQueryDoc().Find("head link[rel='canonical']").First(), "href"))
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

func TestIndexationStatus_Canonical(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		finalURL string
		want     types.IndexStatus
	}{
		{
			name:     "no canonical tag passes",
			html:     `<html><head><title>Test</title></head></html>`,
			finalURL: "https://example.com/page",
			want:     types.IndexStatusIndexable,
		},
		{
			name:     "canonical matches exactly passes",
			html:     `<html><head><link rel="canonical" href="https://example.com/page"></head></html>`,
			finalURL: "https://example.com/page",
			want:     types.IndexStatusIndexable,
		},
		{
			name:     "canonical differs fails",
			html:     `<html><head><link rel="canonical" href="https://example.com/other"></head></html>`,
			finalURL: "https://example.com/page",
			want:     types.IndexStatusNonCanonical,
		},
		{
			name:     "relative canonical resolved and matches",
			html:     `<html><head><link rel="canonical" href="/page"></head></html>`,
			finalURL: "https://example.com/page",
			want:     types.IndexStatusIndexable,
		},
		{
			name:     "relative canonical resolved and differs",
			html:     `<html><head><link rel="canonical" href="/other"></head></html>`,
			finalURL: "https://example.com/page",
			want:     types.IndexStatusNonCanonical,
		},
		{
			name:     "empty href passes",
			html:     `<html><head><link rel="canonical" href=""></head></html>`,
			finalURL: "https://example.com/page",
			want:     types.IndexStatusIndexable,
		},
		{
			name:     "canonical with trailing slash differs from without",
			html:     `<html><head><link rel="canonical" href="https://example.com/page/"></head></html>`,
			finalURL: "https://example.com/page",
			want:     types.IndexStatusNonCanonical,
		},
		{
			name:     "protocol-relative canonical resolved",
			html:     `<html><head><link rel="canonical" href="//example.com/page"></head></html>`,
			finalURL: "https://example.com/page",
			want:     types.IndexStatusIndexable,
		},
		{
			name:     "canonical with different protocol fails",
			html:     `<html><head><link rel="canonical" href="http://example.com/page"></head></html>`,
			finalURL: "https://example.com/page",
			want:     types.IndexStatusNonCanonical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseWithDOM([]byte(tt.html))
			require.NoError(t, err)

			result := doc.IndexationStatus(200, tt.finalURL)
			assert.Equal(t, tt.want, result)
		})
	}
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

		scripts := doc.GoQueryDoc().Find("script")
		assert.Equal(t, 1, scripts.Length())
		assert.Equal(t, "application/ld+json", getSelectionAttr(scripts.First(), "type"))
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

		links := doc.GoQueryDoc().Find("link")
		assert.Equal(t, 1, links.Length())
		assert.Equal(t, "stylesheet", getSelectionAttr(links.First(), "rel"))
	})

	t.Run("removes modulepreload links", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<link rel="modulepreload" href="/module.js">
		</head><body></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		removed := doc.CleanScripts()
		assert.True(t, removed)

		links := doc.GoQueryDoc().Find("link")
		assert.Equal(t, 0, links.Length())
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

		links := doc.GoQueryDoc().Find("link")
		assert.Equal(t, 1, links.Length())
		assert.Equal(t, "style", getSelectionAttr(links.First(), "as"))
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

		scripts := doc.GoQueryDoc().Find("script")
		assert.Equal(t, 2, scripts.Length())
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

		scripts := doc.GoQueryDoc().Find("script")
		assert.Equal(t, 2, scripts.Length(), "should have JSON-LD and template scripts")

		var scriptTypes []string
		scripts.Each(func(_ int, s *goquery.Selection) {
			scriptTypes = append(scriptTypes, getSelectionAttr(s, "type"))
		})
		assert.Contains(t, scriptTypes, "application/ld+json")
		assert.Contains(t, scriptTypes, "text/template")

		links := doc.GoQueryDoc().Find("link")
		assert.Equal(t, 0, links.Length(), "modulepreload link should be removed")
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

		scripts := doc.GoQueryDoc().Find("script")
		assert.Equal(t, 0, scripts.Length(), "all executable scripts in body should be removed")

		paragraphs := doc.GoQueryDoc().Find("p")
		assert.Equal(t, 2, paragraphs.Length())
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

		scripts := doc.GoQueryDoc().Find("script")
		assert.Equal(t, 0, scripts.Length(), "all external scripts should be removed")
	})
}

func TestCleanScripts_AllExecutableTypes(t *testing.T) {
	htmlStr := `<!DOCTYPE html><html><head>
		<script>console.log('no type')</script>
		<script type="text/javascript">console.log('js')</script>
		<script type="module">import './a.js'</script>
		<script type="application/javascript">console.log('app-js')</script>
		<script type="">console.log('empty')</script>
		<script type="  ">console.log('whitespace')</script>
		<script type="TEXT/JAVASCRIPT">console.log('uppercase')</script>
	</head><body></body></html>`

	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	removed := doc.CleanScripts()
	assert.True(t, removed)
	assert.Equal(t, 0, doc.GoQueryDoc().Find("script").Length())
}

func TestCleanScripts_PreservesNonExecutable(t *testing.T) {
	htmlStr := `<!DOCTYPE html><html><head>
		<script type="application/ld+json">{"@type":"Article"}</script>
		<script type="application/json">{}</script>
		<script type="text/template"><div></div></script>
		<script type="text/x-template"><div></div></script>
		<script type="text/x-custom">code</script>
		<script type="importmap">{}</script>
	</head><body></body></html>`

	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	removed := doc.CleanScripts()
	assert.False(t, removed)
	assert.Equal(t, 6, doc.GoQueryDoc().Find("script").Length())
}

func TestCleanScripts_ScriptRelatedLinks(t *testing.T) {
	tests := []struct {
		name      string
		html      string
		removed   bool
		remaining int
	}{
		{
			name:      "rel=import is removed",
			html:      `<!DOCTYPE html><html><head><link rel="import" href="/component.html"></head><body></body></html>`,
			removed:   true,
			remaining: 0,
		},
		{
			name:      "rel=modulepreload is removed",
			html:      `<!DOCTYPE html><html><head><link rel="modulepreload" href="/module.js"></head><body></body></html>`,
			removed:   true,
			remaining: 0,
		},
		{
			name:      "rel=preload as=script is removed",
			html:      `<!DOCTYPE html><html><head><link rel="preload" as="script" href="/app.js"></head><body></body></html>`,
			removed:   true,
			remaining: 0,
		},
		{
			name:      "rel=preload as=style is NOT removed",
			html:      `<!DOCTYPE html><html><head><link rel="preload" as="style" href="/app.css"></head><body></body></html>`,
			removed:   false,
			remaining: 1,
		},
		{
			name:      "rel=stylesheet is NOT removed",
			html:      `<!DOCTYPE html><html><head><link rel="stylesheet" href="/app.css"></head><body></body></html>`,
			removed:   false,
			remaining: 1,
		},
		{
			name:      "rel=canonical is NOT removed",
			html:      `<!DOCTYPE html><html><head><link rel="canonical" href="https://example.com"></head><body></body></html>`,
			removed:   false,
			remaining: 1,
		},
		{
			name:      "rel=IMPORT uppercase is removed",
			html:      `<!DOCTYPE html><html><head><link rel="IMPORT" href="/component.html"></head><body></body></html>`,
			removed:   true,
			remaining: 0,
		},
		{
			name:      "rel=preload as=SCRIPT uppercase is removed",
			html:      `<!DOCTYPE html><html><head><link rel="preload" as="SCRIPT" href="/app.js"></head><body></body></html>`,
			removed:   true,
			remaining: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseWithDOM([]byte(tt.html))
			require.NoError(t, err)

			result := doc.CleanScripts()
			assert.Equal(t, tt.removed, result)
			assert.Equal(t, tt.remaining, doc.GoQueryDoc().Find("link").Length())
		})
	}
}

func TestHTML(t *testing.T) {
	t.Run("returns equivalent HTML", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head><title>Test</title></head><body><p>Content</p></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)

		result := doc.HTML()
		require.NotNil(t, result)

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

	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	removed := doc.CleanScripts()
	assert.True(t, removed)

	result := doc.HTML()
	require.NotNil(t, result)

	doc2, err := ParseWithDOM(result)
	require.NoError(t, err)

	scripts := doc2.GoQueryDoc().Find("script")
	assert.Equal(t, 2, scripts.Length())

	scriptTypes := make(map[string]bool)
	scripts.Each(func(_ int, s *goquery.Selection) {
		scriptTypes[getSelectionAttr(s, "type")] = true
	})

	assert.True(t, scriptTypes["application/ld+json"], "JSON-LD should be preserved")
	assert.True(t, scriptTypes["text/template"], "template should be preserved")
	assert.False(t, scriptTypes[""], "no typeless scripts")
	assert.False(t, scriptTypes["module"], "no module scripts")
	assert.False(t, scriptTypes["text/javascript"], "no text/javascript scripts")

	doc2.GoQueryDoc().Find("link").Each(func(_ int, s *goquery.Selection) {
		assert.NotEqual(t, "modulepreload", getSelectionAttr(s, "rel"))
	})

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

		doc2, err := ParseWithDOM(result)
		require.NoError(t, err)

		scripts := doc2.GoQueryDoc().Find("script")
		assert.Equal(t, 2, scripts.Length())

		scriptTypes := make(map[string]bool)
		scripts.Each(func(_ int, s *goquery.Selection) {
			scriptTypes[getSelectionAttr(s, "type")] = true
		})
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

func TestGoQueryDoc_NotNil(t *testing.T) {
	doc, err := ParseWithDOM([]byte(`<html><head><title>Hello</title></head><body></body></html>`))
	require.NoError(t, err)
	assert.NotNil(t, doc.GoQueryDoc())
}

func TestGoQueryDoc_SameDOM(t *testing.T) {
	doc, err := ParseWithDOM([]byte(`<html><head><title>Test</title></head><body></body></html>`))
	require.NoError(t, err)

	gqDoc := doc.GoQueryDoc()
	require.NotNil(t, gqDoc)
	assert.Equal(t, "Test", gqDoc.Find("title").Text())
}

func TestGoQueryDoc_MutationVisible(t *testing.T) {
	htmlStr := `<html><head><script>alert(1)</script><script type="application/ld+json">{"@type":"Article"}</script></head><body></body></html>`
	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	doc.CleanScripts()

	gqDoc := doc.GoQueryDoc()
	require.NotNil(t, gqDoc)
	assert.Equal(t, 0, gqDoc.Find("script:not([type])").Length())
	assert.Equal(t, 1, gqDoc.Find("script[type='application/ld+json']").Length())
}

func TestHTML_RoundTrip(t *testing.T) {
	htmlStr := `<html><head><title>RoundTrip</title></head><body><p>Hello</p></body></html>`
	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	rendered := doc.HTML()
	require.NotNil(t, rendered)

	doc2, err := ParseWithDOM(rendered)
	require.NoError(t, err)
	assert.Equal(t, "RoundTrip", doc2.Title())
}

func TestCleanScripts_GoQuery_PreservesNonExecutable(t *testing.T) {
	htmlStr := `<!DOCTYPE html><html><head>
		<script type="application/ld+json">{"@type":"Article"}</script>
		<script type="text/template"><div></div></script>
	</head><body></body></html>`

	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	removed := doc.CleanScripts()
	assert.False(t, removed)
	assert.Equal(t, 2, doc.GoQueryDoc().Find("script").Length())
}

func TestCleanScripts_GoQuery_RemovesAllExecutableTypes(t *testing.T) {
	htmlStr := `<!DOCTYPE html><html><head>
		<script>console.log('no type')</script>
		<script type="text/javascript">console.log('js')</script>
		<script type="module">import './a.js'</script>
		<script type="application/javascript">console.log('app-js')</script>
		<script type="">console.log('empty')</script>
	</head><body></body></html>`

	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	removed := doc.CleanScripts()
	assert.True(t, removed)
	assert.Equal(t, 0, doc.GoQueryDoc().Find("script").Length())
}

func TestCleanScripts_GoQuery_RemovesScriptLinks(t *testing.T) {
	htmlStr := `<!DOCTYPE html><html><head>
		<link rel="import" href="x">
		<link rel="modulepreload" href="y">
		<link rel="preload" as="script" href="z">
		<link rel="stylesheet" href="w">
	</head><body></body></html>`

	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)

	removed := doc.CleanScripts()
	assert.True(t, removed)
	assert.Equal(t, 1, doc.GoQueryDoc().Find("link").Length())
}

func TestIndexationStatus_GoQuery_MetaPriority(t *testing.T) {
	t.Run("non-200 returns IndexStatusNon200", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head></head><body></body></html>`
		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)
		assert.Equal(t, types.IndexStatusNon200, doc.IndexationStatus(404, "https://example.com/"))
	})

	t.Run("robots noindex returns IndexStatusBlockedByMeta", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head><meta name="robots" content="noindex"></head><body></body></html>`
		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)
		assert.Equal(t, types.IndexStatusBlockedByMeta, doc.IndexationStatus(200, "https://example.com/"))
	})

	t.Run("non-canonical returns IndexStatusNonCanonical", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head><link rel="canonical" href="https://other.com/"></head><body></body></html>`
		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)
		assert.Equal(t, types.IndexStatusNonCanonical, doc.IndexationStatus(200, "https://example.com/"))
	})

	t.Run("all checks pass returns IndexStatusIndexable", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head><link rel="canonical" href="https://example.com/"></head><body></body></html>`
		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)
		assert.Equal(t, types.IndexStatusIndexable, doc.IndexationStatus(200, "https://example.com/"))
	})
}

func TestIndexationStatus_GoQuery_GooglebotPrecedence(t *testing.T) {
	t.Run("googlebot allows overrides robots noindex", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<meta name="robots" content="noindex">
			<meta name="googlebot" content="index, follow">
		</head><body></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)
		assert.Equal(t, types.IndexStatusIndexable, doc.IndexationStatus(200, "https://example.com/"))
	})

	t.Run("googlebot noindex overrides robots index", func(t *testing.T) {
		htmlStr := `<!DOCTYPE html><html><head>
			<meta name="robots" content="index, follow">
			<meta name="googlebot" content="noindex">
		</head><body></body></html>`

		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)
		assert.Equal(t, types.IndexStatusBlockedByMeta, doc.IndexationStatus(200, "https://example.com/"))
	})
}
