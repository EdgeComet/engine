package htmlprocessor

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"

	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The capture is the first consumer that re-serializes a decoded JSON-LD tree whole. The
// three derived consumers walk at most types.MaxJSONLDRecursionDepth levels and read a
// handful of known keys, so a page can be hostile in ways none of them can feel: a node
// list wide enough to fill the envelope, a key nobody bounded, block text that never went
// through the JSON decoder and so can still hold invalid UTF-8.
//
// Everything below asserts structure rather than content. A downstream reader indexes
// contexts by block and nodes by node_block, and Phase 5 validation treats an absent node
// as a finding about the page - so an envelope that is internally inconsistent is worse
// than one that is empty, whatever the markup did.

// assertCaptureInvariants checks every structural promise the envelope makes, against any
// input at all. Each is something a reader is entitled to assume without looking.
func assertCaptureInvariants(t *testing.T, capture *types.SchemaOrgCapture) {
	t.Helper()
	require.NotNil(t, capture)

	if capture.Contexts != nil {
		assert.Len(t, capture.Contexts, capture.Blocks, "one context per block, or none at all")
	} else if capture.Blocks > 0 {
		assert.True(t, capture.Truncated, "contexts may only go missing on a truncated envelope")
	}

	require.Len(t, capture.NodeBlock, len(capture.Nodes), "provenance is parallel to nodes")
	previous := -1
	for i, block := range capture.NodeBlock {
		assert.GreaterOrEqual(t, block, 0)
		assert.Less(t, block, capture.Blocks, "node_block[%d] names a block the page had", i)
		assert.GreaterOrEqual(t, block, previous, "nodes stay in document order")
		previous = block
	}

	for i, node := range capture.Nodes {
		assert.True(t, json.Valid(node), "node %d must be valid JSON", i)
		assert.True(t, utf8.Valid(node), "node %d must be valid UTF-8", i)
	}

	assert.LessOrEqual(t, len(capture.Errors), types.MaxSchemaOrgErrors)
	for i, e := range capture.Errors {
		assert.GreaterOrEqual(t, e.Block, 0)
		assert.Less(t, e.Block, capture.Blocks, "errors[%d] names a block the page had", i)
		assert.Contains(t,
			[]string{types.SchemaOrgErrorParse, types.SchemaOrgErrorOversize, types.SchemaOrgErrorShape},
			e.Reason)
		assert.LessOrEqual(t, len(e.Excerpt), types.MaxSchemaOrgErrorExcerpt)
		assert.True(t, utf8.ValidString(e.Excerpt), "errors[%d] excerpt must be valid UTF-8", i)
	}

	for i, context := range capture.Contexts {
		assert.LessOrEqual(t, len(context), types.MaxSchemaOrgContextBytes)
		assert.True(t, utf8.ValidString(context), "contexts[%d] must be valid UTF-8", i)
	}

	encoded, err := json.Marshal(capture)
	require.NoError(t, err, "the envelope must always marshal")
	assert.LessOrEqual(t, len(encoded), types.MaxSchemaOrgBytes)
}

// TestSchemaOrgCapture_InvariantsHoldOnEveryHostileFixture reuses the two fixture sets the
// derived consumers are hardened against - roughly a hundred real-world shapes, from
// truncated template output to binary garbage to a @graph of nulls - and asserts the
// capture's own structural promises on each. The fixtures were written for other
// consumers, which is exactly what makes them a fair test: nothing here was chosen to
// suit the assembly.
func TestSchemaOrgCapture_InvariantsHoldOnEveryHostileFixture(t *testing.T) {
	buckets := map[string]map[string]string{
		"broken syntax":    brokenJSONLDBlocks,
		"unexpected shape": unexpectedShapeBlocks,
	}
	for bucket, fixtures := range buckets {
		for name, block := range fixtures {
			t.Run(bucket+"/"+name, func(t *testing.T) {
				assertCaptureInvariants(t, captureFor(t, block))
			})

			// The same fixture next to valid markup: a page is rarely broken all the way
			// through, and provenance is only falsifiable when more than one block exists.
			t.Run(bucket+"/"+name+" beside a valid block", func(t *testing.T) {
				capture := captureFor(t, block, `{"@context":"https://schema.org","@type":"Product","name":"P"}`)
				assertCaptureInvariants(t, capture)
				require.NotEmpty(t, capture.Nodes, "the valid sibling must survive whatever the broken block did")
				assert.Equal(t, 1, capture.NodeBlock[len(capture.NodeBlock)-1],
					"the last node came from the valid block, so indexes did not shift")
			})
		}
	}
}

