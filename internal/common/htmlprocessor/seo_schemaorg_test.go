package htmlprocessor

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The capture is the only record of what structured data a bot actually received, so it
// has to be faithful in both directions: every node the page carried survives byte for
// byte, and every block that yielded no node leaves evidence saying why. Downstream
// readers treat an absent node as a missing-markup finding, which makes a silently
// dropped block worse than a recorded failure.
//
// These tests drive ParseWithDOM + ExtractPageSEO, the real entry point, so the capture
// is exercised through the same single parse the derived consumers share.

// captureFor extracts the envelope of a page carrying the given JSON-LD blocks.
func captureFor(t *testing.T, blocks ...string) *types.SchemaOrgCapture {
	t.Helper()
	doc, err := ParseWithDOM([]byte(probePage(blocks...)))
	require.NoError(t, err)
	seo := doc.ExtractPageSEO(200, "https://example.com/probe")
	require.NotNil(t, seo)
	require.NotNil(t, seo.SchemaOrg, "a parsed document always yields a capture")
	return seo.SchemaOrg
}

// nodeTypes reads the @type of each captured node. Node bytes cannot be compared to a
// source literal directly: Go's decoder loses key order, so only values are stable.
func nodeTypes(t *testing.T, capture *types.SchemaOrgCapture) []string {
	t.Helper()
	out := make([]string, 0, len(capture.Nodes))
	for i, node := range capture.Nodes {
		var decoded map[string]interface{}
		require.NoError(t, json.Unmarshal(node, &decoded), "node %d must be valid JSON", i)
		text, _ := decoded["@type"].(string)
		out = append(out, text)
	}
	return out
}

// errorReasons pairs each error entry's block with its reason, which is what the shape
// cases assert; offsets and excerpts are checked where they carry information.
func errorReasons(capture *types.SchemaOrgCapture) []string {
	out := make([]string, 0, len(capture.Errors))
	for _, e := range capture.Errors {
		out = append(out, fmt.Sprintf("%d:%s", e.Block, e.Reason))
	}
	return out
}

func encodedLen(t *testing.T, capture *types.SchemaOrgCapture) int {
	t.Helper()
	encoded, err := json.Marshal(capture)
	require.NoError(t, err)
	return len(encoded)
}

// TestSchemaOrgCapture_BlockShapes pins which roots are unwrapped and which are stored
// whole. Unwrapping is the one place the capture is allowed to restructure a page's
// markup, so its boundary matters: a bare list and a pure {@context, @graph} wrapper
// expand into their members, and everything else stays exactly one node.
func TestSchemaOrgCapture_BlockShapes(t *testing.T) {
	tests := []struct {
		name       string
		block      string
		wantNodes  []string
		wantErrors []string
		note       string
	}{
		{
			name:      "object root",
			block:     `{"@context":"https://schema.org","@type":"Product","name":"Oltens"}`,
			wantNodes: []string{"Product"},
		},
		{
			name:      "bare array root",
			block:     `[{"@type":"Product"},{"@type":"BreadcrumbList"}]`,
			wantNodes: []string{"Product", "BreadcrumbList"},
			note:      "a list root is a list of nodes, not a node",
		},
		{
			name:      "pure graph wrapper expands",
			block:     `{"@context":"https://schema.org","@graph":[{"@type":"Organization"},{"@type":"WebSite"}]}`,
			wantNodes: []string{"Organization", "WebSite"},
			note:      "the Yoast shape: the wrapper carries nothing of its own",
		},
		{
			name:      "graph beside another key stays whole",
			block:     `{"@context":"https://schema.org","@type":"WebPage","@graph":[{"@type":"Organization"}]}`,
			wantNodes: []string{"WebPage"},
			note:      "unwrapping here would discard the wrapper's own properties",
		},
		{
			name:      "graph that is not a list stays whole",
			block:     `{"@context":"https://schema.org","@graph":{"@type":"Organization"}}`,
			wantNodes: []string{""},
			note:      "the wrapper is not a list of nodes, so it is stored as one node",
		},
		{
			name:       "non object list members",
			block:      `[{"@type":"Product"},"oops",42,null,{"@type":"Offer"}]`,
			wantNodes:  []string{"Product", "Offer"},
			wantErrors: []string{"0:shape"},
			note:       "three bad members are one finding about one block, not three",
		},
		{
			name:       "scalar root",
			block:      `"just a string"`,
			wantErrors: []string{"0:shape"},
		},
		{
			name:       "null root",
			block:      `null`,
			wantErrors: []string{"0:shape"},
			note:       "null decodes without error, so it is a shape failure and not a parse one",
		},
		{
			name:  "empty list root",
			block: `[]`,
			note:  "a block with nothing in it is neither a node nor a failure",
		},
		{
			name:  "empty graph",
			block: `{"@context":"https://schema.org","@graph":[]}`,
		},
		{
			name:      "empty object root",
			block:     `{}`,
			wantNodes: []string{""},
			note:      "an object with no properties is still markup the page served",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := captureFor(t, tt.block)

			assert.Equal(t, 1, capture.Blocks)
			assert.Equal(t, tt.wantNodes, emptyToNil(nodeTypes(t, capture)), tt.note)
			assert.Equal(t, tt.wantErrors, emptyToNil(errorReasons(capture)), tt.note)
			assert.False(t, capture.Truncated)
			assert.Len(t, capture.NodeBlock, len(capture.Nodes), "node_block is parallel to nodes")
		})
	}
}

