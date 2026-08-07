package htmlprocessor

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boilerplateSelector is how the excluded subtrees were named while extractBodyWords
// deleted them from a clone of the body. It survives only in the oracle below.
const boilerplateSelector = "nav, header, footer, aside, form, script, style, noscript"

// cloneBasedBodyWords is the implementation extractBodyWords replaced: deep-copy the
// body, delete the boilerplate subtrees from the copy, take the text. It is kept as an
// oracle because the two must agree on every input forever - the token stream is a
// stored format, and equality with the implementation that produced the first real
// fingerprints is the strongest statement available that nothing moved.
func cloneBasedBodyWords(doc *goquery.Document) []string {
	body := doc.Find("body")
	if body.Length() == 0 {
		return nil
	}
	clone := body.Clone()
	clone.Find(boilerplateSelector).Remove()
	fields := strings.Fields(clone.Text())
	if len(fields) == 0 {
		return nil
	}
	for i, w := range fields {
		fields[i] = strings.ToLower(w)
	}
	return fields
}

// equivalenceCases target each way the walk and the clone could disagree: subtree
// removal at depth and nested within itself, tag-name matching that a type selector
// performs case-sensitively on the parsed name, foreign content whose tag names
// collide with excluded ones, node types that carry no text, and the boundary cases
// around an absent or empty body.
var equivalenceCases = map[string]string{
	"plain":             `<body><p>Hello World Again</p></body>`,
	"every_excluded":    `<body><nav>a b</nav><header>c</header><footer>d</footer><aside>e</aside><form>f</form><script>g</script><style>h</style><noscript>i</noscript><p>keep</p></body>`,
	"nested_same_tag":   `<body><nav>outer <nav>inner</nav> tail</nav><p>keep this</p></body>`,
	"excluded_at_depth": `<body><div><section><form><label>label words</label></form>outside words</section></div></body>`,
	"fuses_around_gap":  `<body><p>left<nav>NAVIGATION</nav>right</p></body>`,
	"fuses_inline":      `<body><p>hyper<b>text</b>markup</p></body>`,
	"fuses_across_br":   `<body><p>one<br>two</p></body>`,
	"uppercase_tags":    `<body><NAV>DROP</NAV><P>KEEP THIS</P></body>`,
	"mixed_case_tags":   `<body><NaV>drop</NaV><p>keep</p></body>`,
	"svg_style":         `<body><svg><style>.c{fill:red}</style><text>svg words</text></svg><p>html words</p></body>`,
	"svg_title":         `<body><svg><title>svg title</title></svg><p>body words</p></body>`,
	"comments":          `<body><!-- comment words --><p>real words</p></body>`,
	"entities":          `<body><p>caf&eacute; a &amp; b &lt; c&nbsp;d</p></body>`,
	"whitespace_only":   `<body>   <nav>x y</nav>   </body>`,
	"empty_body":        `<body></body>`,
	"no_body":           `<html><head><title>t</title></head></html>`,
	"table":             `<body><table><tr><td>cell one</td><td>cell two</td></tr></table></body>`,
	"header_in_table":   `<body><table><thead><tr><th>head cell</th></tr></thead><tbody><tr><td>body cell</td></tr></tbody></table></body>`,
	"malformed":         `<body><p>unclosed <div>mixed <span>nesting</p></div></body>`,
	"template":          `<body><template><p>templated</p></template><p>plain</p></body>`,
	"iframe_srcdoc":     `<body><iframe srcdoc="<p>inner</p>"></iframe><p>outer words</p></body>`,
	"adjacent_list":     `<body><ul><li>List item one</li><li>List item two</li></ul></body>`,
}

func TestExtractBodyWords_MatchesCloneBasedOracle(t *testing.T) {
	for name, markup := range equivalenceCases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t,
				cloneBasedBodyWords(parseGoQueryDoc(t, markup)),
				extractBodyWords(parseGoQueryDoc(t, markup)),
				"skipping boilerplate in place must produce exactly what deleting it from a clone produced")
		})
	}
}

func TestExtractBodyWords_MatchesCloneBasedOracleOnGoldenDocument(t *testing.T) {
	assert.Equal(t,
		cloneBasedBodyWords(parseGoQueryDoc(t, goldenDocument)),
		extractBodyWords(parseGoQueryDoc(t, goldenDocument)))
}

const (
	equivalencePageCards   = 400
	equivalenceVocabLength = 6
)

// buildMarkupDensePage imitates a JavaScript-rendered page: many wrapper elements per
// unit of text, which is both the shape the renderer sees most often and the shape
// where copying the node tree costs the most.
func buildMarkupDensePage(cards int) string {
	rng := rand.New(rand.NewSource(1))
	vocab := [equivalenceVocabLength]string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}
	word := func() string { return vocab[rng.Intn(len(vocab))] }

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><head><title>Dense</title></head><body>`)
	sb.WriteString(`<header><nav><ul>`)
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&sb, `<li><a href="/n/%d">Nav %d</a></li>`, i, i)
	}
	sb.WriteString(`</ul></nav></header><main>`)
	for c := 0; c < cards; c++ {
		fmt.Fprintf(&sb, `<div class="card" data-i="%d"><div class="inner"><h3>%s %s</h3>`+
			`<p>%s <b>%s</b>%s and %s</p><ul><li>%s</li><li>%s</li></ul></div></div>`,
			c, word(), word(), word(), word(), word(), word(), word(), word())
	}
	sb.WriteString(`</main><aside><p>Sidebar</p></aside><footer><p>Colophon</p></footer>`)
	sb.WriteString(`<script>var s = "excluded";</script></body></html>`)
	return sb.String()
}

func TestExtractBodyWords_MatchesCloneBasedOracleOnDensePage(t *testing.T) {
	markup := buildMarkupDensePage(equivalencePageCards)

	want := cloneBasedBodyWords(parseGoQueryDoc(t, markup))
	require.NotEmpty(t, want)
	assert.Equal(t, want, extractBodyWords(parseGoQueryDoc(t, markup)))
}

// BenchmarkExtractBodyWords pairs with BenchmarkExtractBodyWordsCloneBased so the
// cost of the replaced implementation stays measurable rather than remembered.
func BenchmarkExtractBodyWords(b *testing.B) {
	doc := parseGoQueryDoc(b, buildMarkupDensePage(equivalencePageCards))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractBodyWords(doc)
	}
}

func BenchmarkExtractBodyWordsCloneBased(b *testing.B) {
	doc := parseGoQueryDoc(b, buildMarkupDensePage(equivalencePageCards))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cloneBasedBodyWords(doc)
	}
}
