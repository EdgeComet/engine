package htmlprocessor

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractDatesFromHTML(t *testing.T, htmlStr string) []types.DateCandidate {
	t.Helper()
	doc := parseGoQueryDoc(t, htmlStr)
	return extractDates(doc, collectJSONLDBlocks(doc))
}

func jsonLDPage(blocks ...string) string {
	var b strings.Builder
	b.WriteString(`<html><head>`)
	for _, block := range blocks {
		b.WriteString(`<script type="application/ld+json">`)
		b.WriteString(block)
		b.WriteString(`</script>`)
	}
	b.WriteString(`</head><body></body></html>`)
	return b.String()
}

func jsonLDDate(field, raw, context string) types.DateCandidate {
	return types.DateCandidate{Source: types.DateSourceJSONLD, Field: field, Raw: raw, Context: context}
}

func metaDate(field, raw, context string) types.DateCandidate {
	return types.DateCandidate{Source: types.DateSourceMeta, Field: field, Raw: raw, Context: context}
}

func timeDate(raw string) types.DateCandidate {
	return types.DateCandidate{Source: types.DateSourceTimeElement, Field: types.DateFieldUnknown, Raw: raw}
}

func TestExtractDates_JSONLDMappingTable(t *testing.T) {
	block := `{"@type":"Thing",
		"datePublished":"v1","dateModified":"v2","dateCreated":"v3","uploadDate":"v4",
		"datePosted":"v5","releaseDate":"v6","startDate":"v7","endDate":"v8",
		"expires":"v9","validThrough":"v10","priceValidUntil":"v11","lastReviewed":"v12"}`

	got := extractDatesFromHTML(t, jsonLDPage(block))

	assert.Equal(t, []types.DateCandidate{
		jsonLDDate(types.DateFieldPublished, "v1", "Thing"),
		jsonLDDate(types.DateFieldModified, "v2", "Thing"),
		jsonLDDate(types.DateFieldCreated, "v3", "Thing"),
		jsonLDDate(types.DateFieldPublished, "v4", "Thing"),
		jsonLDDate(types.DateFieldPublished, "v5", "Thing"),
		jsonLDDate(types.DateFieldReleased, "v6", "Thing"),
		jsonLDDate(types.DateFieldStart, "v7", "Thing"),
		jsonLDDate(types.DateFieldEnd, "v8", "Thing"),
		jsonLDDate(types.DateFieldExpires, "v9", "Thing"),
		jsonLDDate(types.DateFieldExpires, "v10", "Thing"),
		jsonLDDate(types.DateFieldExpires, "v11", "Thing"),
		jsonLDDate(types.DateFieldModified, "v12", "Thing"),
	}, got)
}

func TestExtractDates_JSONLDContext(t *testing.T) {
	t.Run("nearest typed ancestor wins and is inherited", func(t *testing.T) {
		block := `{"@type":"WebPage","dateModified":"W",
			"mainEntity":{"@type":"Article","datePublished":"A",
				"author":{"name":"X","datePublished":"INHERITED"}}}`

		assert.Equal(t, []types.DateCandidate{
			jsonLDDate(types.DateFieldModified, "W", "WebPage"),
			jsonLDDate(types.DateFieldPublished, "A", "Article"),
			jsonLDDate(types.DateFieldPublished, "INHERITED", "Article"),
		}, extractDatesFromHTML(t, jsonLDPage(block)))
	})

	t.Run("no type anywhere leaves context empty", func(t *testing.T) {
		assert.Equal(t, []types.DateCandidate{
			jsonLDDate(types.DateFieldPublished, "X", ""),
		}, extractDatesFromHTML(t, jsonLDPage(`{"datePublished":"X"}`)))
	})

	t.Run("array type uses first element", func(t *testing.T) {
		block := `{"@type":["NewsArticle","Article"],"datePublished":"X"}`
		assert.Equal(t, []types.DateCandidate{
			jsonLDDate(types.DateFieldPublished, "X", "NewsArticle"),
		}, extractDatesFromHTML(t, jsonLDPage(block)))
	})

	t.Run("unusable type shapes keep the inherited context", func(t *testing.T) {
		block := `{"@type":"Article","mainEntity":{"@type":[42],"datePublished":"A",
			"about":{"@type":{"nested":true},"datePublished":"B"}}}`
		assert.Equal(t, []types.DateCandidate{
			jsonLDDate(types.DateFieldPublished, "A", "Article"),
			jsonLDDate(types.DateFieldPublished, "B", "Article"),
		}, extractDatesFromHTML(t, jsonLDPage(block)))
	})
}

