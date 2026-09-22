package htmlprocessor

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/edgecomet/engine/pkg/types"
)

const (
	jsonLDContextKey = "@context"
	jsonLDGraphKey   = "@graph"

	// What one admitted node adds beyond its own bytes and its node_block literal: the
	// separator in each of the two arrays. Charged per node, which overstates the total
	// by two bytes, since the first entry of each array has no separator.
	schemaOrgNodeSeparatorBytes = 2

	// The two node members cost this much before a single node is written: `"nodes":[],`
	// is 11 bytes and `"node_block":[],` is 16. Reserved up front because the budget is
	// measured on an envelope that carries neither.
	schemaOrgNodeKeysBytes = 27

	// `,"truncated":true` is appended after the budget is fixed, so its 17 bytes are
	// reserved too: an envelope that spent its budget exactly would otherwise overrun
	// the cap at the moment truncation is recorded.
	schemaOrgTruncatedFlagBytes = 17
)

// schemaOrgCandidate is a node the classification pass accepted, with the block it came
// from. Every candidate is collected before the node budget is computed, so the
// metadata is final by then and the cap is exact in a single pass.
type schemaOrgCandidate struct {
	block int
	node  interface{}
}

// schemaOrgBlockIssues records what a block's classification refused. A block can hit both:
// a list holding a scalar next to an object that nests too deep.
type schemaOrgBlockIssues struct {
	malformed bool
	tooDeep   bool
}

// buildSchemaOrgCapture assembles the page's JSON-LD envelope from one outcome per
// script block. Metadata (block count, per-block @context, failure evidence) is
// assembled and sized first, then nodes fill whatever of types.MaxSchemaOrgBytes is
// left, in document order; the first node that does not fit sets Truncated and ends the
// list. Node content is never rewritten, and numbers keep the json.Number literals the
// decoder read, so "19.990" leaves the gateway as "19.990".
//
// All input is untrusted: on a panic the capture is dropped and the render proceeds.
func buildSchemaOrgCapture(outcomes []jsonLDOutcome) (capture *types.SchemaOrgCapture) {
	defer func() {
		if r := recover(); r != nil {
			capture = nil
		}
	}()

	capture = &types.SchemaOrgCapture{Blocks: len(outcomes)}
	if len(outcomes) == 0 {
		return capture
	}

	contexts := make([]string, len(outcomes))
	candidates := make([]schemaOrgCandidate, 0, len(outcomes))
	var errs []types.SchemaOrgError
	for i, outcome := range outcomes {
		contexts[i] = schemaOrgContext(outcome.root)
		if outcome.reason != "" {
			errs = appendSchemaOrgError(errs, i, outcome.reason, outcome.offset, outcome.excerpt)
			continue
		}
		var issues schemaOrgBlockIssues
		candidates, issues = appendSchemaOrgCandidates(candidates, i, outcome.root)
		if issues.malformed {
			errs = appendSchemaOrgError(errs, i, types.SchemaOrgErrorShape, 0, outcome.excerpt)
		}
		if issues.tooDeep {
			errs = appendSchemaOrgError(errs, i, types.SchemaOrgErrorDepth, 0, outcome.excerpt)
		}
	}
	capture.Contexts = contexts
	capture.Errors = errs

	// The flag is reserved before the metadata is judged, not after: metadata that
	// leaves less room than the flag costs has nowhere to record that it truncated.
	budget := schemaOrgNodeBudget(capture) - schemaOrgTruncatedFlagBytes
	if budget < 0 {
		// One context per block against an unbounded block count is the only metadata
		// that can overrun the cap on its own. Without it the envelope is the block
		// count plus at most MaxSchemaOrgErrors entries, which is bounded.
		capture.Contexts = nil
		capture.Truncated = true
		// The flag is part of the measured envelope now, so only the node members are
		// still unaccounted for.
		budget = schemaOrgNodeBudget(capture)
	}
	budget -= schemaOrgNodeKeysBytes

	for _, candidate := range candidates {
		encoded, err := json.Marshal(candidate.node)
		if err != nil {
			// A tree the decoder produced always marshals back; if one somehow does not,
			// the node list is incomplete and has to say so.
			capture.Truncated = true
			continue
		}
		cost := len(encoded) + schemaOrgNodeSeparatorBytes + len(strconv.Itoa(candidate.block))
		if cost > budget {
			capture.Truncated = true
			break
		}
		budget -= cost
		capture.Nodes = append(capture.Nodes, encoded)
		capture.NodeBlock = append(capture.NodeBlock, candidate.block)
	}
	return capture
}

// appendSchemaOrgCandidates classifies one parsed root into the nodes it contributes.
// Only a bare list and a pure {@context, @graph} wrapper are unwrapped: a node that
// carries an @graph alongside other keys stays one whole node. The returned issues report what
// the block held that could not be stored - a member that is not a node, or one nesting past
// types.MaxSchemaOrgNodeDepth - each as one error per block however many members were wrong.
func appendSchemaOrgCandidates(candidates []schemaOrgCandidate, block int, root interface{}) ([]schemaOrgCandidate, schemaOrgBlockIssues) {
	switch value := root.(type) {
	case []interface{}:
		return appendSchemaOrgMembers(candidates, block, value)
	case map[string]interface{}:
		if graph, ok := pureGraphWrapper(value); ok {
			return appendSchemaOrgMembers(candidates, block, graph)
		}
		if exceedsSchemaOrgDepth(value, types.MaxSchemaOrgNodeDepth) {
			return candidates, schemaOrgBlockIssues{tooDeep: true}
		}
		return append(candidates, schemaOrgCandidate{block: block, node: value}), schemaOrgBlockIssues{}
	default:
		return candidates, schemaOrgBlockIssues{malformed: true}
	}
}

