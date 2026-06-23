package htmlprocessor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

// captured returns the PageLinks indexed by normalized target for easy assertions.
func capturedByTarget(links []types.PageLink) map[string]types.PageLink {
	m := make(map[string]types.PageLink, len(links))
	for _, l := range links {
		m[l.Target] = l
	}
	return m
}

func TestCaptureLinks_TargetsFlagsAndNormalization(t *testing.T) {
	html := `<html><body>
		<a href="/about">Internal</a>
		<a href="HTTPS://Example.COM:443/Path?b=2&a=1#frag">Messy</a>
		<a href="https://other.com/x" rel="nofollow sponsored">Ext</a>
		<a href="https://ugc.com/y" rel="ugc">UGC</a>
		<a href="/gallery"><img src="/i.png" alt="pic"></a>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/dir/", "https://example.com/dir/", seo)

	byTgt := capturedByTarget(seo.PageLinks)
	require.Len(t, seo.PageLinks, 5)

	// Relative resolved-to-absolute then normalized; internal.
	about := byTgt["https://example.com/about"]
	assert.Equal(t, "Internal", about.Anchor)
	assert.True(t, about.IsInternal)

	// Target string normalization: default port stripped, host lowercased, query
	// sorted, fragment dropped, path case preserved. is_internal is classified on the
	// same normalized host, so an uppercase same-origin host is internal (not external).
	messy, messyOK := byTgt["https://example.com/Path?a=1&b=2"]
	assert.True(t, messyOK, "messy URL stored in fully normalized form")
	assert.True(t, messy.IsInternal, "uppercase same-origin host classified internal, not external")

	ext := byTgt["https://other.com/x"]
	assert.False(t, ext.IsInternal)
	assert.True(t, ext.Nofollow)
	assert.True(t, ext.Sponsored)
	assert.False(t, ext.UGC)

	ugc := byTgt["https://ugc.com/y"]
	assert.True(t, ugc.UGC)
	assert.False(t, ugc.Nofollow)

	img := byTgt["https://example.com/gallery"]
	assert.True(t, img.IsImage, "anchor wrapping an <img> is flagged is_image")

	assert.False(t, seo.PageLinksTruncated)
}

// Absolute internal links whose host differs from the page only by case or a trailing FQDN
// dot must classify as internal: the stored target is normalized (lowercased, dot stripped) so
// it hash-matches the target page, and is_internal must agree or the internal inlink is hidden.
func TestCaptureLinks_NonCanonicalHostStillInternal(t *testing.T) {
	html := `<html><body>
		<a href="https://EXAMPLE.COM/upper">Upper</a>
		<a href="https://example.com./dotted">Dotted</a>
		<a href="https://other.com/ext">Ext</a>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	byTgt := capturedByTarget(seo.PageLinks)
	assert.True(t, byTgt["https://example.com/upper"].IsInternal, "uppercase host is same-origin")
	assert.True(t, byTgt["https://example.com/dotted"].IsInternal, "trailing-dot host is same-origin")
	assert.False(t, byTgt["https://other.com/ext"].IsInternal)

	assert.Equal(t, 2, seo.LinksInternal, "both non-canonical-host links counted internal")
	assert.Equal(t, 1, seo.LinksExternal)
}

func TestCaptureLinks_DedupMergesFlagsAndAnchor(t *testing.T) {
	// Same target three times: no anchor, then sponsored w/ anchor, then nofollow.
	html := `<html><body>
		<a href="https://example.com/p"></a>
		<a href="https://example.com/p" rel="sponsored">Second</a>
		<a href="https://example.com/p" rel="nofollow">Third</a>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1, "three edges to one target dedupe to one PageLink")
	l := seo.PageLinks[0]
	assert.Equal(t, "https://example.com/p", l.Target)
	assert.Equal(t, "Second", l.Anchor, "keeps first non-empty anchor")
	assert.True(t, l.Sponsored)
	assert.True(t, l.Nofollow, "flags ORed across duplicate edges")

	// Aggregate counts still count every <a> (parity unaffected by dedup).
	assert.Equal(t, 3, seo.LinksTotal)
	assert.Equal(t, 3, seo.LinksInternal)
}

func TestCaptureLinks_CapAndTruncation(t *testing.T) {
	var b strings.Builder
	b.WriteString("<html><body>")
	total := types.MaxPageLinks + 25
	for i := 0; i < total; i++ {
		fmt.Fprintf(&b, `<a href="https://example.com/p%d">L%d</a>`, i, i)
	}
	b.WriteString("</body></html>")

	doc := parseGoQueryDoc(t, b.String())
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	assert.Len(t, seo.PageLinks, types.MaxPageLinks, "distinct targets capped at MaxPageLinks")
	assert.True(t, seo.PageLinksTruncated, "truncation recorded, never silent")
	assert.Equal(t, total, seo.LinksTotal, "aggregate count is unaffected by the capture cap")
}

func TestCaptureLinks_AnchorTrimAndCap(t *testing.T) {
	longAnchor := strings.Repeat("a", types.MaxAnchorLength+50)
	html := `<html><body>
		<a href="https://example.com/ws">  spaced   out
		  text </a>
		<a href="https://example.com/long">` + longAnchor + `</a>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	byTgt := capturedByTarget(seo.PageLinks)
	assert.Equal(t, "spaced out text", byTgt["https://example.com/ws"].Anchor, "whitespace collapsed")

	capped := byTgt["https://example.com/long"].Anchor
	assert.Equal(t, types.MaxAnchorLength, len([]rune(capped)), "anchor capped at MaxAnchorLength runes")
}

func TestCaptureLinks_SkippedHrefsNotCaptured(t *testing.T) {
	html := `<html><body>
		<a href="#frag">Frag</a>
		<a href="javascript:void(0)">JS</a>
		<a href="mailto:a@b.com">Mail</a>
		<a href="https://example.com/real">Real</a>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1)
	assert.Equal(t, "https://example.com/real", seo.PageLinks[0].Target)
}

// ExtractPageSEO runs for both render and bypass serve paths (both call
// ProcessContent -> ExtractPageSEO), so verifying capture here covers the
// bypass-served page case too.
func TestExtractPageSEO_CapturesLinks(t *testing.T) {
	html := `<html><head><title>T</title></head><body>
		<a href="/internal">In</a>
		<a href="https://external.com/page" rel="nofollow">Out</a>
	</body></html>`
	doc, err := ParseWithDOM([]byte(html))
	require.NoError(t, err)

	seo := doc.ExtractPageSEO(200, "https://example.com/page")
	require.Len(t, seo.PageLinks, 2)

	byTgt := capturedByTarget(seo.PageLinks)
	assert.True(t, byTgt["https://example.com/internal"].IsInternal)
	assert.False(t, byTgt["https://external.com/page"].IsInternal)
	assert.True(t, byTgt["https://external.com/page"].Nofollow)
}