func TestExtractDates_JSONLDGraphVisitedOnce(t *testing.T) {
	block := `{"@context":"https://schema.org","@graph":[
		{"@type":"Article","datePublished":"A"},
		{"@type":"WebPage","dateModified":"B"}]}`

	assert.Equal(t, []types.DateCandidate{
		jsonLDDate(types.DateFieldPublished, "A", "Article"),
		jsonLDDate(types.DateFieldModified, "B", "WebPage"),
	}, extractDatesFromHTML(t, jsonLDPage(block)))
}

func TestExtractDates_JSONLDNestedGraphVisitedOnce(t *testing.T) {
	block := `{"@graph":[{"@graph":[{"@type":"Article","datePublished":"A"}]}]}`

	assert.Equal(t, []types.DateCandidate{
		jsonLDDate(types.DateFieldPublished, "A", "Article"),
	}, extractDatesFromHTML(t, jsonLDPage(block)))
}

func TestExtractDates_JSONLDSkipKeys(t *testing.T) {
	block := `{"@type":"Product","releaseDate":"PRIMARY",
		"review":{"@type":"Review","datePublished":"R1"},
		"reviews":[{"@type":"Review","datePublished":"R2"}],
		"comment":{"@type":"Comment","datePublished":"C1"},
		"isRelatedTo":{"@type":"Product","releaseDate":"REL"},
		"isSimilarTo":{"@type":"Product","releaseDate":"SIM"},
		"itemListElement":[{"@type":"ListItem","item":{"@type":"Product","releaseDate":"L1"}}],
		"offers":{"@type":"Offer","priceValidUntil":"OFFER"}}`

	assert.Equal(t, []types.DateCandidate{
		jsonLDDate(types.DateFieldReleased, "PRIMARY", "Product"),
		jsonLDDate(types.DateFieldExpires, "OFFER", "Offer"),
	}, extractDatesFromHTML(t, jsonLDPage(block)))
}

func TestExtractDates_JSONLDSkipKeysAreCaseSensitive(t *testing.T) {
	block := `{"@type":"Product","Review":{"@type":"Review","datePublished":"R"}}`

	assert.Equal(t, []types.DateCandidate{
		jsonLDDate(types.DateFieldPublished, "R", "Review"),
	}, extractDatesFromHTML(t, jsonLDPage(block)))
}

func TestExtractDates_RepetitionGuard(t *testing.T) {
	articleBlock := func(value string) string {
		return `{"@type":"Article","datePublished":"` + value + `"}`
	}

	t.Run("two occurrences of a signature are both kept", func(t *testing.T) {
		got := extractDatesFromHTML(t, jsonLDPage(articleBlock("A"), articleBlock("B")))
		assert.Equal(t, []types.DateCandidate{
			jsonLDDate(types.DateFieldPublished, "A", "Article"),
			jsonLDDate(types.DateFieldPublished, "B", "Article"),
		}, got)
	})

	t.Run("three occurrences collapse to the first in document order", func(t *testing.T) {
		got := extractDatesFromHTML(t, jsonLDPage(articleBlock("A"), articleBlock("B"), articleBlock("C")))
		assert.Equal(t, []types.DateCandidate{
			jsonLDDate(types.DateFieldPublished, "A", "Article"),
		}, got)
	})

	t.Run("signature is context plus property, not value", func(t *testing.T) {
		block := `{"@graph":[
			{"@type":"Product","releaseDate":"p1","priceValidUntil":"e1"},
			{"@type":"Product","releaseDate":"p2","priceValidUntil":"e2"},
			{"@type":"Product","releaseDate":"p3","priceValidUntil":"e3"},
			{"@type":"Event","startDate":"s1"}]}`

		assert.Equal(t, []types.DateCandidate{
			jsonLDDate(types.DateFieldReleased, "p1", "Product"),
			jsonLDDate(types.DateFieldExpires, "e1", "Product"),
			jsonLDDate(types.DateFieldStart, "s1", "Event"),
		}, extractDatesFromHTML(t, jsonLDPage(block)))
	})

	t.Run("same property under different types is not repetition", func(t *testing.T) {
		got := extractDatesFromHTML(t, jsonLDPage(
			`{"@type":"Article","datePublished":"A"}`,
			`{"@type":"NewsArticle","datePublished":"B"}`,
			`{"@type":"BlogPosting","datePublished":"C"}`,
		))
		assert.Len(t, got, 3)
	})
}

