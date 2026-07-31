package htmlprocessor

import (
	"strings"
	"testing"

	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// JSON-LD is attacker-controlled markup and, on real sites, routinely malformed:
// truncated by a failed template render, double-encoded, carrying HTML, or holding a
// type nobody expected under a schema.org key. Every block reaching this package went
// through a browser that does not validate it.
//
// These tests drive the real entry point, ExtractPageSEO, so one hostile block is
// checked against all three JSON-LD consumers at once (structured data types,
// breadcrumbs, dates). Surviving means four things, and "no panic" alone is the
// weakest of them:
//
//  1. no panic, on any input;
//  2. a broken block does not poison the page - valid siblings still extract;
//  3. output stays deterministic, so a retry of the same page cannot disagree;
//  4. no unbounded work - pathological nesting and width terminate.

const (
	// robustnessTitle is the marker every fixture carries. Extraction returning it
	// proves the processor completed rather than bailing out early.
	robustnessTitle = "Robustness Probe"

	// goJSONMaxNestingDepth is encoding/json's own nesting cap. Input past it fails to
	// parse and the block is skipped, which is what keeps decoding from growing the
	// stack without bound. Pinned here because the guarantee is the standard
	// library's, not this package's.
	goJSONMaxNestingDepth = 10000
)

// probePage wraps blocks in a document carrying a title, a breadcrumb-shaped valid
// block is NOT added - callers decide what else is on the page.
func probePage(blocks ...string) string {
	var b strings.Builder
	b.WriteString(`<html><head><title>` + robustnessTitle + `</title>`)
	for _, block := range blocks {
		b.WriteString(`<script type="application/ld+json">`)
		b.WriteString(block)
		b.WriteString(`</script>`)
	}
	b.WriteString(`</head><body><h1>probe</h1></body></html>`)
	return b.String()
}

// extractProbe runs the full extractor and fails the test on a panic rather than
// letting it take the suite down.
func extractProbe(t *testing.T, htmlStr string) {
	t.Helper()
	require.NotPanics(t, func() {
		doc, err := ParseWithDOM([]byte(htmlStr))
		require.NoError(t, err)
		seo := doc.ExtractPageSEO(200, "https://example.com/probe")
		require.NotNil(t, seo)
		assert.Equal(t, robustnessTitle, seo.Title, "processor must complete and still read the rest of the page")
		assert.NotNil(t, seo.Dates, "dates must stay initialized on hostile input")
	})
}

// brokenJSONLDBlocks are blocks that must never parse. Each is a real-world shape:
// template output cut off mid-write, PHP notices interleaved, double encoding, or a
// browser handing back markup where JSON was expected.
var brokenJSONLDBlocks = map[string]string{
	"empty":                   ``,
	"whitespace only":         "   \n\t  ",
	"bare word":               `undefined`,
	"js undefined value":      `{"@type":"Article","datePublished":undefined}`,
	"js single quotes":        `{'@type':'Article','datePublished':'2024-01-01'}`,
	"unquoted keys":           `{@type:"Article"}`,
	"trailing comma object":   `{"@type":"Article","datePublished":"2024-01-01",}`,
	"trailing comma array":    `[{"@type":"Article"},]`,
	"missing closing brace":   `{"@type":"Article","datePublished":"2024-01-01"`,
	"missing closing bracket": `[{"@type":"Article"}`,
	"truncated mid string":    `{"@type":"BreadcrumbList","itemListElement":[{"name":"Ho`,
	"truncated mid key":       `{"@ty`,
	"extra closing brace":     `{"@type":"Article"}}`,
	"two roots concatenated":  `{"@type":"Article"}{"@type":"Product"}`,
	"root then garbage":       `{"@type":"Article"} <!-- oops -->`,
	"php notice prefix":       `<br />Notice: Undefined index in /var/www/x.php on line 3{"@type":"Article"}`,
	"html error page":         `<!DOCTYPE html><html><body>500</body></html>`,
	"nan literal":             `{"@type":"Article","position":NaN}`,
	"infinity literal":        `{"@type":"Article","position":Infinity}`,
	"hex number":              `{"position":0x1F}`,
	"leading zero number":     `{"position":007}`,
	"lone plus number":        `{"position":+1}`,
	"trailing dot number":     `{"position":1.}`,
	"raw newline in string":   "{\"@type\":\"Art\nicle\"}",
	"raw tab in string":       "{\"@type\":\"Art\ticle\"}",
	"binary garbage":          "\x00\x01\xff\xfe\x7f\x00\x01",
	"unterminated escape":     `{"@type":"abc\`,
	"bad escape char":         `{"@type":"a\qb"}`,
	"comment inside":          `{"@type":"Article" /* comment */}`,
	"only opening brace":      `{`,
	"only closing brace":      `}`,
	"colon without value":     `{"@type":}`,
	"array as key":            `{["a"]:"b"}`,
	"cdata wrapper":           `<![CDATA[{"@type":"Article"}]]>`,
	"html comment wrapper":    `<!--{"@type":"Article"}-->`,
	// Valid JSON as written, but the HTML tokenizer ends the script element at the
	// embedded closing tag, so the decoder only ever sees the truncated prefix. It
	// belongs here because of what reaches the parser, not because of what was authored.
	"script close truncates block": `{"@type":"Art</script>icle"}`,
	"bom then broken":              "\ufeff{\"@type\":",
	"utf16 bom binary":             "\xff\xfe{\x00\"\x00",
	"deeper than encoding/json": strings.Repeat("[", goJSONMaxNestingDepth+1) +
		strings.Repeat("]", goJSONMaxNestingDepth+1),
}

func TestJSONLD_BrokenSyntaxIsSkippedNotFatal(t *testing.T) {
	for name, block := range brokenJSONLDBlocks {
		t.Run(name, func(t *testing.T) {
			extractProbe(t, probePage(block))

			// Assert the bucket's premise instead of assuming it. Without this, a block
			// that quietly parses still passes every no-panic check while testing
			// something other than what its name claims - and the HTML tokenizer makes
			// that easy to get wrong, since it rewrites NUL and invalid UTF-8 to U+FFFD
			// before the decoder ever runs.
			doc := parseGoQueryDoc(t, probePage(block))
			assert.Empty(t, collectJSONLDBlocks(doc), "block must not survive parsing")
		})
	}
}

// TestJSONLD_BrokenBlockDoesNotPoisonValidSibling is the property that matters most in
// production: one bad block on a page must not cost the page its good markup. A parser
// that aborted the document, or a walk that stopped at the first error, would pass the
// no-panic tests above and still lose every real signal on the page.
func TestJSONLD_BrokenBlockDoesNotPoisonValidSibling(t *testing.T) {
	const validBlock = `{"@type":"BlogPosting","datePublished":"2024-03-05","dateModified":"2024-04-01"}`

	for name, broken := range brokenJSONLDBlocks {
		t.Run(name+" before valid", func(t *testing.T) {
			doc, err := ParseWithDOM([]byte(probePage(broken, validBlock)))
			require.NoError(t, err)
			seo := doc.ExtractPageSEO(200, "https://example.com/probe")

			assert.Contains(t, seo.StructuredDataTypes, "BlogPosting", "valid sibling types survive")
			require.Len(t, seo.Dates, 2, "valid sibling dates survive")
			assert.Equal(t, "2024-03-05", seo.Dates[0].Raw)
			assert.Equal(t, "2024-04-01", seo.Dates[1].Raw)
		})

		t.Run(name+" after valid", func(t *testing.T) {
			doc, err := ParseWithDOM([]byte(probePage(validBlock, broken)))
			require.NoError(t, err)
			seo := doc.ExtractPageSEO(200, "https://example.com/probe")

			assert.Contains(t, seo.StructuredDataTypes, "BlogPosting")
			require.Len(t, seo.Dates, 2)
		})
	}
}

// unexpectedShapeBlocks parse cleanly but hold a type nobody writing the schema had in
// mind. These are the ones that reach the walkers, so they are where an unchecked type
// assertion would actually fire.
//
// Four entries look like syntax errors and are not, which is why they live here: Go's
// decoder accepts a lone surrogate escape and yields U+FFFD rather than erroring, the
// HTML tokenizer rewrites raw NUL and invalid UTF-8 to U+FFFD before the decoder runs,
// and a double-encoded document is simply a JSON string at the root.
var unexpectedShapeBlocks = map[string]string{
	"double encoded root string": `"{\"@type\":\"Article\",\"datePublished\":\"2024-01-01\"}"`,
	"lone high surrogate":        `{"@type":"\ud800","datePublished":"X"}`,
	"invalid utf8 becomes fffd":  "{\"@type\":\"\xff\xfe\",\"datePublished\":\"X\"}",
	"raw nul becomes fffd":       "{\"@type\":\"Art\x00icle\",\"datePublished\":\"X\"}",

	"root is string":              `"just a string"`,
	"root is number":              `42`,
	"root is bool":                `true`,
	"root is null":                `null`,
	"root is empty object":        `{}`,
	"root is empty array":         `[]`,
	"root is array of nulls":      `[null,null,null]`,
	"root is array of scalars":    `[1,"two",true,null]`,
	"root is deeply nested array": `[[[[[[[[[[[[{"@type":"Article","datePublished":"X"}]]]]]]]]]]]]`,

	"@type is object":            `{"@type":{"@type":"Article"},"datePublished":"X"}`,
	"@type is number":            `{"@type":42,"datePublished":"X"}`,
	"@type is bool":              `{"@type":true,"datePublished":"X"}`,
	"@type is null":              `{"@type":null,"datePublished":"X"}`,
	"@type is empty array":       `{"@type":[],"datePublished":"X"}`,
	"@type is array of nulls":    `{"@type":[null,null],"datePublished":"X"}`,
	"@type is array of objects":  `{"@type":[{"a":1}],"datePublished":"X"}`,
	"@type is nested array":      `{"@type":[["Article"]],"datePublished":"X"}`,
	"@type is empty string":      `{"@type":"","datePublished":"X"}`,
	"@type number then string":   `{"@type":[42,"Article"],"datePublished":"X"}`,
	"@type duplicated key":       `{"@type":"First","@type":"Second","datePublished":"X"}`,
	"@type with nul in value":    "{\"@type\":\"Art\\u0000icle\",\"datePublished\":\"X\"}",
	"@type absurdly long":        `{"@type":"` + strings.Repeat("T", 100000) + `","datePublished":"X"}`,
	"@type is self referencing":  `{"@type":"@type"}`,
	"@context is array":          `{"@context":["https://schema.org",{"x":1}],"@type":"Article","datePublished":"X"}`,
	"@graph is null":             `{"@graph":null,"datePublished":"X"}`,
	"@graph is string":           `{"@graph":"not-an-array"}`,
	"@graph is number":           `{"@graph":7}`,
	"@graph is object":           `{"@graph":{"@type":"Article","datePublished":"X"}}`,
	"@graph nested in @graph":    `{"@graph":{"@graph":[{"@type":"Article","datePublished":"X"}]}}`,
	"@graph of nulls":            `{"@graph":[null,null]}`,
	"@graph of arrays":           `{"@graph":[[{"@type":"Article","datePublished":"X"}]]}`,
	"date value is object":       `{"@type":"Article","datePublished":{"@value":"2024-01-01"}}`,
	"date value is array":        `{"@type":"Article","datePublished":["2024-01-01"]}`,
	"date value is bool":         `{"@type":"Article","datePublished":true}`,
	"date value is null":         `{"@type":"Article","datePublished":null}`,
	"date value is empty string": `{"@type":"Article","datePublished":""}`,
	"date value huge int":        `{"@type":"Article","datePublished":123456789012345678901234567890}`,
	"date value 1e309":           `{"@type":"Article","datePublished":1e309}`,
	"date value negative":        `{"@type":"Article","datePublished":-1709632800}`,
	"date value tiny exponent":   `{"@type":"Article","datePublished":1e-400}`,
	"date key duplicated":        `{"@type":"Article","datePublished":"first","datePublished":"second"}`,
	"skip key holds scalar":      `{"@type":"Product","review":"not-an-object","releaseDate":"X"}`,
	"skip key holds date":        `{"@type":"Product","review":{"datePublished":"hidden"},"releaseDate":"X"}`,
	"itemListElement is string":  `{"@type":"BreadcrumbList","itemListElement":"oops"}`,
	"itemListElement is number":  `{"@type":"BreadcrumbList","itemListElement":42}`,
	"itemListElement of scalars": `{"@type":"BreadcrumbList","itemListElement":[1,"two",null,true]}`,
	"breadcrumb item is array":   `{"@type":"BreadcrumbList","itemListElement":[{"item":["https://e.com/"]}]}`,
	"breadcrumb position 1e309":  `{"@type":"BreadcrumbList","itemListElement":[{"position":1e309,"item":"https://e.com/"}]}`,
	"breadcrumb position huge":   `{"@type":"BreadcrumbList","itemListElement":[{"position":99999999999999999999,"item":"https://e.com/"}]}`,
	"breadcrumb url is javascript": `{"@type":"BreadcrumbList","itemListElement":` +
		`[{"position":1,"name":"x","item":"javascript:alert(1)"}]}`,
	"key is empty string": `{"":"empty key","@type":"Article","datePublished":"X"}`,
	"key with nul":        "{\"a\\u0000b\":1,\"@type\":\"Article\",\"datePublished\":\"X\"}",
	// The closing tag is JSON-escaped so the HTML tokenizer does not end the script
	// element at it. Written unescaped, the block is truncated before the decoder sees
	// it and this shape is never exercised against a walker at all.
	"value holds script tag":   `{"@type":"Article","datePublished":"<script>alert(1)<\/script>"}`,
	"value holds html entity":  `{"@type":"Article","datePublished":"&lt;&amp;&gt;"}`,
	"value holds rtl override": "{\"@type\":\"Article\",\"datePublished\":\"2024\\u202e01-01\"}",
	"value is 1mb string": `{"@type":"Article","datePublished":"` +
		strings.Repeat("x", 200000) + `"}`,
}

func TestJSONLD_UnexpectedShapesAreTolerated(t *testing.T) {
	for name, block := range unexpectedShapeBlocks {
		t.Run(name, func(t *testing.T) {
			extractProbe(t, probePage(block))
		})
	}
}

// TestJSONLD_UnexpectedShapeDoesNotWipeValidSibling exists because a no-panic assertion
// cannot see a panic in two of the three consumers: extractDates and extractBreadcrumbs
// each recover() internally and degrade to an empty result. A nil dereference in either
// walk would therefore leave every no-panic test and the fuzzer green while silently
// costing production every date on the page.
//
// Pairing each hostile shape with a known-good block closes that hole. These shapes do
// reach the walkers - unlike the syntax-error bucket, which never gets past the decoder -
// so this is the check that actually exercises them. If a walk dies and recovers, the
// sibling's dates vanish and this fails.
func TestJSONLD_UnexpectedShapeDoesNotWipeValidSibling(t *testing.T) {
	const validBlock = `{"@type":"BlogPosting","datePublished":"2024-03-05","dateModified":"2024-04-01"}`

	rawValues := func(dates []types.DateCandidate) []string {
		out := make([]string, 0, len(dates))
		for _, d := range dates {
			out = append(out, d.Raw)
		}
		return out
	}

	for name, shape := range unexpectedShapeBlocks {
		for _, order := range []struct {
			label  string
			blocks []string
		}{
			{"shape first", []string{shape, validBlock}},
			{"shape second", []string{validBlock, shape}},
		} {
			t.Run(name+" "+order.label, func(t *testing.T) {
				doc, err := ParseWithDOM([]byte(probePage(order.blocks...)))
				require.NoError(t, err)
				seo := doc.ExtractPageSEO(200, "https://example.com/probe")

				assert.Contains(t, rawValues(seo.Dates), "2024-03-05",
					"a recovered panic in the date walk would drop the valid sibling's dates")
				assert.Contains(t, rawValues(seo.Dates), "2024-04-01")
				assert.Contains(t, seo.StructuredDataTypes, "BlogPosting")
			})
		}
	}
}

// TestJSONLD_HostileInputIsDeterministic guards the Go map-iteration trap on exactly
// the inputs most likely to expose it: objects with many sibling keys, where any walk
// that leaked map order would reorder candidates between runs. A retry of the same page
// must never disagree with the first attempt.
func TestJSONLD_HostileInputIsDeterministic(t *testing.T) {
	var wide strings.Builder
	wide.WriteString(`{"@type":"Article"`)
	for i := 0; i < 200; i++ {
		wide.WriteString(`,"k`)
		wide.WriteString(strings.Repeat("z", i%7+1))
		wide.WriteString(string(rune('a' + i%26)))
		wide.WriteString(`":{"@type":"Sub","dateCreated":"c`)
		wide.WriteString(string(rune('a' + i%26)))
		wide.WriteString(`"}`)
	}
	wide.WriteString(`,"datePublished":"P","dateModified":"M"}`)

	pages := []string{
		probePage(wide.String()),
		probePage(`{"@graph":[{"@type":"A","datePublished":"1"},{"@type":"B","dateModified":"2"}]}`),
		probePage(unexpectedShapeBlocks["@type is array of objects"], wide.String()),
	}

	for i, page := range pages {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			doc, err := ParseWithDOM([]byte(page))
			require.NoError(t, err)
			first := doc.ExtractPageSEO(200, "https://example.com/probe")

			for run := 0; run < 20; run++ {
				reDoc, err := ParseWithDOM([]byte(page))
				require.NoError(t, err)
				again := reDoc.ExtractPageSEO(200, "https://example.com/probe")

				assert.Equal(t, first.Dates, again.Dates, "dates must not depend on map iteration order")
				assert.Equal(t, first.StructuredDataTypes, again.StructuredDataTypes)
				assert.Equal(t, first.Breadcrumbs, again.Breadcrumbs)
			}
		})
	}
}

