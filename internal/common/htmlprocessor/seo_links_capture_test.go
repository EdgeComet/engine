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
	// Same target three times in the SAME placement (one <nav>): identical dom_path, so the
	// (target, dom_path) grain collapses them to one PageLink with merged flags/anchor.
	html := `<html><body><nav>
		<a href="https://example.com/p"></a>
		<a href="https://example.com/p" rel="sponsored">Second</a>
		<a href="https://example.com/p" rel="nofollow">Third</a>
	</nav></body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1, "three same-placement edges to one target dedupe to one PageLink")
	l := seo.PageLinks[0]
	assert.Equal(t, "https://example.com/p", l.Target)
	assert.Equal(t, []string{"nav", "a"}, l.DomPath)
	assert.Equal(t, "Second", l.Anchor, "keeps first non-empty anchor")
	assert.True(t, l.Sponsored)
	assert.True(t, l.Nofollow, "flags ORed across duplicate edges")

	// Aggregate counts still count every <a> (parity unaffected by dedup).
	assert.Equal(t, 3, seo.LinksTotal)
	assert.Equal(t, 3, seo.LinksInternal)
}

func TestCaptureLinks_DualPlacementProducesTwoLinks(t *testing.T) {
	// Same target from two distinct placements (nav vs article) is kept as two PageLinks.
	html := `<html><body>
		<nav><a href="https://example.com/dup">Nav</a></nav>
		<article><a href="https://example.com/dup">Body</a></article>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 2, "two placements of one target -> two PageLinks")
	for _, l := range seo.PageLinks {
		assert.Equal(t, "https://example.com/dup", l.Target)
	}
	assert.Equal(t, []string{"nav", "a"}, seo.PageLinks[0].DomPath, "nav placement first in DOM order")
	assert.Equal(t, []string{"article", "a"}, seo.PageLinks[1].DomPath)
}

func TestCaptureLinks_SixthPlacementDropped(t *testing.T) {
	// One target in six distinct placements: only the first five (DOM order) survive.
	html := `<html><body>
		<div class="a"><a href="https://example.com/t">1</a></div>
		<div class="b"><a href="https://example.com/t">2</a></div>
		<div class="c"><a href="https://example.com/t">3</a></div>
		<div class="d"><a href="https://example.com/t">4</a></div>
		<div class="e"><a href="https://example.com/t">5</a></div>
		<div class="f"><a href="https://example.com/t">6</a></div>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, types.MaxPlacementsPerTarget, "sixth placement dropped at the cap")
	assert.False(t, seo.PageLinksTruncated, "placement-cap drop is not a page-level truncation")
	assert.Equal(t, []string{"div.a", "a"}, seo.PageLinks[0].DomPath)
	assert.Equal(t, []string{"div.e", "a"}, seo.PageLinks[4].DomPath, "fifth kept, sixth (div.f) dropped")
}

func TestBuildDOMPath_SignificantStepFiltering(t *testing.T) {
	// div and li are bare generic containers (skipped); nav and ul are semantic/list (kept bare).
	html := `<html><body><div><nav><ul><li><a href="/x">L</a></li></ul></nav></div></body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1)
	assert.Equal(t, []string{"nav", "ul", "a"}, seo.PageLinks[0].DomPath,
		"bare div and li skipped; nav and ul kept bare; ul > li > a collapses to ul + a")
}

func TestBuildDOMPath_ClassAndAttributeContainersKept(t *testing.T) {
	// A class on an otherwise-generic container makes the step significant.
	html := `<html><body>
		<div class="card"><a href="/d">D</a></div>
		<ul><li class="item"><a href="/z">Z</a></li></ul>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	byTgt := capturedByTarget(seo.PageLinks)
	assert.Equal(t, []string{"div.card", "a"}, byTgt["https://example.com/d"].DomPath, "div.card kept via class")
	assert.Equal(t, []string{"ul", "li.item", "a"}, byTgt["https://example.com/z"].DomPath, "li.item kept via class")
}

func TestBuildDOMPath_ClassSortAndCap(t *testing.T) {
	// Class tokens are lowercased, sanitized, sorted alphabetically, and capped at four.
	html := `<html><body><div class="Zebra alpha MIKE bravo yankee"><a href="/c">C</a></div></body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1)
	assert.Equal(t, []string{"div.alpha.bravo.mike.yankee", "a"}, seo.PageLinks[0].DomPath,
		"five classes sorted, capped to first four, zebra dropped")
}