// TestSchemaOrgCapture_ExcerptSurvivesBytesTheDecoderNeverSaw pins the one value in the
// envelope that does not come from the JSON decoder. Decoded strings are already valid
// UTF-8 - encoding/json replaces bad bytes as it unquotes - but an excerpt is raw block
// text, so a block of binary reaches it intact. Left alone, each bad byte becomes U+FFFD
// at the event's marshal and a 200-byte excerpt lands in storage as 600.
func TestSchemaOrgCapture_ExcerptSurvivesBytesTheDecoderNeverSaw(t *testing.T) {
	tests := map[string]string{
		"binary garbage":   "\x00\x01\xff\xfe\x7f\x00\x01",
		"utf16 le bom":     "\xff\xfe{\x00\"\x00",
		"long invalid run": strings.Repeat("\xff", 400),
		"truncated rune":   `{"@type":"Thing","name":"` + strings.Repeat("日", 80),
	}

	for name, block := range tests {
		t.Run(name, func(t *testing.T) {
			capture := captureFor(t, block)
			require.Len(t, capture.Errors, 1)
			excerpt := capture.Errors[0].Excerpt

			assert.True(t, utf8.ValidString(excerpt), "excerpt must be valid UTF-8 before it is marshaled")
			assert.LessOrEqual(t, len(excerpt), types.MaxSchemaOrgErrorExcerpt)

			// The bound has to survive the round trip, not just hold in memory.
			encoded, err := json.Marshal(capture.Errors[0])
			require.NoError(t, err)
			var stored types.SchemaOrgError
			require.NoError(t, json.Unmarshal(encoded, &stored))
			assert.LessOrEqual(t, len(stored.Excerpt), types.MaxSchemaOrgErrorExcerpt,
				"a stored excerpt must not exceed the cap the constant advertises")
		})
	}
}

// TestSchemaOrgCapture_AdversarialScale covers the shapes that only threaten the capture:
// the derived consumers stop at ten levels and never re-serialize, so width, key size and
// block count cost them nothing. Each case asserts the same invariants plus what the
// envelope is expected to do with that particular kind of excess.
func TestSchemaOrgCapture_AdversarialScale(t *testing.T) {
	repeatJoin := func(item string, n int) string {
		return strings.TrimSuffix(strings.Repeat(item+",", n), ",")
	}
	manyKeys := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, `"k%d":%d,`, i, i)
		}
		return b.String()
	}
	filler := strings.Repeat("x", 500_000)

	tests := []struct {
		name  string
		block string
		// nodes reports whether any node is expected to be admitted at all.
		nodes     bool
		truncated bool
	}{
		{
			name:      "graph of 50k members fills the envelope",
			block:     `{"@context":"https://schema.org","@graph":[` + repeatJoin(`{"@type":"Thing"}`, 50000) + `]}`,
			nodes:     true,
			truncated: true,
		},
		{
			name:      "bare list of 50k members fills the envelope",
			block:     `[` + repeatJoin(`{"@type":"Thing"}`, 50000) + `]`,
			nodes:     true,
			truncated: true,
		},
		{
			name:      "one node wider than the envelope is admitted whole or not at all",
			block:     `{"@type":"Thing",` + strings.TrimSuffix(manyKeys(50000), ",") + `}`,
			nodes:     false,
			truncated: true,
		},
		{
			name:      "a single 500KB key",
			block:     `{"@type":"Thing","` + filler + `":1}`,
			nodes:     false,
			truncated: true,
		},
		{
			name:      "a single 500KB value",
			block:     `{"@type":"Thing","v":"` + filler + `"}`,
			nodes:     false,
			truncated: true,
		},
		{
			name:      "a 500KB string context does not starve the node budget silently",
			block:     `{"@context":"` + filler + `","@type":"Thing"}`,
			nodes:     false,
			truncated: true,
		},
		{
			name:      "a 500KB object context",
			block:     `{"@context":{"x":"` + filler + `"},"@type":"Thing"}`,
			nodes:     false,
			truncated: true,
		},
		{
			name:  "a list of 50k scalars is one shape error, not 50k",
			block: `[` + repeatJoin(`1`, 50000) + `]`,
			nodes: false,
		},
		{
			name:  "nesting just under the decoder's own cap is re-serialized whole",
			block: strings.Repeat(`{"a":`, 9000) + `1` + strings.Repeat(`}`, 9000),
			nodes: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := captureFor(t, tt.block)
			assertCaptureInvariants(t, capture)
			assert.Equal(t, 1, capture.Blocks)
			if tt.nodes {
				assert.NotEmpty(t, capture.Nodes)
			} else {
				assert.Empty(t, capture.Nodes, "a node that cannot fit is never stored in part")
			}
			assert.Equal(t, tt.truncated, capture.Truncated)
		})
	}

	t.Run("a list of 50k scalars reports one error", func(t *testing.T) {
		capture := captureFor(t, `[`+repeatJoin(`1`, 50000)+`]`)
		require.Len(t, capture.Errors, 1, "one entry per block, however many members were wrong")
		assert.Equal(t, types.SchemaOrgErrorShape, capture.Errors[0].Reason)
	})
}