// TestJSONLD_PathologicalStructure covers shapes designed to exhaust something: stack
// through nesting, heap through width, or CPU through repetition. Nesting past
// encoding/json's cap must fail to parse rather than grow the stack, and nesting under
// it must terminate at this package's own recursion limit.
func TestJSONLD_PathologicalStructure(t *testing.T) {
	nest := func(depth int, leaf string) string {
		return strings.Repeat(`{"a":`, depth) + leaf + strings.Repeat(`}`, depth)
	}

	tests := map[string]string{
		"nested objects just under encoding/json cap": nest(goJSONMaxNestingDepth-2, `{"@type":"Article","datePublished":"deep"}`),
		"nested objects past encoding/json cap":       nest(goJSONMaxNestingDepth+10, `1`),
		"nested arrays just under cap": strings.Repeat(`[`, goJSONMaxNestingDepth-2) + `1` +
			strings.Repeat(`]`, goJSONMaxNestingDepth-2),
		"nested graph keys":  strings.Repeat(`{"@graph":`, 500) + `{"@type":"Article","datePublished":"g"}` + strings.Repeat(`}`, 500),
		"wide array":         `[` + strings.TrimSuffix(strings.Repeat(`{"@type":"Product","releaseDate":"r"},`, 5000), ",") + `]`,
		"wide object":        wideObject(5000),
		"many empty objects": `[` + strings.TrimSuffix(strings.Repeat(`{},`, 20000), ",") + `]`,
		"unbalanced nesting": strings.Repeat(`{"a":`, 5000),
		"alternating types":  `[` + strings.TrimSuffix(strings.Repeat(`{"@type":["A",42,null,{"b":1}]},`, 2000), ",") + `]`,
	}

	for name, block := range tests {
		t.Run(name, func(t *testing.T) {
			extractProbe(t, probePage(block))
		})
	}
}

