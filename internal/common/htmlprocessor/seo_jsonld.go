package htmlprocessor

import (
	"encoding/json"
	"sort"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/pkg/types"
)

// extractStructuredDataTypes extracts @type values from JSON-LD scripts.
func extractStructuredDataTypes(doc *goquery.Document) []string {
	typeSet := make(map[string]struct{})
	doc.Find("script[type='application/ld+json']").Each(func(_ int, s *goquery.Selection) {
		content := s.Text()
		if len(content) > types.MaxJSONLDSize {
			return
		}
		extractTypesFromJSON([]byte(content), typeSet, 0)
	})
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

// extractTypesFromJSON recursively extracts @type values from JSON.
func extractTypesFromJSON(data []byte, typeSet map[string]struct{}, depth int) {
	if depth > types.MaxJSONLDRecursionDepth {
		return
	}

	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return
	}

	extractTypesFromValue(obj, typeSet, depth)
}

// extractTypesFromValue recursively processes JSON values for @type.
func extractTypesFromValue(v interface{}, typeSet map[string]struct{}, depth int) {
	if depth > types.MaxJSONLDRecursionDepth {
		return
	}

	switch val := v.(type) {
	case map[string]interface{}:
		if typeVal, ok := val["@type"]; ok {
			addType(typeVal, typeSet)
		}
		if graphVal, ok := val["@graph"]; ok {
			extractTypesFromValue(graphVal, typeSet, depth+1)
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