// emptyToNil lets a case leave wantNodes/wantErrors unset to mean "none", instead of
// every case spelling out an empty slice.
func emptyToNil(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}

// TestSchemaOrgCapture_NodesAreStoredVerbatim is the contract downstream validation
// rests on: a node is evidence of what the page served, so nothing about it may be
// rewritten on the way out. Numbers are the case that breaks first - a float64 round
// trip silently turns a price of 19.990 into 19.99 and a 19-digit identifier into
// exponent notation.
func TestSchemaOrgCapture_NodesAreStoredVerbatim(t *testing.T) {
	// name holds U+00A0, not a space: the excerpt path collapses whitespace and a node
	// must not, or a price written "1 299" reaches storage as a different number.
	const block = `{"@type":"Product","price":19.990,"sku":1234567890123456789,"tiny":1e-7,"name":"a b"}`

	capture := captureFor(t, block)
	require.Len(t, capture.Nodes, 1)

	node := string(capture.Nodes[0])
	assert.Contains(t, node, `"price":19.990`, "the source literal must survive, not its float64 value")
	assert.Contains(t, node, `"sku":1234567890123456789`)
	assert.Contains(t, node, `"tiny":1e-7`)
	assert.Contains(t, node, "a\u00a0b", "node text is stored uncollapsed")
	assert.JSONEq(t, block, node, "no value is dropped or altered")
}

// TestSchemaOrgCapture_NodeBlockPointsAtSourceBlock covers the provenance array on the
// page shape that makes it load-bearing: several blocks, one of them broken, one of them
// contributing two nodes. Without it a reader cannot say which script tag to fix.
func TestSchemaOrgCapture_NodeBlockPointsAtSourceBlock(t *testing.T) {
	capture := captureFor(t,
		`{"@context":"https://schema.org","@type":"Product"}`,
		`{"@type":"Broken",`,
		`{"@context":"https://schema.org","@graph":[{"@type":"Organization"},{"@type":"WebSite"}]}`,
	)

	assert.Equal(t, 3, capture.Blocks)
	assert.Equal(t, []string{"Product", "Organization", "WebSite"}, nodeTypes(t, capture))
	assert.Equal(t, []int{0, 2, 2}, capture.NodeBlock)
	assert.Equal(t, []string{"1:parse"}, errorReasons(capture))
	assert.Len(t, capture.Contexts, capture.Blocks)
}