// TestJSONLD_NestingLimitsAreLoadBearing pins the two limits that together keep a
// nesting bomb from growing the stack, because neither is obvious from reading the
// walkers and both are easy to lose silently.
//
// encoding/json refuses to decode past its own cap, so absurd nesting never becomes a
// tree at all. That cap is what keeps the process safe. Below it the tree does get
// built, and MaxJSONLDRecursionDepth stops the walk from following it down; that second
// limit bounds work rather than preventing a crash, since Go grows stacks on demand and
// ten thousand frames would not overflow one on its own.
func TestJSONLD_NestingLimitsAreLoadBearing(t *testing.T) {
	arrayNest := func(d int) string { return strings.Repeat(`[`, d) + `1` + strings.Repeat(`]`, d) }

	t.Run("decoder accepts nesting at the cap", func(t *testing.T) {
		_, ok := decodeJSONLD(arrayNest(goJSONMaxNestingDepth))
		assert.True(t, ok, "encoding/json should still decode at exactly its cap")
	})

	t.Run("decoder refuses nesting past the cap", func(t *testing.T) {
		_, ok := decodeJSONLD(arrayNest(goJSONMaxNestingDepth + 1))
		assert.False(t, ok, "one level past the cap must fail to parse, not grow the stack")
	})

	// The positive control matters: without it, "no dates from a deep tree" also passes
	// when date collection is broken outright, or when the walk panicked and recovered.
	// Pairing shallow-collected against deep-not-collected pins the depth limit itself.
	objectNest := func(d int) string {
		return strings.Repeat(`{"a":`, d) + `{"@type":"Article","datePublished":"buried"}` + strings.Repeat(`}`, d)
	}

	t.Run("control: a date within the walk limit is collected", func(t *testing.T) {
		shallow := objectNest(types.MaxJSONLDRecursionDepth - 2)
		parsed, ok := decodeJSONLD(shallow)
		require.True(t, ok)

		doc := parseGoQueryDoc(t, probePage(shallow))
		require.Len(t, extractDates(doc, []interface{}{parsed}), 1,
			"collection must work at shallow depth, otherwise the deep case proves nothing")
	})

	t.Run("walk stops long before a parsed tree bottoms out", func(t *testing.T) {
		deep := objectNest(goJSONMaxNestingDepth - 2)

		parsed, ok := decodeJSONLD(deep)
		require.True(t, ok, "this depth must parse, or the test is not exercising the walk limit")

		doc := parseGoQueryDoc(t, probePage(deep))
		assert.Empty(t, extractDates(doc, []interface{}{parsed}),
			"a date past MaxJSONLDRecursionDepth is not collected, so the walk never recurses that far")
	})
}