// TestSchemaOrgCapture_BlockCountIsUnbounded covers the axis with no cap of its own. A
// page can carry any number of script elements, and blocks counts every one, so both the
// node list and the per-block contexts array have to yield before the envelope does.
func TestSchemaOrgCapture_BlockCountIsUnbounded(t *testing.T) {
	for _, count := range []int{1000, 20000, 100000} {
		t.Run(fmt.Sprintf("%d blocks", count), func(t *testing.T) {
			var page strings.Builder
			page.WriteString(`<html><head><title>` + robustnessTitle + `</title>`)
			for i := 0; i < count; i++ {
				page.WriteString(`<script type="application/ld+json">{"@type":"Thing"}</script>`)
			}
			page.WriteString(`</head><body></body></html>`)

			doc, err := ParseWithDOM([]byte(page.String()))
			require.NoError(t, err)
			capture := doc.ExtractPageSEO(200, "https://example.com/probe").SchemaOrg

			assertCaptureInvariants(t, capture)
			assert.Equal(t, count, capture.Blocks, "every block is counted however few are stored")
		})
	}
}

// FuzzSchemaOrgCapture drives the assembly through the real entry point on arbitrary
// script content. The oracle is the invariant set rather than an expected envelope: what
// matters is that no input produces an envelope a reader can be misled by, and that
// nothing takes the render down.
func FuzzSchemaOrgCapture(f *testing.F) {
	seeds := []string{
		`{"@context":"https://schema.org","@type":"Product","offers":{"price":19.990}}`,
		`{"@context":"https://schema.org","@graph":[{"@type":"Organization"},{"@type":"WebSite"}]}`,
		`[{"@type":"Thing"},null,42]`,
		`{"@context":{"@vocab":"https://schema.org/"},"@type":"Thing"}`,
		`{"@type":"Article"} trailing`,
		"\xff\xfe\x00binary",
		`{"a.b":1,"a":{"b":2}}`,
		``,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, block string) {
		doc, err := ParseWithDOM([]byte(probePage(block)))
		if err != nil {
			return
		}
		seo := doc.ExtractPageSEO(200, "https://example.com/probe")
		require.NotNil(t, seo.SchemaOrg, "a parsed document always yields a capture")
		assertCaptureInvariants(t, seo.SchemaOrg)
	})
}

// ownsItsBytes reports whether a's bytes live outside b's allocation. A Go substring
// shares its parent's backing array, which is invisible to every value-based assertion
// and is the difference between a capture that costs 200 bytes per block and one that
// costs a megabyte.
func ownsItsBytes(a, b string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	start := uintptr(unsafe.Pointer(unsafe.StringData(b)))
	at := uintptr(unsafe.Pointer(unsafe.StringData(a)))
	return at < start || at >= start+uintptr(len(b))
}