func TestExtractDates_JSONLDValueTypes(t *testing.T) {
	t.Run("numeric source literal is preserved", func(t *testing.T) {
		block := `{"@type":"Article","datePublished":1709632800,"dateModified":9007199254740993}`
		assert.Equal(t, []types.DateCandidate{
			jsonLDDate(types.DateFieldPublished, "1709632800", "Article"),
			jsonLDDate(types.DateFieldModified, "9007199254740993", "Article"),
		}, extractDatesFromHTML(t, jsonLDPage(block)))
	})

	t.Run("object and array values are not captured", func(t *testing.T) {
		block := `{"@type":"Article","datePublished":{"@value":"2024-03-05"},
			"dateModified":["2024-03-06"],"dateCreated":null,"uploadDate":true}`
		assert.Empty(t, extractDatesFromHTML(t, jsonLDPage(block)))
	})

	t.Run("malformed and empty values are captured verbatim", func(t *testing.T) {
		block := `{"@type":"Article","datePublished":"yesterday-ish","dateModified":""}`
		assert.Equal(t, []types.DateCandidate{
			jsonLDDate(types.DateFieldPublished, "yesterday-ish", "Article"),
			jsonLDDate(types.DateFieldModified, "", "Article"),
		}, extractDatesFromHTML(t, jsonLDPage(block)))
	})
}

func TestExtractDates_JSONLDScriptTypeMatching(t *testing.T) {
	htmlStr := `<html><head>
		<script type="Application/LD+JSON; charset=utf-8">{"@type":"Article","datePublished":"A"}</script>
		<script type="  application/ld+json  ">{"@type":"Article","dateModified":"B"}</script>
		<script type="application/json">{"@type":"Article","datePublished":"IGNORED"}</script>
		<script>{"@type":"Article","datePublished":"IGNORED"}</script>
		</head><body></body></html>`

	assert.Equal(t, []types.DateCandidate{
		jsonLDDate(types.DateFieldPublished, "A", "Article"),
		jsonLDDate(types.DateFieldModified, "B", "Article"),
	}, extractDatesFromHTML(t, htmlStr))
}

func TestExtractDates_JSONLDUnparseableBlockSkipped(t *testing.T) {
	got := extractDatesFromHTML(t, jsonLDPage(
		`{invalid json`,
		`{"@type":"Article","datePublished":"A"} trailing garbage`,
		`{"@type":"Article","dateModified":"B"}`,
	))

	assert.Equal(t, []types.DateCandidate{
		jsonLDDate(types.DateFieldModified, "B", "Article"),
	}, got)
}

func TestExtractDates_JSONLDOversizedBlockSkipped(t *testing.T) {
	padding := strings.Repeat("x", types.MaxJSONLDSize)
	got := extractDatesFromHTML(t, jsonLDPage(
		`{"@type":"Article","datePublished":"A","description":"`+padding+`"}`,
		`{"@type":"Article","dateModified":"B"}`,
	))

	assert.Equal(t, []types.DateCandidate{
		jsonLDDate(types.DateFieldModified, "B", "Article"),
	}, got)
}

func TestExtractDates_JSONLDRecursionDepthBounded(t *testing.T) {
	var b strings.Builder
	closes := 0
	for i := 0; i < types.MaxJSONLDRecursionDepth+5; i++ {
		b.WriteString(`{"mainEntity":`)
		closes++
	}
	b.WriteString(`{"@type":"Article","datePublished":"DEEP"}`)
	b.WriteString(strings.Repeat(`}`, closes))

	assert.NotPanics(t, func() {
		assert.Empty(t, extractDatesFromHTML(t, jsonLDPage(b.String())))
	})
}