func TestBuildDOMPath_RoleIncluded(t *testing.T) {
	html := `<html><body><div role="Navigation"><a href="/r">R</a></div></body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1)
	assert.Equal(t, []string{"div[role=navigation]", "a"}, seo.PageLinks[0].DomPath,
		"role makes a bare container significant and lowercases the value")
}

func TestBuildDOMPath_DepthMiddleTruncation(t *testing.T) {
	// 20 nested <section> (all significant) + the <a> = 21 steps -> middle-truncated to 16.
	const depth = 20
	html := "<html><body>" + strings.Repeat("<section>", depth) + `<a href="/deep">D</a>` +
		strings.Repeat("</section>", depth) + "</body></html>"
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1)
	path := seo.PageLinks[0].DomPath
	require.Len(t, path, types.MaxDomPathSteps, "path capped at MaxDomPathSteps")
	assert.Equal(t, "section", path[0], "outermost zone preserved")
	assert.Equal(t, "...", path[types.DomPathHeadSteps], "literal marker sits after the head steps")
	assert.Equal(t, "a", path[len(path)-1], "innermost <a> preserved")
}

func TestBuildDOMPath_Deterministic(t *testing.T) {
	html := `<html><body><main><section class="beta alpha"><nav role="menu"><a href="/x">X</a></nav></section></main></body></html>`

	run := func() []string {
		doc := parseGoQueryDoc(t, html)
		seo := &types.PageSEO{}
		extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)
		require.Len(t, seo.PageLinks, 1)
		return seo.PageLinks[0].DomPath
	}

	assert.Equal(t, run(), run(), "same DOM yields an identical path on repeat")
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

func TestCaptureLinks_WhitespaceHrefJoinsRealTarget(t *testing.T) {
	// A wrapped/padded href must produce the same target as its clean form: same
	// placement plus same target means the two anchors merge into one PageLink.
	html := "<html><body><nav>" +
		`<a href="/about">Clean</a>` +
		"<a href=\"  /ab\tout\r\n  \">Padded</a>" +
		"</nav></body></html>"
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1, "padded href resolves to the same target and merges")
	assert.Equal(t, "https://example.com/about", seo.PageLinks[0].Target)
	assert.Equal(t, "Clean", seo.PageLinks[0].Anchor)
	assert.Equal(t, 2, seo.LinksTotal, "both anchors still counted")
	assert.Equal(t, 2, seo.LinksInternal)
}

func TestCaptureLinks_NonWebSchemesExcluded(t *testing.T) {
	html := `<html><body>
		<a href="whatsapp://send?text=hi">Chat</a>
		<a href="ftp://files.example.org/pub">FTP</a>
		<a href="data:text/html,<b>x</b>">Data</a>
		<a href="javascript:void(0)">JS</a>
		<a href="mailto:a@b.com">Mail</a>
		<a href="tel:+1234567890">Phone</a>
		<a href="https://other.com/real">Real</a>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1, "only the http(s) link is a link edge")
	assert.Equal(t, "https://other.com/real", seo.PageLinks[0].Target)

	assert.Equal(t, 1, seo.LinksTotal, "non-web schemes are not links at all")
	assert.Equal(t, 1, seo.LinksExternal)
	assert.Equal(t, map[string]int{"other.com": 1}, seo.ExternalDomains,
		"an opaque scheme body never reaches the external-domain counts")
}

func TestCaptureLinks_NonWebBaseHrefExcluded(t *testing.T) {
	// The scheme is decided by resolution: a relative href under a non-web base is not
	// a link either, even though the href itself declares no scheme.
	html := `<html><body><a href="page.html">Rel</a></body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "ftp://files.example.org/pub/", "https://example.com/", seo)

	assert.Empty(t, seo.PageLinks)
	assert.Equal(t, 0, seo.LinksTotal)
}

func TestCaptureLinks_UnnormalizableTargetDropped(t *testing.T) {
	// A dotless host cannot be normalized, so its hash would never join a page row.
	// The aggregate counts still see the anchor; only the captured edge is dropped.
	html := `<html><body>
		<a href="https://intranet/page">Intranet</a>
		<a href="https://other.com/ok">Ok</a>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1, "unnormalizable target dropped instead of stored raw")
	assert.Equal(t, "https://other.com/ok", seo.PageLinks[0].Target)
	assert.Equal(t, 2, seo.LinksTotal, "aggregate counts unaffected")
}

