package htmlprocessor

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	metaSelector    = "meta"
	timeSelector    = "time"
	articleSelector = "article"

	contentAttribute   = "content"
	datetimeAttribute  = "datetime"
	propertyAttribute  = "property"
	nameAttribute      = "name"
	httpEquivAttribute = "http-equiv"

	jsonLDTypeKey = "@type"

	// nestedArticleAncestors is the <article> ancestor count that marks a <time> as
	// sitting inside a nested article: its closest article ancestor has an article
	// ancestor of its own. Nested articles are the HTML5 pattern for comments and
	// other inner items.
	nestedArticleAncestors = 2
)

// jsonLDDateProperties maps the closed list of date-bearing JSON-LD properties onto
// the field each collapses to. The slice order is load-bearing: Go randomizes map
// iteration, so emitting one object's mapped properties in this fixed order is what
// makes repeated extraction of the same document byte-identical.
var jsonLDDateProperties = []struct {
	property string
	field    string
}{
	{"datePublished", types.DateFieldPublished},
	{"dateModified", types.DateFieldModified},
	{"dateCreated", types.DateFieldCreated},
	{"uploadDate", types.DateFieldPublished},
	{"datePosted", types.DateFieldPublished},
	{"releaseDate", types.DateFieldReleased},
	{"startDate", types.DateFieldStart},
	{"endDate", types.DateFieldEnd},
	{"expires", types.DateFieldExpires},
	{"validThrough", types.DateFieldExpires},
	{"priceValidUntil", types.DateFieldExpires},
	{"lastReviewed", types.DateFieldModified},
}

// jsonLDDateSkipKeys name item-level subtrees: list items (ItemList, BreadcrumbList,
// carousels), per-review and per-comment dates, and properly nested related entities.
// The walk does not descend into their values, so dates describing items listed on
// the page never become candidates. Singular nested objects (author, mainEntity,
// offers) are still walked.
var jsonLDDateSkipKeys = map[string]struct{}{
	"itemListElement": {},
	"review":          {},
	"reviews":         {},
	"comment":         {},
	"isRelatedTo":     {},
	"isSimilarTo":     {},
}

// metaDateFields maps a lowercased meta attribute value to the field it collapses to.
// Sites swap the carrying attribute freely, so the value alone decides the match.
var metaDateFields = map[string]string{
	"article:published_time": types.DateFieldPublished,
	"article:modified_time":  types.DateFieldModified,
	"og:updated_time":        types.DateFieldModified,
	"date":                   types.DateFieldPublished,
	"last-modified":          types.DateFieldModified,
	"revised":                types.DateFieldModified,
	"dcterms.created":        types.DateFieldPublished,
	"dcterms.modified":       types.DateFieldModified,
	"dcterms.date":           types.DateFieldPublished,
	"dc.date":                types.DateFieldPublished,
	"dc.date.issued":         types.DateFieldPublished,
	"dc.date.modified":       types.DateFieldModified,
	"parsely-pub-date":       types.DateFieldPublished,
}

// metaDateAttributes are probed in this order and the first match on a tag wins, so a
// tag carrying two matching attributes still contributes exactly one candidate.
var metaDateAttributes = []string{propertyAttribute, nameAttribute, httpEquivAttribute}

// jsonLDDateSignature identifies repeated markup: the same property under the same
// @type, which is what a listing emits once per item.
type jsonLDDateSignature struct {
	context  string
	property string
}

// collectedJSONLDDate is a candidate before the repetition guard, which needs the
// property name that types.DateCandidate no longer carries once mapped to a field.
type collectedJSONLDDate struct {
	signature jsonLDDateSignature
	field     string
	raw       string
}

