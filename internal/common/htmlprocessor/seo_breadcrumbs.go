package htmlprocessor

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/pkg/types"
)

// extractBreadcrumbs locates the first BreadcrumbList in JSON-LD scripts
// (document order) and returns its items as ordered BreadcrumbEntry slice.
// All input is treated as untrusted; the function returns nil on any malformed
// or unrecognized input and never panics on hostile JSON.
func extractBreadcrumbs(doc *goquery.Document, pageURL string) (result []types.BreadcrumbEntry) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
		}
	}()

	var list map[string]interface{}
	doc.Find("script[type='application/ld+json']").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		content := s.Text()
		if len(content) > types.MaxJSONLDSize {
			return true
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			return true
		}
		list = findFirstBreadcrumbList(parsed, 0)
		return list == nil
	})

	if list == nil {
		return nil
	}
	return buildBreadcrumbEntries(list, pageURL)
}

// findFirstBreadcrumbList walks JSON-LD in declaration order looking for an
// object with @type "BreadcrumbList". Recursion is limited to arrays and the
// @graph wrapper to keep traversal deterministic (Go map iteration is not
// ordered, so we deliberately do not recurse through arbitrary object keys).
func findFirstBreadcrumbList(v interface{}, depth int) map[string]interface{} {
	if depth > types.MaxJSONLDRecursionDepth {
		return nil
	}
	switch val := v.(type) {
	case map[string]interface{}:
		if isBreadcrumbListType(val["@type"]) {
			return val
		}
		if g, ok := val["@graph"]; ok {
			return findFirstBreadcrumbList(g, depth+1)
		}
	case []interface{}:
		for _, item := range val {
			if found := findFirstBreadcrumbList(item, depth+1); found != nil {
				return found
			}
		}
	}
	return nil
}

// isBreadcrumbListType reports whether @type names BreadcrumbList exactly.
// Case-sensitive (schema.org's canonical form).
func isBreadcrumbListType(v interface{}) bool {
	switch val := v.(type) {
	case string:
		return val == "BreadcrumbList"
	case []interface{}:
		for _, item := range val {
			if s, ok := item.(string); ok && s == "BreadcrumbList" {
				return true
			}
		}
	}
	return false
}

// buildBreadcrumbEntries reads itemListElement, orders by position, normalizes,
// filters non-navigational URLs, drops items without a URL, and caps at 5.
func buildBreadcrumbEntries(list map[string]interface{}, pageURL string) []types.BreadcrumbEntry {
	raw, ok := list["itemListElement"].([]interface{})
	if !ok || len(raw) == 0 {
		return nil
	}

	type pending struct {
		item     map[string]interface{}
		position int
		hasPos   bool
		declIdx  int
	}
	pendings := make([]pending, 0, len(raw))
	for i, e := range raw {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		pos, hasPos := readBreadcrumbPosition(m["position"])
		pendings = append(pendings, pending{item: m, position: pos, hasPos: hasPos, declIdx: i})
	}

	sort.SliceStable(pendings, func(i, j int) bool {
		if pendings[i].hasPos != pendings[j].hasPos {
			return pendings[i].hasPos
		}
		if pendings[i].hasPos {
			return pendings[i].position < pendings[j].position
		}
		return pendings[i].declIdx < pendings[j].declIdx
	})

	var result []types.BreadcrumbEntry
	for _, p := range pendings {
		rawURL := readBreadcrumbURL(p.item)
		if rawURL == "" || shouldSkipLink(rawURL) {
			continue
		}
		resolved := resolveCanonicalURL(rawURL, pageURL)
		if resolved == "" {
			resolved = rawURL
		}
		name := readBreadcrumbName(p.item)
		result = append(result, types.BreadcrumbEntry{
			Name: truncateRunes(collapseWhitespace(name), types.MaxHeadingLength),
			URL:  truncateRunes(resolved, types.MaxHreflangURLLength),
		})
		if len(result) >= types.MaxBreadcrumbs {
			break
		}
	}
	return result
}

// readBreadcrumbName resolves the display name from ListItem.name then item.name.
func readBreadcrumbName(item map[string]interface{}) string {
	if s := readJSONString(item, "name"); s != "" {
		return s
	}
	if inner, ok := item["item"].(map[string]interface{}); ok {
		return readJSONString(inner, "name")
	}
	return ""
}

// readBreadcrumbURL resolves the URL in order: item-as-string, item.@id,
// item.url, item.identifier.
func readBreadcrumbURL(item map[string]interface{}) string {
	if s, ok := item["item"].(string); ok && s != "" {
		return s
	}
	inner, ok := item["item"].(map[string]interface{})
	if !ok {
		return ""
	}
	for _, key := range []string{"@id", "url", "identifier"} {
		if s := readJSONString(inner, key); s != "" {
			return s
		}
	}
	return ""
}

// readBreadcrumbPosition extracts position from float64 or numeric string.
// Returns (0, false) for any other shape, NaN, infinity, fractional, negative,
// or out-of-int32-range values.
func readBreadcrumbPosition(v interface{}) (int, bool) {
	switch val := v.(type) {
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return 0, false
		}
		if val != math.Trunc(val) {
			return 0, false
		}
		if val < 0 || val > math.MaxInt32 {
			return 0, false
		}
		return int(val), true
	case string:
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			return 0, false
		}
		n, err := strconv.Atoi(trimmed)
		if err != nil || n < 0 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// readJSONString returns the string at key, or "" if the key is absent or the
// value is not a string.
func readJSONString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