func TestBuildDOMPath_MultiTokenRoleUsesFirstToken(t *testing.T) {
	html := `<html><body><div role="  navigation banner ">
		<a href="/r">R</a>
	</div></body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1)
	assert.Equal(t, []string{"div[role=navigation]", "a"}, seo.PageLinks[0].DomPath,
		"a role list keeps its first value instead of collapsing into one unmatchable token")
}

func TestBuildDOMPath_StepTruncationDropsWholeTokens(t *testing.T) {
	// Two 20-char classes fit next to "div"; the third and fourth do not and are dropped
	// whole, while the role still fits in what they leave behind. A step must never end
	// mid-token: a severed "[role=" never matches, and a half class token can match a
	// SHORTER rule token instead.
	const tokenLen = 20
	classA := strings.Repeat("a", tokenLen)
	classB := strings.Repeat("b", tokenLen)
	classC := strings.Repeat("c", tokenLen)
	classD := strings.Repeat("d", tokenLen)

	html := `<html><body><div class="` + classA + " " + classB + " " + classC + " " + classD +
		`" role="navigation"><a href="/t">T</a></div></body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1)
	step := seo.PageLinks[0].DomPath[0]
	assert.Equal(t, "div."+classA+"."+classB+"[role=navigation]", step)
	assert.LessOrEqual(t, len(step), types.MaxDomPathStepLength, "step stays within the budget")
}

func TestBuildDOMPath_OverlongTokenDroppedNotSevered(t *testing.T) {
	longID := strings.Repeat("i", types.MaxDomPathStepLength)
	html := `<html><body><div id="` + longID + `"><a href="/o">O</a></div></body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 1)
	assert.Equal(t, []string{"div", "a"}, seo.PageLinks[0].DomPath,
		"an id that cannot fit is dropped whole, and the container stays significant")
}

func TestBuildDOMPath_SharedAncestorsProduceIdenticalPaths(t *testing.T) {
	html := `<html><body>
		<div class="wrap"><nav class="menu">
			<a href="/one">One</a>
			<a href="/two">Two</a>
		</nav></div>
		<div class="wrap"><nav class="menu"><a href="/three">Three</a></nav></div>
	</body></html>`
	doc := parseGoQueryDoc(t, html)
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, 3)
	want := []string{"div.wrap", "nav.menu", "a"}
	for _, l := range seo.PageLinks {
		assert.Equal(t, want, l.DomPath, "shared ancestors render the same step for every anchor")
	}
}

func TestCaptureLinks_TruncationSignalSurvivesSkippedWalk(t *testing.T) {
	// Past the page budget the ancestor walk is skipped for a brand-new target, but the
	// truncation signal must still be recorded - it is the only evidence the page had
	// more links than were stored.
	var b strings.Builder
	b.WriteString("<html><body><nav>")
	for i := 0; i < types.MaxPageLinks; i++ {
		fmt.Fprintf(&b, `<a href="https://example.com/p%d">L%d</a>`, i, i)
	}
	b.WriteString(`</nav><div class="late"><a href="https://example.com/fresh">Fresh</a></div>`)
	b.WriteString(`<div class="late"><a href="https://example.com/p0">Repeat</a></div>`)
	b.WriteString("</body></html>")

	doc := parseGoQueryDoc(t, b.String())
	seo := &types.PageSEO{}
	extractLinkMetrics(doc, "https://example.com/", "https://example.com/", seo)

	require.Len(t, seo.PageLinks, types.MaxPageLinks)
	assert.True(t, seo.PageLinksTruncated, "truncation recorded even when the walk is skipped")

	byTgt := capturedByTarget(seo.PageLinks)
	_, freshOK := byTgt["https://example.com/fresh"]
	assert.False(t, freshOK, "a new target past the budget is not stored")
	assert.Equal(t, []string{"nav", "a"}, byTgt["https://example.com/p0"].DomPath,
		"the placement captured before the budget ran out is untouched")
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