// TestSchemaOrgCapture_Contexts covers the per-block @context slot. It is what tells a
// reader whether a node is schema.org vocabulary at all, and it is stored per block
// rather than per node because a wrapper's context applies to every node it held.
func TestSchemaOrgCapture_Contexts(t *testing.T) {
	longValue := strings.Repeat("v", types.MaxSchemaOrgContextBytes*2)

	t.Run("string context", func(t *testing.T) {
		capture := captureFor(t, `{"@context":"https://schema.org","@type":"Product"}`)
		assert.Equal(t, []string{"https://schema.org"}, capture.Contexts)
	})

	t.Run("object context is serialized and cut", func(t *testing.T) {
		capture := captureFor(t, `{"@context":{"@vocab":"`+longValue+`"},"@type":"Product"}`)

		require.Len(t, capture.Contexts, 1)
		assert.Len(t, capture.Contexts[0], types.MaxSchemaOrgContextBytes)
		assert.True(t, strings.HasPrefix(capture.Contexts[0], `{"@vocab":"vvv`),
			"the cut keeps the head, which is the part that identifies the vocabulary")
	})

	t.Run("list context is serialized and cut", func(t *testing.T) {
		capture := captureFor(t, `{"@context":["https://schema.org",{"x":"`+longValue+`"}],"@type":"Product"}`)

		require.Len(t, capture.Contexts, 1)
		assert.Len(t, capture.Contexts[0], types.MaxSchemaOrgContextBytes)
		assert.True(t, strings.HasPrefix(capture.Contexts[0], `["https://schema.org"`))
	})

	// A megabyte-long string context would otherwise be metadata the node budget has to
	// pay for, so the cut applies to every form, not only the serialized ones.
	t.Run("oversized string context is cut too", func(t *testing.T) {
		capture := captureFor(t, `{"@context":"`+longValue+`","@type":"Product"}`)

		require.Len(t, capture.Contexts, 1)
		assert.Len(t, capture.Contexts[0], types.MaxSchemaOrgContextBytes)
	})

	t.Run("absent and unusable contexts are empty", func(t *testing.T) {
		capture := captureFor(t,
			`{"@type":"Product"}`,
			`[{"@type":"Product"}]`,
			`{"@context":42,"@type":"Product"}`,
		)
		assert.Equal(t, []string{"", "", ""}, capture.Contexts)
	})

	// The slot is positional, so a failed block has to keep its place or every context
	// after it points at the wrong script tag.
	t.Run("a failed block keeps its slot", func(t *testing.T) {
		capture := captureFor(t,
			`{"@context":"https://first.example","@type":"Product"}`,
			`{"@type":"Broken",`,
			`{"@context":"https://third.example","@type":"Offer"}`,
		)

		require.Len(t, capture.Contexts, capture.Blocks)
		assert.Equal(t, []string{"https://first.example", "", "https://third.example"}, capture.Contexts)
	})
}