// extractDates collects every page-level date signal as raw evidence: no
// normalization, no validation, no winner-picking. Candidates are grouped by source
// in the order json_ld, meta, time_element, document order within each group.
//
// The result is never nil. An empty slice states that the page was inspected and
// carries no date signal, which consumers must be able to tell apart from an absent
// key. Hostile markup is contained: the walk is depth-limited, every value is type
// checked, and a panic degrades to no candidates rather than failing the request.
func extractDates(doc *goquery.Document, blocks []interface{}) (result []types.DateCandidate) {
	result = make([]types.DateCandidate, 0, types.MaxDateCandidates)
	defer func() {
		if r := recover(); r != nil {
			result = make([]types.DateCandidate, 0)
		}
	}()

	result = append(result, jsonLDDateCandidates(blocks)...)
	result = append(result, metaDateCandidates(doc)...)
	result = append(result, timeDateCandidates(doc)...)

	for i := range result {
		result[i].Raw = truncateRunes(result[i].Raw, types.MaxDateRawLength)
		result[i].Context = truncateRunes(result[i].Context, types.MaxDateRawLength)
	}
	if len(result) > types.MaxDateCandidates {
		result = result[:types.MaxDateCandidates]
	}
	return result
}

// AppendDateCandidate appends one candidate to seo.Dates, truncating raw and context
// to the same rune budget extraction applies. It is the seam for signals that are not
// markup - the origin response header - which is why it does not enforce the markup
// candidate cap that ExtractPageSEO has already applied.
func AppendDateCandidate(seo *types.PageSEO, source, field, raw, contextValue string) {
	if seo == nil {
		return
	}
	seo.Dates = append(seo.Dates, types.DateCandidate{
		Source:  source,
		Field:   field,
		Raw:     truncateRunes(raw, types.MaxDateRawLength),
		Context: truncateRunes(contextValue, types.MaxDateRawLength),
	})
}

// jsonLDDateCandidates walks every parsed block, then applies the repetition guard.
// Counting requires the full set, so this cannot stream.
func jsonLDDateCandidates(blocks []interface{}) []types.DateCandidate {
	var w jsonLDDateWalker
	for _, block := range blocks {
		w.walk(block, "", 0)
	}
	return applyJSONLDDateRepetitionGuard(w.collected)
}

type jsonLDDateWalker struct {
	collected []collectedJSONLDDate
}

// walk descends one JSON-LD value, carrying the context of the nearest ancestor
// object that declared an @type. Each value is visited exactly once: @graph is an
// ordinary child key here, and walking it twice would double every candidate under it
// and trip the repetition guard on markup that repeats nothing.
//
// Within one object the mapped properties are emitted first, in mapping table order,
// before descending into children in sorted-key order. Arrays keep element order, so
// sibling blocks, array elements and @graph entries all keep document order.
func (w *jsonLDDateWalker) walk(v interface{}, context string, depth int) {
	if depth > types.MaxJSONLDRecursionDepth {
		return
	}

	switch val := v.(type) {
	case map[string]interface{}:
		if declared := jsonLDTypeOf(val); declared != "" {
			context = declared
		}
		for _, mapping := range jsonLDDateProperties {
			raw, ok := jsonLDDateLiteral(val[mapping.property])
			if !ok {
				continue
			}
			w.collected = append(w.collected, collectedJSONLDDate{
				signature: jsonLDDateSignature{context: context, property: mapping.property},
				field:     mapping.field,
				raw:       raw,
			})
		}
		for _, key := range sortedJSONKeys(val) {
			if _, skip := jsonLDDateSkipKeys[key]; skip {
				continue
			}
			w.walk(val[key], context, depth+1)
		}
	case []interface{}:
		for _, item := range val {
			w.walk(item, context, depth+1)
		}
	}
}

// applyJSONLDDateRepetitionGuard drops flat item-level noise that no skip key catches,
// such as a @graph of 50 Product objects or one script block per card: a signature
// occurring DateRepetitionThreshold times or more keeps only its first occurrence in
// document order. Signatures occurring once or twice keep every occurrence, so two
// blocks disagreeing about an article's date both survive - that conflict is the
// finding, not noise.
func applyJSONLDDateRepetitionGuard(collected []collectedJSONLDDate) []types.DateCandidate {
	counts := make(map[jsonLDDateSignature]int, len(collected))
	for _, c := range collected {
		counts[c.signature]++
	}

	emitted := make(map[jsonLDDateSignature]struct{}, len(counts))
	kept := make([]types.DateCandidate, 0, len(collected))
	for _, c := range collected {
		if counts[c.signature] >= types.DateRepetitionThreshold {
			if _, done := emitted[c.signature]; done {
				continue
			}
			emitted[c.signature] = struct{}{}
		}
		kept = append(kept, types.DateCandidate{
			Source:  types.DateSourceJSONLD,
			Field:   c.field,
			Raw:     c.raw,
			Context: c.signature.context,
		})
	}
	return kept
}