func TestExtractDates_MetaTags(t *testing.T) {
	htmlStr := `<html><head>
		<meta property="article:published_time" content="2024-03-05">
		<meta property="article:modified_time" content="2024-03-06">
		<meta property="og:updated_time" content="2024-03-07">
		<meta name="date" content="2024-03-08">
		<meta name="last-modified" content="2024-03-09">
		<meta name="revised" content="2024-03-10">
		<meta name="dcterms.created" content="2024-03-11">
		<meta name="dcterms.modified" content="2024-03-12">
		<meta name="dcterms.date" content="2024-03-13">
		<meta name="DC.date" content="2024-03-14">
		<meta name="DC.date.issued" content="2024-03-15">
		<meta name="DC.date.modified" content="2024-03-16">
		<meta name="parsely-pub-date" content="2024-03-17">
		</head><body></body></html>`

	assert.Equal(t, []types.DateCandidate{
		metaDate(types.DateFieldPublished, "2024-03-05", "article:published_time"),
		metaDate(types.DateFieldModified, "2024-03-06", "article:modified_time"),
		metaDate(types.DateFieldModified, "2024-03-07", "og:updated_time"),
		metaDate(types.DateFieldPublished, "2024-03-08", "date"),
		metaDate(types.DateFieldModified, "2024-03-09", "last-modified"),
		metaDate(types.DateFieldModified, "2024-03-10", "revised"),
		metaDate(types.DateFieldPublished, "2024-03-11", "dcterms.created"),
		metaDate(types.DateFieldModified, "2024-03-12", "dcterms.modified"),
		metaDate(types.DateFieldPublished, "2024-03-13", "dcterms.date"),
		metaDate(types.DateFieldPublished, "2024-03-14", "DC.date"),
		metaDate(types.DateFieldPublished, "2024-03-15", "DC.date.issued"),
		metaDate(types.DateFieldModified, "2024-03-16", "DC.date.modified"),
		metaDate(types.DateFieldPublished, "2024-03-17", "parsely-pub-date"),
	}, extractDatesFromHTML(t, htmlStr))
}

func TestExtractDates_MetaAttributeSwapAndCase(t *testing.T) {
	htmlStr := `<html><head>
		<meta name="article:published_time" content="wp-plugin">
		<meta http-equiv="last-modified" content="legacy">
		<meta NAME="DATE" content="upper">
		<meta property="Article:Modified_Time" content="mixed">
		</head><body></body></html>`

	assert.Equal(t, []types.DateCandidate{
		metaDate(types.DateFieldPublished, "wp-plugin", "article:published_time"),
		metaDate(types.DateFieldModified, "legacy", "last-modified"),
		metaDate(types.DateFieldPublished, "upper", "DATE"),
		metaDate(types.DateFieldModified, "mixed", "Article:Modified_Time"),
	}, extractDatesFromHTML(t, htmlStr))
}

func TestExtractDates_MetaContentAttribute(t *testing.T) {
	htmlStr := `<html><head>
		<meta name="date" content="">
		<meta name="revised">
		<meta name="last-modified" content=" 2024-03-05 ">
		</head><body></body></html>`

	assert.Equal(t, []types.DateCandidate{
		metaDate(types.DateFieldPublished, "", "date"),
		metaDate(types.DateFieldModified, " 2024-03-05 ", "last-modified"),
	}, extractDatesFromHTML(t, htmlStr))
}

func TestExtractDates_MetaDuplicateTags(t *testing.T) {
	htmlStr := `<html><head>
		<meta property="article:published_time" content="first">
		<meta property="article:published_time" content="second">
		</head><body></body></html>`

	assert.Equal(t, []types.DateCandidate{
		metaDate(types.DateFieldPublished, "first", "article:published_time"),
		metaDate(types.DateFieldPublished, "second", "article:published_time"),
	}, extractDatesFromHTML(t, htmlStr))
}

func TestExtractDates_MetaMultipleMatchingAttributesYieldOneCandidate(t *testing.T) {
	htmlStr := `<html><head>
		<meta property="og:updated_time" name="date" http-equiv="revised" content="X">
		</head><body></body></html>`

	assert.Equal(t, []types.DateCandidate{
		metaDate(types.DateFieldModified, "X", "og:updated_time"),
	}, extractDatesFromHTML(t, htmlStr))
}