func wideObject(keys int) string {
	var b strings.Builder
	b.WriteString(`{"@type":"Article"`)
	for i := 0; i < keys; i++ {
		b.WriteString(`,"key`)
		b.WriteString(strings.Repeat("0", i%3))
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(strings.Repeat("1", i%5))
		b.WriteString(`":"v"`)
	}
	b.WriteString(`}`)
	return b.String()
}

// TestJSONLD_ManyBrokenBlocks mirrors a broken CMS emitting one malformed block per
// listing item. The page must still cost bounded work and still surface the one block
// that parses.
func TestJSONLD_ManyBrokenBlocks(t *testing.T) {
	blocks := make([]string, 0, 501)
	for i := 0; i < 500; i++ {
		blocks = append(blocks, `{"@type":"Product","releaseDate":`)
	}
	blocks = append(blocks, `{"@type":"BlogPosting","datePublished":"2024-03-05"}`)

	doc, err := ParseWithDOM([]byte(probePage(blocks...)))
	require.NoError(t, err)

	var seo = doc.ExtractPageSEO(200, "https://example.com/probe")
	assert.Equal(t, []string{"BlogPosting"}, seo.StructuredDataTypes)
	require.Len(t, seo.Dates, 1)
	assert.Equal(t, "2024-03-05", seo.Dates[0].Raw)
}