// jsonLDTypeOf returns the object's own @type: the string value, or the first element
// of an array value when that element is a string. Any other shape leaves the
// inherited context in place.
func jsonLDTypeOf(obj map[string]interface{}) string {
	switch val := obj[jsonLDTypeKey].(type) {
	case string:
		return val
	case []interface{}:
		if len(val) == 0 {
			return ""
		}
		first, _ := val[0].(string)
		return first
	}
	return ""
}

// jsonLDDateLiteral returns the captured text of a mapped property value. Strings are
// verbatim; numbers keep their source literal, because reformatting an epoch through
// float64 would turn 1709632800 into 1.7096328e+09 and lose precision past 2^53.
// Objects and arrays (@value wrappers, lists) carry no date of their own.
func jsonLDDateLiteral(v interface{}) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	case json.Number:
		return val.String(), true
	}
	return "", false
}

func sortedJSONKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// metaDateCandidates reads the closed list of date meta tags in document order. The
// comparison runs in Go rather than through a selector: goquery attribute selectors
// are case-sensitive and would silently miss name="Date".
func metaDateCandidates(doc *goquery.Document) []types.DateCandidate {
	var candidates []types.DateCandidate
	doc.Find(metaSelector).Each(func(_ int, s *goquery.Selection) {
		for _, attribute := range metaDateAttributes {
			value, exists := s.Attr(attribute)
			if !exists {
				continue
			}
			field, mapped := metaDateFields[strings.ToLower(strings.TrimSpace(value))]
			if !mapped {
				continue
			}
			// A tag with no content attribute carries no value to capture, while
			// content="" is captured: an empty value is a finding.
			content, hasContent := s.Attr(contentAttribute)
			if !hasContent {
				return
			}
			candidates = append(candidates, types.DateCandidate{
				Source:  types.DateSourceMeta,
				Field:   field,
				Raw:     content,
				Context: value,
			})
			return
		}
	})
	return candidates
}

// timeDateCandidates reads <time datetime> values. The element says which date it is
// only through its surroundings, so field is unknown and context stays empty. The
// structural skip rules run before the cap: a feed page whose page-level element
// follows three in-article ones must still capture it.
func timeDateCandidates(doc *goquery.Document) []types.DateCandidate {
	elements := doc.Find(timeSelector)
	if elements.Length() == 0 {
		return nil
	}
	skipInsideArticles := countTopLevelArticles(doc) >= types.FeedArticleThreshold

	var candidates []types.DateCandidate
	elements.EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if len(candidates) >= types.MaxTimeElementDates {
			return false
		}
		datetime, exists := s.Attr(datetimeAttribute)
		if !exists {
			return true
		}
		articleAncestors := s.ParentsFiltered(articleSelector).Length()
		if articleAncestors >= nestedArticleAncestors {
			return true
		}
		if skipInsideArticles && articleAncestors > 0 {
			return true
		}
		candidates = append(candidates, types.DateCandidate{
			Source: types.DateSourceTimeElement,
			Field:  types.DateFieldUnknown,
			Raw:    datetime,
		})
		return true
	})
	return candidates
}

// countTopLevelArticles counts <article> elements without an <article> ancestor.
// Counting every article instead would discard a post's own date as soon as it had
// two comments marked up as nested articles.
func countTopLevelArticles(doc *goquery.Document) int {
	count := 0
	doc.Find(articleSelector).Each(func(_ int, s *goquery.Selection) {
		if s.ParentsFiltered(articleSelector).Length() == 0 {
			count++
		}
	})
	return count
}