func TestExtractDates_MetaParselyPageIsNotParsed(t *testing.T) {
	htmlStr := `<html><head>
		<meta name="parsely-page" content='{"title":"T","pub_date":"2024-03-05T10:00:00Z"}'>
		<meta name="parsely-pub-date" content="2024-03-05T10:00:00Z">
		</head><body></body></html>`

	assert.Equal(t, []types.DateCandidate{
		metaDate(types.DateFieldPublished, "2024-03-05T10:00:00Z", "parsely-pub-date"),
	}, extractDatesFromHTML(t, htmlStr))
}

func TestExtractDates_TimeElements(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected []types.DateCandidate
	}{
		{
			name:     "datetime attribute captured, missing attribute skipped",
			html:     `<body><time datetime="2024-03-05">March 5</time><time>March 6</time></body>`,
			expected: []types.DateCandidate{timeDate("2024-03-05")},
		},
		{
			name:     "empty datetime is captured",
			html:     `<body><time datetime="">unknown</time></body>`,
			expected: []types.DateCandidate{timeDate("")},
		},
		{
			name: "first three kept in document order",
			html: `<body><time datetime="1"></time><time datetime="2"></time>
				<time datetime="3"></time><time datetime="4"></time></body>`,
			expected: []types.DateCandidate{timeDate("1"), timeDate("2"), timeDate("3")},
		},
		{
			name: "multiple top-level articles drop every in-article element",
			html: `<body>
				<article><time datetime="a1"></time></article>
				<article><time datetime="a2"></time></article>
				</body>`,
			expected: nil,
		},
		{
			name: "single article keeps its own element and drops nested article ones",
			html: `<body><article>
					<time datetime="post"></time>
					<article><time datetime="comment1"></time></article>
					<article><time datetime="comment2"></time></article>
				</article></body>`,
			expected: []types.DateCandidate{timeDate("post")},
		},
		{
			name: "skip rules run before the first-three cap",
			html: `<body>
				<article><time datetime="a1"></time></article>
				<article><time datetime="a2"></time></article>
				<article><time datetime="a3"></time></article>
				<footer><time datetime="page-level"></time></footer>
				</body>`,
			expected: []types.DateCandidate{timeDate("page-level")},
		},
		{
			name: "nested article skip applies without a feed",
			html: `<body><div><article><article>
					<time datetime="nested"></time>
				</article></article></div></body>`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDatesFromHTML(t, `<html><head></head>`+tt.html+`</html>`)
			if tt.expected == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestExtractDates_SourceGrouping(t *testing.T) {
	htmlStr := `<html><head>
		<script type="application/ld+json">{"@type":"Article","datePublished":"jsonld"}</script>
		<meta name="date" content="meta">
		</head><body><time datetime="time"></time></body></html>`

	assert.Equal(t, []types.DateCandidate{
		jsonLDDate(types.DateFieldPublished, "jsonld", "Article"),
		metaDate(types.DateFieldPublished, "meta", "date"),
		timeDate("time"),
	}, extractDatesFromHTML(t, htmlStr))
}

func TestExtractDates_NoDedupAcrossSources(t *testing.T) {
	htmlStr := `<html><head>
		<script type="application/ld+json">{"@type":"Article","datePublished":"2024-03-05"}</script>
		<meta property="article:published_time" content="2024-03-05">
		</head><body></body></html>`

	assert.Len(t, extractDatesFromHTML(t, htmlStr), 2)
}

func TestExtractDates_Truncation(t *testing.T) {
	longValue := strings.Repeat("日", types.MaxDateRawLength+6)
	longType := strings.Repeat("型", types.MaxDateRawLength+6)
	block := `{"@type":"` + longType + `","datePublished":"` + longValue + `"}`

	got := extractDatesFromHTML(t, jsonLDPage(block))

	require.Len(t, got, 1)
	assert.Equal(t, types.MaxDateRawLength, utf8.RuneCountInString(got[0].Raw))
	assert.Equal(t, types.MaxDateRawLength, utf8.RuneCountInString(got[0].Context))
	assert.Equal(t, strings.Repeat("日", types.MaxDateRawLength), got[0].Raw)
	assert.Equal(t, strings.Repeat("型", types.MaxDateRawLength), got[0].Context)
	assert.True(t, utf8.ValidString(got[0].Raw))
}

func TestExtractDates_CandidateCap(t *testing.T) {
	t.Run("markup candidates are capped dropping from the tail", func(t *testing.T) {
		var blocks []string
		for i := 0; i < 15; i++ {
			blocks = append(blocks, fmt.Sprintf(`{"@type":"Type%d","datePublished":"j%d"}`, i, i))
		}
		var meta strings.Builder
		for i := 0; i < 10; i++ {
			fmt.Fprintf(&meta, `<meta name="date" content="m%d">`, i)
		}
		htmlStr := `<html><head>` + strings.Join(wrapJSONLD(blocks), "") + meta.String() + `</head><body></body></html>`

		got := extractDatesFromHTML(t, htmlStr)

		require.Len(t, got, types.MaxDateCandidates)
		assert.Equal(t, jsonLDDate(types.DateFieldPublished, "j0", "Type0"), got[0])
		assert.Equal(t, metaDate(types.DateFieldPublished, "m4", "date"), got[types.MaxDateCandidates-1])
	})

	t.Run("repetition guard runs before the cap", func(t *testing.T) {
		var items []string
		for i := 0; i < 25; i++ {
			items = append(items, fmt.Sprintf(`{"@type":"Product","releaseDate":"p%d"}`, i))
		}
		htmlStr := `<html><head>` +
			`<script type="application/ld+json">{"@graph":[` + strings.Join(items, ",") + `]}</script>` +
			`<meta name="date" content="published">` +
			`<meta name="last-modified" content="modified">` +
			`</head><body></body></html>`

		assert.Equal(t, []types.DateCandidate{
			jsonLDDate(types.DateFieldReleased, "p0", "Product"),
			metaDate(types.DateFieldPublished, "published", "date"),
			metaDate(types.DateFieldModified, "modified", "last-modified"),
		}, extractDatesFromHTML(t, htmlStr))
	})
}

func wrapJSONLD(blocks []string) []string {
	wrapped := make([]string, 0, len(blocks))
	for _, block := range blocks {
		wrapped = append(wrapped, `<script type="application/ld+json">`+block+`</script>`)
	}
	return wrapped
}

func TestExtractDates_Deterministic(t *testing.T) {
	htmlStr := `<html><head>
		<script type="application/ld+json">{"@context":"https://schema.org","@graph":[
			{"@type":"WebPage","dateModified":"w1","lastReviewed":"w2","about":{"@type":"Thing","dateCreated":"t1"}},
			{"@type":"Article","datePublished":"a1","dateModified":"a2","author":{"name":"A"},
			 "publisher":{"@type":"Organization","dateCreated":"o1"}},
			{"@type":"Event","startDate":"e1","endDate":"e2","location":{"@type":"Place","dateCreated":"p1"}}]}</script>
		<meta property="article:published_time" content="m1">
		<meta name="dcterms.modified" content="m2">
		</head><body><time datetime="t"></time></body></html>`

	first, err := json.Marshal(extractDatesFromHTML(t, htmlStr))
	require.NoError(t, err)

	for i := 0; i < 30; i++ {
		next, err := json.Marshal(extractDatesFromHTML(t, htmlStr))
		require.NoError(t, err)
		require.JSONEq(t, string(first), string(next))
		require.Equal(t, string(first), string(next))
	}
}

func TestExtractDates_HostileInput(t *testing.T) {
	inputs := []string{
		`{"@type":{"@type":"Article"},"datePublished":"X"}`,
		`[[[[{"@type":"Article","datePublished":"X"}]]]]`,
		`{"@type":[],"datePublished":"X"}`,
		`{"@graph":null,"datePublished":"X"}`,
		`{"@graph":"not-an-array"}`,
		`"just a string"`,
		`12345`,
		`null`,
		`[]`,
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			assert.NotPanics(t, func() {
				got := extractDatesFromHTML(t, jsonLDPage(input))
				assert.NotNil(t, got)
			})
		})
	}
}