// TestJSONLD_OversizedBlockSkippedWithoutParsing pins the size guard: a block past
// MaxJSONLDSize never reaches the decoder, so an enormous body cannot be turned into an
// enormous tree. A valid neighbour still extracts.
func TestJSONLD_OversizedBlockSkippedWithoutParsing(t *testing.T) {
	oversized := `{"@type":"Article","datePublished":"` + strings.Repeat("x", 1024*1024+64) + `"}`

	doc, err := ParseWithDOM([]byte(probePage(oversized, `{"@type":"BlogPosting","datePublished":"kept"}`)))
	require.NoError(t, err)
	seo := doc.ExtractPageSEO(200, "https://example.com/probe")

	assert.Equal(t, []string{"BlogPosting"}, seo.StructuredDataTypes, "oversized block contributes nothing")
	require.Len(t, seo.Dates, 1)
	assert.Equal(t, "kept", seo.Dates[0].Raw)
}

// TestJSONLD_ScriptTypeVariantsOnBrokenContent checks the type-attribute matcher itself
// against hostile attribute values. A block is only parsed when its type says JSON-LD,
// and a bizarre attribute must not change that decision or panic the matcher.
func TestJSONLD_ScriptTypeVariantsOnBrokenContent(t *testing.T) {
	attrs := []struct {
		attr      string
		isJSONLD  bool
		assertion string
	}{
		{`application/ld+json`, true, "canonical"},
		{`APPLICATION/LD+JSON`, true, "case-insensitive"},
		{`  application/ld+json  `, true, "surrounding whitespace"},
		{`application/ld+json; charset=utf-8`, true, "MIME parameter"},
		{`application/ld+json;charset="utf-8";boundary=x`, true, "several MIME parameters"},
		{`;`, false, "bare separator"},
		{``, false, "empty"},
		{`application/json`, false, "plain JSON is not JSON-LD"},
		{`text/javascript`, false, "script"},
		{`application/ld+json extra`, false, "trailing junk is not a parameter"},
		{strings.Repeat("a", 10000), false, "absurdly long"},
		{"application/ld+json\x00", false, "trailing NUL"},
		{"\xff\xfe", false, "binary"},
	}

	// The block is VALID, so the accept/reject decision is observable. With broken
	// content both outcomes look identical and the test would pass even if the matcher
	// returned a constant.
	const block = `{"@type":"MatcherProbe","datePublished":"2024-01-01"}`

	for _, tc := range attrs {
		t.Run(tc.assertion, func(t *testing.T) {
			htmlStr := `<html><head><title>` + robustnessTitle + `</title><script type="` +
				tc.attr + `">` + block + `</script></head><body></body></html>`
			extractProbe(t, htmlStr)

			doc := parseGoQueryDoc(t, htmlStr)
			if tc.isJSONLD {
				assert.Len(t, collectJSONLDBlocks(doc), 1, "attribute names JSON-LD, block must be parsed")
			} else {
				assert.Empty(t, collectJSONLDBlocks(doc), "attribute does not name JSON-LD, block must be ignored")
			}
		})
	}
}