// TestSchemaOrgCapture_BoundedValuesDoNotPinTheirSource is a memory test, not a
// correctness one, and it exists because the failure it guards cannot be seen by
// comparing values: every excerpt and context is the right length and the right text
// either way.
//
// The capture keeps one context and one excerpt per block, and nothing bounds the block
// count. If either is a substring of the block text or of a decoded @context, one page of
// large blocks holds all of them alive for as long as the event does - through the
// emitter queue, the batcher and any disk spill. That is an out-of-memory path, and
// unlike a panic, the assembly's recover guard cannot catch it: Go terminates the process
// on OOM and on stack exhaustion regardless of any deferred recover.
func TestSchemaOrgCapture_BoundedValuesDoNotPinTheirSource(t *testing.T) {
	const sourceBytes = 1 << 20

	t.Run("excerpt of a block with no whitespace", func(t *testing.T) {
		// Minified JSON and binary garbage collapse to a single field, which is the case
		// where strings.Join hands back its input untouched.
		block := `{"@type":"Thing","v":"` + strings.Repeat("x", sourceBytes)
		excerpt := schemaOrgExcerpt(block)

		require.LessOrEqual(t, len(excerpt), types.MaxSchemaOrgErrorExcerpt)
		assert.True(t, ownsItsBytes(excerpt, block),
			"a %d-byte excerpt must not hold the whole %d-byte block alive", len(excerpt), len(block))
	})

	t.Run("context cut from a huge string", func(t *testing.T) {
		root, err := decodeJSONLD(`{"@context":"` + strings.Repeat("x", sourceBytes) + `","@type":"Thing"}`)
		require.NoError(t, err)
		source := root.(map[string]interface{})[jsonLDContextKey].(string)

		context := schemaOrgContext(root)
		require.LessOrEqual(t, len(context), types.MaxSchemaOrgContextBytes)
		assert.True(t, ownsItsBytes(context, source),
			"a %d-byte context must not hold the whole %d-byte @context alive", len(context), len(source))
	})

	t.Run("context cut from a huge object", func(t *testing.T) {
		capture := captureFor(t, `{"@context":{"x":"`+strings.Repeat("y", sourceBytes)+`"},"@type":"Thing"}`)
		require.Len(t, capture.Contexts, 1)
		assert.LessOrEqual(t, len(capture.Contexts[0]), types.MaxSchemaOrgContextBytes)
	})

	// The whole-page case the two above compose into: many large blocks that yield no
	// node, where the capture is all that outlives the document.
	t.Run("a page of large unparseable blocks", func(t *testing.T) {
		const blocks = 40
		var page strings.Builder
		page.WriteString(`<html><head><title>` + robustnessTitle + `</title>`)
		for i := 0; i < blocks; i++ {
			page.WriteString(`<script type="application/ld+json">{"@type":"Thing","v":"` +
				strings.Repeat("x", sourceBytes/2) + `</script>`)
		}
		page.WriteString(`</head><body></body></html>`)
		source := page.String()

		doc, err := ParseWithDOM([]byte(source))
		require.NoError(t, err)
		capture := doc.ExtractPageSEO(200, "https://example.com/probe").SchemaOrg

		assertCaptureInvariants(t, capture)
		require.Len(t, capture.Errors, types.MaxSchemaOrgErrors)
		for i, e := range capture.Errors {
			assert.True(t, ownsItsBytes(e.Excerpt, source), "errors[%d] excerpt pins the document", i)
		}
		for i, context := range capture.Contexts {
			assert.True(t, ownsItsBytes(context, source), "contexts[%d] pins the document", i)
		}
	})
}

// TestSchemaOrgCapture_RecursionIsBoundedByTheDecoder pins the other fatal path. The
// capture re-serializes whole trees, and json.Marshal recurses per level, so the only
// thing standing between a hostile page and a stack overflow is encoding/json's own
// nesting cap on the way in. A stack overflow is fatal in Go: no recover, no deferred
// anything, the process dies. If a future Go release lifts that cap, this fails.
func TestSchemaOrgCapture_RecursionIsBoundedByTheDecoder(t *testing.T) {
	const decoderMaxNesting = 10000

	nest := func(depth int) string {
		return strings.Repeat(`{"a":`, depth) + `1` + strings.Repeat(`}`, depth)
	}

	t.Run("the decoder refuses anything deeper than its cap", func(t *testing.T) {
		_, err := decodeJSONLD(nest(decoderMaxNesting + 1))
		require.Error(t, err, "nothing deeper than the cap can ever reach json.Marshal")
	})

	t.Run("the deepest tree the decoder admits re-serializes", func(t *testing.T) {
		capture := captureFor(t, nest(decoderMaxNesting-1))
		assertCaptureInvariants(t, capture)
		require.Len(t, capture.Nodes, 1)
		assert.Empty(t, capture.Errors)
	})
}