func TestExtractPageSEO_DatesAlwaysInitialized(t *testing.T) {
	doc, err := ParseWithDOM([]byte(`<html><head><title>No dates here</title></head><body><p>x</p></body></html>`))
	require.NoError(t, err)

	seo := doc.ExtractPageSEO(200, "https://example.com/")

	require.NotNil(t, seo.Dates)
	assert.Empty(t, seo.Dates)

	encoded, err := json.Marshal(seo)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"dates":[]`)
	assert.NotContains(t, string(encoded), `"dates":null`)
}

func TestDateCandidate_SerializesEveryKey(t *testing.T) {
	encoded, err := json.Marshal(types.DateCandidate{
		Source: types.DateSourceTimeElement,
		Field:  types.DateFieldUnknown,
	})
	require.NoError(t, err)

	assert.JSONEq(t, `{"source":"time_element","field":"unknown","raw":"","context":""}`, string(encoded))
}

func TestExtractPageSEO_DatesIntegration(t *testing.T) {
	htmlContent := `<!DOCTYPE html>
<html><head>
	<title>Blog post</title>
	<meta property="article:published_time" content="2024-03-05T10:00:00+02:00">
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"BlogPosting",
	 "datePublished":"2024-03-05T10:00:00+02:00","dateModified":"2024-03-06T09:00:00+02:00",
	 "comment":[{"@type":"Comment","datePublished":"2024-03-07"}]}
	</script>
