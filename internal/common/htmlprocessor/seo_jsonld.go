package htmlprocessor

import (
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	scriptSelector = "script"
	typeAttribute  = "type"

	jsonLDScriptType = "application/ld+json"
	mimeParamsSep    = ";"
)

// collectJSONLDBlocks parses every JSON-LD script in the document exactly once and
// returns the parsed roots in document order. It is the single parse shared by all
// JSON-LD consumers in this package (structured data types, breadcrumbs, dates), so
// a page with many blocks is not re-unmarshaled per consumer.
//
// Numbers decode as json.Number, keeping their source literal: every consumer that
// reads a numeric JSON-LD value must handle json.Number rather than float64.
// Oversized and unparseable blocks are skipped - we cannot enumerate what we cannot
// parse.
func collectJSONLDBlocks(doc *goquery.Document) []interface{} {
	var blocks []interface{}
	doc.Find(scriptSelector).Each(func(_ int, s *goquery.Selection) {
		if !isJSONLDScriptType(getSelectionAttr(s, typeAttribute)) {
			return
		}
		content := s.Text()
		if len(content) > types.MaxJSONLDSize {
			return
		}
		value, ok := decodeJSONLD(content)
		if !ok {
			return
		}
		blocks = append(blocks, value)
	})
	return blocks
}

// isJSONLDScriptType reports whether a script type attribute names JSON-LD. Matching
// is case-insensitive and tolerates surrounding whitespace and MIME parameters
// ("Application/LD+JSON; charset=utf-8"), none of which a goquery attribute selector
// can express.
func isJSONLDScriptType(attr string) bool {
	value := attr
	if i := strings.Index(value, mimeParamsSep); i >= 0 {
		value = value[:i]
	}
	return strings.EqualFold(strings.TrimSpace(value), jsonLDScriptType)
}

// decodeJSONLD decodes one block into an untyped tree. The trailing-content check
// keeps json.Unmarshal's all-or-nothing contract: a decoder alone would accept a
// truncated document followed by garbage.
func decodeJSONLD(content string) (interface{}, bool) {
	dec := json.NewDecoder(strings.NewReader(content))
	dec.UseNumber()

	var value interface{}
	if err := dec.Decode(&value); err != nil {
		return nil, false
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return value, true
}

// extractStructuredDataTypes extracts @type values from parsed JSON-LD blocks.
func extractStructuredDataTypes(blocks []interface{}) []string {
	typeSet := make(map[string]struct{})
	for _, block := range blocks {
		extractTypesFromValue(block, typeSet, 0)
	}
	if len(typeSet) == 0 {
		return nil
	}
	result := make([]string, 0, len(typeSet))
	for t := range typeSet {
		result = append(result, t)
	}
	sort.Strings(result)
	return result
}

// extractTypesFromValue recursively processes JSON values for @type.
func extractTypesFromValue(v interface{}, typeSet map[string]struct{}, depth int) {
	if depth > types.MaxJSONLDRecursionDepth {
		return
	}

	switch val := v.(type) {
	case map[string]interface{}:
		if typeVal, ok := val[jsonLDTypeKey]; ok {
			addType(typeVal, typeSet)
		}
		for _, child := range val {
			extractTypesFromValue(child, typeSet, depth+1)
		}
	case []interface{}:
		for _, item := range val {
			extractTypesFromValue(item, typeSet, depth+1)
		}
	}
}

// addType adds @type value(s) to the set. Handles both string and array types.
func addType(v interface{}, typeSet map[string]struct{}) {
	switch val := v.(type) {
	case string:
		if val != "" {
			typeSet[val] = struct{}{}
		}
	case []interface{}:
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				typeSet[s] = struct{}{}
			}
		}
	}
}