// TestSchemaOrgCapture_Errors covers the three failure reasons. The evidence is the
// point: a reader who cannot see the offending text cannot tell a truncated template
// from a double-encoded blob.
func TestSchemaOrgCapture_Errors(t *testing.T) {
	t.Run("syntax error carries an offset and an excerpt", func(t *testing.T) {
		const block = `{"@type":"A",,}`
		capture := captureFor(t, block)

		require.Len(t, capture.Errors, 1)
		assert.Equal(t, types.SchemaOrgErrorParse, capture.Errors[0].Reason)
		assert.Equal(t, int64(14), capture.Errors[0].Offset, "the byte the decoder rejected")
		assert.Equal(t, block, capture.Errors[0].Excerpt)
		assert.Empty(t, capture.Nodes)
	})

	// A decoder alone accepts the first value and ignores the rest, which would store a
	// node from a block a browser's JSON-LD reader rejects outright.
	t.Run("trailing content is a parse failure at the value end", func(t *testing.T) {
		const block = `{"@type":"A"} <!-- oops -->`
		capture := captureFor(t, block)

		require.Len(t, capture.Errors, 1)
		assert.Equal(t, types.SchemaOrgErrorParse, capture.Errors[0].Reason)
		assert.Equal(t, int64(13), capture.Errors[0].Offset, "where the first value ended")
		assert.Empty(t, capture.Nodes)
	})

	// encoding/json reports unexpected EOF without a position, and the decoder's own
	// input offset is 0 there, so a cut-off block records no offset at all. omitempty
	// then drops the key, which is the honest reading: the position is unknown.
	t.Run("a truncated block records no offset", func(t *testing.T) {
		capture := captureFor(t, `{"@type":"A",`)

		require.Len(t, capture.Errors, 1)
		assert.Equal(t, types.SchemaOrgErrorParse, capture.Errors[0].Reason)
		assert.Zero(t, capture.Errors[0].Offset)

		encoded, err := json.Marshal(capture.Errors[0])
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), `"offset"`)
	})

	// The block is VALID JSON, so a node is the observable proof of a decode. Getting
	// none is what shows the size guard ran before the decoder, not after it.
	t.Run("an oversize block is never decoded", func(t *testing.T) {
		oversize := `{"@type":"Product","description":"` +
			strings.Repeat("x", types.MaxJSONLDSize) + `"}`
		capture := captureFor(t, oversize, `{"@type":"Offer"}`)

		assert.Equal(t, 2, capture.Blocks)
		assert.Equal(t, []string{"Offer"}, nodeTypes(t, capture), "only the sibling becomes a node")
		require.Len(t, capture.Errors, 1)
		assert.Equal(t, types.SchemaOrgErrorOversize, capture.Errors[0].Reason)
		assert.Zero(t, capture.Errors[0].Offset)
		assert.Len(t, capture.Errors[0].Excerpt, types.MaxSchemaOrgErrorExcerpt)
	})

	t.Run("excerpts are cut and collapsed to one line", func(t *testing.T) {
		block := "{\n\t\"@type\":\t\"A\",\n\t\"name\":\t\"" +
			strings.Repeat("n", types.MaxSchemaOrgErrorExcerpt) + "\",,}"
		capture := captureFor(t, block)

		require.Len(t, capture.Errors, 1)
		excerpt := capture.Errors[0].Excerpt
		assert.LessOrEqual(t, len(excerpt), types.MaxSchemaOrgErrorExcerpt)
		assert.NotContains(t, excerpt, "\n")
		assert.NotContains(t, excerpt, "\t")
		assert.True(t, strings.HasPrefix(excerpt, `{ "@type": "A", "name": "nnn`), excerpt)
	})
}

// TestSchemaOrgCapture_ErrorsAreCappedButBlocksAreNot pins the pair that keeps a broken
// CMS from filling the column: the evidence list is bounded, the block count is not, so
// a reader always sees how much markup the page carried even when most of the failures
// were dropped.
func TestSchemaOrgCapture_ErrorsAreCappedButBlocksAreNot(t *testing.T) {
	const brokenBlocks = 30

	blocks := make([]string, 0, brokenBlocks)
	for i := 0; i < brokenBlocks; i++ {
		blocks = append(blocks, fmt.Sprintf(`{"@type":"Item%d",`, i))
	}

	capture := captureFor(t, blocks...)

	assert.Equal(t, brokenBlocks, capture.Blocks)
	assert.Len(t, capture.Errors, types.MaxSchemaOrgErrors)
	assert.Empty(t, capture.Nodes)
	assert.LessOrEqual(t, encodedLen(t, capture), types.MaxSchemaOrgBytes)

	for i, e := range capture.Errors {
		assert.Equal(t, i, e.Block, "the kept entries are the first ones, in block order")
	}
}