</head><body>
	<article><time datetime="2024-03-05">March 5, 2024</time></article>
</body></html>`

	doc, err := ParseWithDOM([]byte(htmlContent))
	require.NoError(t, err)

	seo := doc.ExtractPageSEO(200, "https://example.com/post")

	assert.Equal(t, []types.DateCandidate{
		jsonLDDate(types.DateFieldPublished, "2024-03-05T10:00:00+02:00", "BlogPosting"),
		jsonLDDate(types.DateFieldModified, "2024-03-06T09:00:00+02:00", "BlogPosting"),
		metaDate(types.DateFieldPublished, "2024-03-05T10:00:00+02:00", "article:published_time"),
		timeDate("2024-03-05"),
	}, seo.Dates)
}

func TestAppendDateCandidate(t *testing.T) {
	t.Run("appends beyond the markup cap with truncation", func(t *testing.T) {
		seo := &types.PageSEO{Dates: make([]types.DateCandidate, types.MaxDateCandidates)}
		longRaw := strings.Repeat("é", types.MaxDateRawLength+10)

		AppendDateCandidate(seo, types.DateSourceHTTPHeader, types.DateFieldModified, longRaw, types.LastModifiedHeader)

		require.Len(t, seo.Dates, types.MaxDateCandidates+1)
		last := seo.Dates[types.MaxDateCandidates]
		assert.Equal(t, types.DateSourceHTTPHeader, last.Source)
		assert.Equal(t, types.DateFieldModified, last.Field)
		assert.Equal(t, types.MaxDateRawLength, utf8.RuneCountInString(last.Raw))
		assert.Equal(t, types.LastModifiedHeader, last.Context)
	})

	t.Run("initializes a nil slice", func(t *testing.T) {
		seo := &types.PageSEO{}

		AppendDateCandidate(seo, types.DateSourceHTTPHeader, types.DateFieldModified, "Wed, 05 Mar 2024 08:00:00 GMT", types.LastModifiedHeader)

		assert.Equal(t, []types.DateCandidate{{
			Source:  types.DateSourceHTTPHeader,
			Field:   types.DateFieldModified,
			Raw:     "Wed, 05 Mar 2024 08:00:00 GMT",
			Context: types.LastModifiedHeader,
		}}, seo.Dates)
	})

	t.Run("nil page is a no-op", func(t *testing.T) {
		assert.NotPanics(t, func() {
			AppendDateCandidate(nil, types.DateSourceHTTPHeader, types.DateFieldModified, "x", types.LastModifiedHeader)
		})
	})
}

func TestBreadcrumbPositionAcceptsJSONNumber(t *testing.T) {
	htmlStr := `<html><head><script type="application/ld+json">{"@type":"BreadcrumbList","itemListElement":[
		{"position":3,"name":"Third","item":"https://e.com/c"},
		{"position":1,"name":"First","item":"https://e.com/a"},
		{"position":2.0,"name":"Second","item":"https://e.com/b"}]}</script></head></html>`

	doc := parseGoQueryDoc(t, htmlStr)
	got := extractBreadcrumbs(collectJSONLDBlocks(doc), "https://e.com/")

	require.Len(t, got, 3)
	assert.Equal(t, "First", got[0].Name)
	assert.Equal(t, "Second", got[1].Name)
	assert.Equal(t, "Third", got[2].Name)
}