// appendSchemaOrgMembers takes the object elements of an unwrapped list. An empty list
// contributes no node and no error: it is a block with nothing in it.
func appendSchemaOrgMembers(candidates []schemaOrgCandidate, block int, members []interface{}) ([]schemaOrgCandidate, schemaOrgBlockIssues) {
	var issues schemaOrgBlockIssues
	for _, member := range members {
		object, ok := member.(map[string]interface{})
		if !ok {
			issues.malformed = true
			continue
		}
		if exceedsSchemaOrgDepth(object, types.MaxSchemaOrgNodeDepth) {
			issues.tooDeep = true
			continue
		}
		candidates = append(candidates, schemaOrgCandidate{block: block, node: object})
	}
	return candidates, issues
}

// exceedsSchemaOrgDepth reports whether v nests past allowed container levels, counting a scalar
// as 0 and an object or list as one more than its deepest member. It returns at the first path
// that passes the bound, so a pathologically deep tree costs the bound rather than its own size.
func exceedsSchemaOrgDepth(v interface{}, allowed int) bool {
	switch value := v.(type) {
	case map[string]interface{}:
		if allowed <= 0 {
			return true
		}
		for _, child := range value {
			if exceedsSchemaOrgDepth(child, allowed-1) {
				return true
			}
		}
	case []interface{}:
		if allowed <= 0 {
			return true
		}
		for _, item := range value {
			if exceedsSchemaOrgDepth(item, allowed-1) {
				return true
			}
		}
	}
	return false
}

// pureGraphWrapper returns the @graph list of a root that carries nothing but @context
// and @graph.
func pureGraphWrapper(root map[string]interface{}) ([]interface{}, bool) {
	for key := range root {
		if key != jsonLDContextKey && key != jsonLDGraphKey {
			return nil, false
		}
	}
	graph, ok := root[jsonLDGraphKey].([]interface{})
	return graph, ok
}

// appendSchemaOrgError records one block failure, at most types.MaxSchemaOrgErrors per
// page. Blocks still counts every block, so a capped error list never hides how much
// markup the page carried.
func appendSchemaOrgError(errs []types.SchemaOrgError, block int, reason string, offset int64, excerpt string) []types.SchemaOrgError {
	if len(errs) >= types.MaxSchemaOrgErrors {
		return errs
	}
	return append(errs, types.SchemaOrgError{
		Block:   block,
		Reason:  reason,
		Offset:  offset,
		Excerpt: excerpt,
	})
}

// schemaOrgNodeBudget reports the bytes left for nodes once the metadata already on the
// capture is serialized. A marshal failure reads as no room at all rather than as an
// unbounded envelope.
func schemaOrgNodeBudget(capture *types.SchemaOrgCapture) int {
	encoded, err := json.Marshal(capture)
	if err != nil {
		return -1
	}
	return types.MaxSchemaOrgBytes - len(encoded)
}

// schemaOrgContext reports a block's root @context as the envelope stores it: a string
// as written, an object or list as its compact JSON text, "" for every other value and
// for every block whose root is not an object. Every form is cut to
// types.MaxSchemaOrgContextBytes, an adversarial page's megabyte-long string @context
// included, because the metadata is sized before any node is admitted.
func schemaOrgContext(root interface{}) string {
	object, ok := root.(map[string]interface{})
	if !ok {
		return ""
	}
	value, ok := object[jsonLDContextKey]
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return truncateBytes(text, types.MaxSchemaOrgContextBytes)
	}
	switch value.(type) {
	case map[string]interface{}, []interface{}:
		encoded, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return truncateBytes(string(encoded), types.MaxSchemaOrgContextBytes)
	}
	return ""
}

// schemaOrgExcerpt keeps the head of a block's text as evidence: at most
// types.MaxSchemaOrgErrorExcerpt bytes, whitespace collapsed so the excerpt reads as
// one line.
//
// Block text is the one value here that never went through the JSON decoder, so it is
// the only one that can hold invalid UTF-8. Those bytes are replaced now rather than at
// the event's marshal, which would silently turn each one into three and store a
// 200-byte excerpt as 600. The head is cut twice because the replacement can grow it.
func schemaOrgExcerpt(text string) string {
	head := strings.ToValidUTF8(truncateBytes(text, types.MaxSchemaOrgErrorExcerpt), string(utf8.RuneError))
	return collapseWhitespace(truncateBytes(head, types.MaxSchemaOrgErrorExcerpt))
}

// truncateBytes cuts a string to at most maxBytes, never mid-rune.
//
// The cut is copied, not sliced. A Go substring shares its parent's allocation, so an
// excerpt sliced from a megabyte block, or a context sliced from a megabyte @context,
// would hold that megabyte alive for as long as the event does - one per block, with
// nothing bounding the block count.
func truncateBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.Clone(s[:cut])
}