// TestSchemaOrgCapture_PathologicalContextsAreDropped covers the one metadata that grows
// without bound: one @context slot per block, on a page with enough blocks. Sizing the
// metadata before admitting nodes is what keeps the envelope under the cap here, and
// dropping the whole array (rather than half of it) keeps the remaining slots from
// pointing at the wrong block.
func TestSchemaOrgCapture_PathologicalContextsAreDropped(t *testing.T) {
	const blockCount = 1400
	context := strings.Repeat("c", types.MaxSchemaOrgContextBytes)

	blocks := make([]string, 0, blockCount)
	for i := 0; i < blockCount; i++ {
		blocks = append(blocks, `{"@context":"`+context+`","@type":"Thing"}`)
	}

	capture := captureFor(t, blocks...)

	assert.Equal(t, blockCount, capture.Blocks)
	assert.Nil(t, capture.Contexts, "the contexts array alone did not fit")
	assert.True(t, capture.Truncated, "dropped metadata makes the envelope incomplete")
	assert.LessOrEqual(t, encodedLen(t, capture), types.MaxSchemaOrgBytes)
}

// contextOutcome is a block that parsed to one node carrying the given @context. The
// @context slots are the only metadata a page grows without bound, so they are what a
// test pushes against the cap with.
func contextOutcome(context string) jsonLDOutcome {
	return jsonLDOutcome{root: map[string]interface{}{
		jsonLDContextKey: context,
		"@type":          "Thing",
	}}
}

// schemaOrgMetadataSize reports what these blocks cost before a single node is admitted,
// which is the quantity the node budget is measured against.
func schemaOrgMetadataSize(t *testing.T, outcomes []jsonLDOutcome) int {
	t.Helper()
	contexts := make([]string, len(outcomes))
	for i, outcome := range outcomes {
		contexts[i] = schemaOrgContext(outcome.root)
	}
	return encodedLen(t, &types.SchemaOrgCapture{Blocks: len(outcomes), Contexts: contexts})
}

// outcomesWithMetadataSize builds blocks whose metadata serializes to exactly size
// bytes: equal blocks to get within one block of the target, then one padded block to
// land on the byte. Their contexts are half length so that one block costs less than the
// padding range, which is what lets the last block absorb whatever the others left.
func outcomesWithMetadataSize(t *testing.T, size int) []jsonLDOutcome {
	t.Helper()
	coarse := contextOutcome(strings.Repeat("c", types.MaxSchemaOrgContextBytes/2))
	perBlock := schemaOrgMetadataSize(t, []jsonLDOutcome{coarse, coarse}) -
		schemaOrgMetadataSize(t, []jsonLDOutcome{coarse})

	outcomes := make([]jsonLDOutcome, size/perBlock)
	for i := range outcomes {
		outcomes[i] = coarse
	}
	outcomes = append(outcomes, contextOutcome(""))
	for schemaOrgMetadataSize(t, outcomes) > size {
		require.Greater(t, len(outcomes), 1, "target too small for a single block")
		outcomes = append(outcomes[:len(outcomes)-2], outcomes[len(outcomes)-1])
	}

	pad := size - schemaOrgMetadataSize(t, outcomes)
	require.LessOrEqual(t, pad, types.MaxSchemaOrgContextBytes, "the last block must absorb the remainder")
	outcomes[len(outcomes)-1] = contextOutcome(strings.Repeat("c", pad))
	require.Equal(t, size, schemaOrgMetadataSize(t, outcomes))
	return outcomes
}