// FuzzExtractPageSEOJSONLD fuzzes the whole extractor rather than one consumer, so a
// crash anywhere in the shared parse or any of the three walks is caught. Run with
// -fuzz=FuzzExtractPageSEOJSONLD to explore beyond the seed corpus.
func FuzzExtractPageSEOJSONLD(f *testing.F) {
	seeds := []string{
		`{"@type":"Article","datePublished":"2024-01-01"}`,
		`{"@graph":[{"@type":"BreadcrumbList","itemListElement":[{"position":1,"item":"/"}]}]}`,
		`[{"@type":["A","B"],"dateModified":123}]`,
		`{"@type":"Product","review":{"datePublished":"x"},"releaseDate":1e309}`,
		`{`,
		`null`,
		"\x00\xff\xfe",
		`{"@type":"Article","datePublished":undefined}`,
		strings.Repeat(`[`, 64) + strings.Repeat(`]`, 64),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, block string) {
		doc, err := ParseWithDOM([]byte(probePage(block)))
		if err != nil {
			return
		}
		seo := doc.ExtractPageSEO(200, "https://example.com/probe")
		if seo == nil {
			t.Fatal("ExtractPageSEO returned nil")
		}
		if seo.Dates == nil {
			t.Fatal("dates must never be nil after extraction")
		}
		for _, d := range seo.Dates {
			if d.Source == "" || d.Field == "" {
				t.Fatalf("candidate with empty source/field: %+v", d)
			}
		}
	})
}