// TestSchemaOrgCapture_CapHoldsWhenMetadataFillsTheEnvelope covers the window the node
// boundary test cannot reach: metadata that on its own lands within a few dozen bytes of
// the cap. The assembly still has to add the two node members and, once it stops
// admitting, the truncated flag, so a reserve taken after the metadata is judged is a
// reserve taken too late: the envelope goes over by the flag's own bytes while carrying
// no node at all. slack is exactly what the metadata left for everything else, which is
// why the fallback boundary can be asserted against it.
//
// Synthetic outcomes rather than a page, because the target here is a byte count and the
// HTML that produces it is thousands of blocks of padding.
func TestSchemaOrgCapture_CapHoldsWhenMetadataFillsTheEnvelope(t *testing.T) {
	for slack := 0; slack <= schemaOrgNodeKeysBytes+schemaOrgTruncatedFlagBytes; slack++ {
		outcomes := outcomesWithMetadataSize(t, types.MaxSchemaOrgBytes-slack)
		capture := buildSchemaOrgCapture(outcomes)
		require.NotNil(t, capture)

		assert.Truef(t, capture.Truncated, "an envelope this full is never complete (slack %d)", slack)
		assert.LessOrEqualf(t, encodedLen(t, capture), types.MaxSchemaOrgBytes,
			"metadata %d bytes short of the cap", slack)

		if slack < schemaOrgTruncatedFlagBytes {
			assert.Nilf(t, capture.Contexts,
				"metadata with no room for its own flag gives up the contexts (slack %d)", slack)
			continue
		}
		assert.NotNilf(t, capture.Contexts, "metadata stays whole once the flag fits (slack %d)", slack)
		assert.Emptyf(t, capture.Nodes, "no node fits beside metadata this large (slack %d)", slack)
	}
}

// TestSchemaOrgCapture_CapStopsAtTheFirstNodeThatDoesNotFit covers the ordinary
// truncation path and its deliberate non-behaviour: admission is document order, so a
// small node after a large one is not pulled forward. Reordering would make node_block
// the only way to read the page, and a partial capture that looks complete is worse than
// one that admits it stopped.
func TestSchemaOrgCapture_CapStopsAtTheFirstNodeThatDoesNotFit(t *testing.T) {
	large := func(name string) string {
		return `{"@type":"` + name + `","description":"` +
			strings.Repeat("x", types.MaxSchemaOrgBytes*3/4) + `"}`
	}

	capture := captureFor(t, large("First"), large("Second"), `{"@type":"Small"}`)

	assert.Equal(t, 3, capture.Blocks)
	assert.Equal(t, []string{"First"}, nodeTypes(t, capture))
	assert.Equal(t, []int{0}, capture.NodeBlock)
	assert.True(t, capture.Truncated)
	assert.Empty(t, capture.Errors, "running out of room is not a defect in the markup")
	assert.LessOrEqual(t, encodedLen(t, capture), types.MaxSchemaOrgBytes)
}

// TestSchemaOrgCapture_CapIsExactAtTheBoundary is the test the cap arithmetic exists
// for. Truncated says the node list is incomplete, but the byte guarantee is the one the
// storage column depends on, and it is the one an off-by-a-few budget breaks without
// changing any flag: an envelope assembled from a budget that ignores the bytes its own
// nodes and node_block members cost overruns the cap only at the exact boundary.
//
// The boundary is searched for rather than hard coded, so the test keeps testing the
// boundary when the envelope's fixed cost changes.
func TestSchemaOrgCapture_CapIsExactAtTheBoundary(t *testing.T) {
	padded := func(pad int) string {
		return `{"@type":"Product","description":"` + strings.Repeat("x", pad) + `"}`
	}
	fits := func(pad int) bool {
		return !captureFor(t, padded(pad)).Truncated
	}

	require.True(t, fits(0), "a tiny node must fit, or the search below is meaningless")
	low, high := 0, types.MaxSchemaOrgBytes
	require.False(t, fits(high), "a node the size of the cap cannot fit beside the envelope")
	for high-low > 1 {
		mid := (low + high) / 2
		if fits(mid) {
			low = mid
		} else {
			high = mid
		}
	}

	t.Run("largest node that fits", func(t *testing.T) {
		capture := captureFor(t, padded(low))

		require.Len(t, capture.Nodes, 1)
		assert.False(t, capture.Truncated)

		size := encodedLen(t, capture)
		assert.LessOrEqual(t, size, types.MaxSchemaOrgBytes)
		// The reserve is a fixed cost, not a percentage: a budget that quietly gave away
		// kilobytes would still pass the assertion above.
		assert.Greater(t, size, types.MaxSchemaOrgBytes-64,
			"the budget must spend the cap, not hold most of it back")
	})

	t.Run("one byte more does not fit", func(t *testing.T) {
		capture := captureFor(t, padded(low+1))

		assert.Empty(t, capture.Nodes)
		assert.True(t, capture.Truncated)
		assert.LessOrEqual(t, encodedLen(t, capture), types.MaxSchemaOrgBytes,
			"recording the truncation must not itself push the envelope over")
	})
}

// TestSchemaOrgCapture_PageWithoutJSONLD pins the distinction every consumer reads
// first. An absent capture means no page was inspected (a cache hit, an error); a
// capture of zero blocks means a page was inspected and carries no structured data,
// which is a finding. Blocks therefore has no omitempty and must survive marshaling.
func TestSchemaOrgCapture_PageWithoutJSONLD(t *testing.T) {
	doc, err := ParseWithDOM([]byte(`<html><head><title>Bare</title></head><body><p>No markup</p></body></html>`))
	require.NoError(t, err)

	seo := doc.ExtractPageSEO(200, "https://example.com/bare")
	require.NotNil(t, seo.SchemaOrg)

	encoded, err := json.Marshal(seo.SchemaOrg)
	require.NoError(t, err)
	assert.JSONEq(t, `{"blocks":0}`, string(encoded))
}

// TestSchemaOrgCapture_NonJSONLDScriptsAreNotBlocks keeps the block count meaning what
// its name says. Counting a data island or an inline script would shift every
// node_block index and invent failures for markup that was never JSON-LD.
func TestSchemaOrgCapture_NonJSONLDScriptsAreNotBlocks(t *testing.T) {
	htmlStr := `<html><head><title>Mixed</title>` +
		`<script>var x = {"@type":"NotLD"};</script>` +
		`<script type="application/json">{"@type":"AlsoNotLD"}</script>` +
		`<script type="application/ld+json">{"@type":"Product"}</script>` +
		`</head><body></body></html>`

	doc, err := ParseWithDOM([]byte(htmlStr))
	require.NoError(t, err)
	capture := doc.ExtractPageSEO(200, "https://example.com/mixed").SchemaOrg

	require.NotNil(t, capture)
	assert.Equal(t, 1, capture.Blocks)
	assert.Equal(t, []string{"Product"}, nodeTypes(t, capture))
	assert.Equal(t, []int{0}, capture.NodeBlock)
}

// panicOnMarshal is a value no decoder can produce: it exists only to fire the recover
// guard for real. Driving the guard with a synthetic outcome is the honest option -
// every input the collector can actually hand buildSchemaOrgCapture is a tree of maps,
// slices, strings, bools, nil and json.Number, none of which panics on marshal.
type panicOnMarshal struct{}

func (panicOnMarshal) MarshalJSON() ([]byte, error) {
	panic("marshal exploded")
}

// TestSchemaOrgCapture_PanicGuardDropsTheCapture covers both halves of the assembly,
// because the guard's whole value is that it wraps all of it: a panic while sizing
// metadata and a panic while admitting nodes must each cost the capture and nothing
// else. Losing the envelope is acceptable, failing the render is not.
func TestSchemaOrgCapture_PanicGuardDropsTheCapture(t *testing.T) {
	tests := map[string][]jsonLDOutcome{
		"panic while sizing metadata": {{
			root: map[string]interface{}{jsonLDContextKey: map[string]interface{}{"boom": panicOnMarshal{}}},
		}},
		"panic while admitting nodes": {{
			root: map[string]interface{}{"@type": panicOnMarshal{}},
		}},
	}

	for name, outcomes := range tests {
		t.Run(name, func(t *testing.T) {
			var capture *types.SchemaOrgCapture
			require.NotPanics(t, func() {
				capture = buildSchemaOrgCapture(outcomes)
			})
			assert.Nil(t, capture, "a capture that cannot be assembled is dropped, not half built")
		})
	}
}
