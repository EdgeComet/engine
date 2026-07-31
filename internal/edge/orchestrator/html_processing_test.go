package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func sampleHTML(title, body string) []byte {
	return []byte(fmt.Sprintf(`<!DOCTYPE html><html><head><title>%s</title></head><body>%s</body></html>`, title, body))
}

const testLastModified = "Wed, 05 Mar 2024 08:00:00 GMT"

// originHeaders builds a raw origin header map in the shape both carriers produce: a
// multi-value map keyed by the name exactly as received.
func originHeaders(name, value string) map[string][]string {
	return map[string][]string{name: {value}}
}

func lastModifiedCandidate(raw string) types.DateCandidate {
	return types.DateCandidate{
		Source:  types.DateSourceHTTPHeader,
		Field:   types.DateFieldModified,
		Raw:     raw,
		Context: types.LastModifiedHeader,
	}
}

func TestProcessContent_BasicParsing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Test Title", "<p>Hello world</p>")

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", false, 1, nil, logger)

	require.NotNil(t, result)
	assert.Equal(t, "Test Title", result.PageSEO.Title)
	assert.NotEmpty(t, result.HTML)
	assert.Nil(t, result.RuleIDs)
	assert.Nil(t, result.Override)
}

func TestProcessContent_ParseFailure(t *testing.T) {
	logger := zaptest.NewLogger(t)
	// goquery/net/html is very lenient, so we test with nil input
	// which will produce an empty document; verify PageSEO is still populated
	result := ProcessContent(context.Background(), nil, 200, nil, "https://example.com/page", false, 1, nil, logger)

	require.NotNil(t, result)
	require.NotNil(t, result.PageSEO)
	assert.Equal(t, types.IndexStatusIndexable, result.PageSEO.IndexStatus)
	assert.NotNil(t, result.PageSEO.Dates, "dates must be initialized so the event never carries null")
	assert.Empty(t, result.PageSEO.Dates)
}

func TestProcessContent_StripScripts(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := []byte(`<!DOCTYPE html><html><head><title>Test</title><script type="application/ld+json">{"@type":"Article"}</script></head><body><p>Content</p><script>alert('xss')</script></body></html>`)

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", true, 1, nil, logger)

	require.NotNil(t, result)
	resultHTML := string(result.HTML)
	assert.NotContains(t, resultHTML, "alert('xss')")
	assert.Contains(t, resultHTML, "application/ld+json")
	assert.Contains(t, resultHTML, "Article")
}

func TestProcessContent_NoStripScripts(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := []byte(`<!DOCTYPE html><html><head><title>Test</title><script type="application/ld+json">{"@type":"Article"}</script></head><body><p>Content</p><script>alert('xss')</script></body></html>`)

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", false, 1, nil, logger)

	require.NotNil(t, result)
	resultHTML := string(result.HTML)
	assert.Contains(t, resultHTML, "alert('xss')")
	assert.Contains(t, resultHTML, "application/ld+json")
}

func TestProcessContent_NilContentProcessor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Title", "<p>Body</p>")

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", false, 1, nil, logger)

	require.NotNil(t, result)
	assert.NotNil(t, result.PageSEO)
	assert.NotEmpty(t, result.HTML)
	assert.Nil(t, result.RuleIDs)
}

func TestProcessContent_ContentProcessorError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Original Title", "<p>Body</p>")

	cp := &mockContentProcessor{
		err: fmt.Errorf("processing failed"),
	}

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	assert.Equal(t, "Original Title", result.PageSEO.Title)
	assert.NotEmpty(t, result.HTML)
	assert.Nil(t, result.RuleIDs)
}

func TestProcessContent_ContentProcessorNilOutput(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Original Title", "<p>Body</p>")

	cp := &mockContentProcessor{
		output: nil,
		err:    nil,
	}

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	assert.Equal(t, "Original Title", result.PageSEO.Title)
	assert.NotEmpty(t, result.HTML)
}

func TestProcessContent_ContentProcessorModifiesHTML(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Old Title", "<p>Body</p>")
	modifiedHTML := sampleHTML("New Title", "<p>Modified Body</p>")

	cp := &mockContentProcessor{
		output: &ContentOutput{
			HTML: modifiedHTML,
		},
	}

	headers := originHeaders(types.LastModifiedHeader, testLastModified)
	result := ProcessContent(context.Background(), html, 200, headers, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	assert.Equal(t, modifiedHTML, result.HTML)
	assert.Equal(t, "New Title", result.PageSEO.Title)
	require.NotNil(t, result.OriginalPageSEO)
	assert.Equal(t, "Old Title", result.OriginalPageSEO.Title)

	// The re-extracted snapshot and the original snapshot each carry the header candidate.
	expected := []types.DateCandidate{lastModifiedCandidate(testLastModified)}
	assert.Equal(t, expected, result.PageSEO.Dates)
	assert.Equal(t, expected, result.OriginalPageSEO.Dates)
}

func TestProcessContent_ContentProcessorOverride(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Title", "<p>Body</p>")

	cp := &mockContentProcessor{
		output: &ContentOutput{
			Override: &ResponseOverride{
				StatusCode: 301,
				Location:   "https://example.com/new",
			},
			RuleIDs: []uint32{42},
		},
	}

	headers := originHeaders(types.LastModifiedHeader, testLastModified)
	result := ProcessContent(context.Background(), html, 200, headers, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	require.NotNil(t, result.Override)
	assert.Equal(t, 301, result.Override.StatusCode)
	assert.Equal(t, "https://example.com/new", result.Override.Location)
	assert.Equal(t, []uint32{42}, result.RuleIDs)
	require.NotNil(t, result.OriginalPageSEO)
	assert.Equal(t, []types.DateCandidate{lastModifiedCandidate(testLastModified)}, result.OriginalPageSEO.Dates)
}

func TestProcessContent_ContentProcessorRuleIDsOnly(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Title", "<p>Body</p>")

	cp := &mockContentProcessor{
		output: &ContentOutput{
			RuleIDs: []uint32{1, 2, 3},
		},
	}

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	assert.Equal(t, []uint32{1, 2, 3}, result.RuleIDs)
	assert.Nil(t, result.OriginalPageSEO)
	assert.Nil(t, result.Override)
	assert.NotEmpty(t, result.HTML)
	assert.Equal(t, "Title", result.PageSEO.Title)
}

func TestProcessContent_ContentProcessorOverride404(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Title", "<p>Body</p>")

	cp := &mockContentProcessor{
		output: &ContentOutput{
			Override: &ResponseOverride{
				StatusCode: 404,
			},
			RuleIDs: []uint32{10},
		},
	}

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	require.NotNil(t, result.Override)
	assert.Equal(t, 404, result.Override.StatusCode)
	assert.Empty(t, result.Override.Location)
	assert.Equal(t, []uint32{10}, result.RuleIDs)
	require.NotNil(t, result.OriginalPageSEO)
	assert.Equal(t, "Title", result.OriginalPageSEO.Title)
}

func TestProcessContent_ContentProcessorOverride410(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Title", "<p>Body</p>")

	cp := &mockContentProcessor{
		output: &ContentOutput{
			Override: &ResponseOverride{
				StatusCode: 410,
			},
			RuleIDs: []uint32{20, 21},
		},
	}

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	require.NotNil(t, result.Override)
	assert.Equal(t, 410, result.Override.StatusCode)
	assert.Empty(t, result.Override.Location)
	assert.Equal(t, []uint32{20, 21}, result.RuleIDs)
	require.NotNil(t, result.OriginalPageSEO)
}

func TestProcessContent_ContentProcessorModifiedFlag(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Old Title", "<p>Body</p>")

	cp := &mockContentProcessorFn{
		fn: func(_ context.Context, input *ContentInput) (*ContentOutput, error) {
			input.Doc.GoQueryDoc().Find("title").SetText("New Title")
			return &ContentOutput{
				Modified: true,
				RuleIDs:  []uint32{5},
			}, nil
		},
	}

	headers := originHeaders(types.LastModifiedHeader, testLastModified)
	result := ProcessContent(context.Background(), html, 200, headers, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	assert.Contains(t, string(result.HTML), "New Title")
	assert.Equal(t, "New Title", result.PageSEO.Title)
	require.NotNil(t, result.OriginalPageSEO)
	assert.Equal(t, "Old Title", result.OriginalPageSEO.Title)
	assert.Equal(t, []uint32{5}, result.RuleIDs)

	expected := []types.DateCandidate{lastModifiedCandidate(testLastModified)}
	assert.Equal(t, expected, result.PageSEO.Dates)
	assert.Equal(t, expected, result.OriginalPageSEO.Dates)
}

func TestProcessContent_ContentProcessorModifiedNoChange(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Same Title", "<p>Body</p>")

	cp := &mockContentProcessorFn{
		fn: func(_ context.Context, _ *ContentInput) (*ContentOutput, error) {
			return &ContentOutput{
				Modified: true,
			}, nil
		},
	}

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	assert.Contains(t, string(result.HTML), "Same Title")
	assert.Equal(t, "Same Title", result.PageSEO.Title)
	require.NotNil(t, result.OriginalPageSEO)
	assert.Equal(t, "Same Title", result.OriginalPageSEO.Title)
}

func TestProcessContent_PageSEOExtractedBeforeScriptStripping(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := []byte(`<!DOCTYPE html><html><head><title>Test</title><script type="application/ld+json">{"@type":"Article","name":"Test"}</script></head><body><p>Content</p></body></html>`)

	result := ProcessContent(context.Background(), html, 200, nil, "https://example.com/page", true, 1, nil, logger)

	require.NotNil(t, result)
	require.NotNil(t, result.PageSEO)
	assert.NotEmpty(t, result.PageSEO.StructuredDataTypes)
	assert.Contains(t, result.PageSEO.StructuredDataTypes, "Article")
	// Verify script stripping happened
	assert.True(t, strings.Contains(string(result.HTML), "application/ld+json"),
		"JSON-LD script should be preserved after stripping")
}

func TestProcessContent_LastModifiedHeaderCaptured(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Title", "<p>Body</p>")

	tests := []struct {
		name     string
		headers  map[string][]string
		expected []types.DateCandidate
	}{
		{
			name:     "canonical name",
			headers:  originHeaders(types.LastModifiedHeader, testLastModified),
			expected: []types.DateCandidate{lastModifiedCandidate(testLastModified)},
		},
		{
			// HTTP/2 origins deliver lowercase names, and both header carriers store
			// names exactly as received.
			name:     "lowercase name from HTTP/2 origin",
			headers:  originHeaders("last-modified", testLastModified),
			expected: []types.DateCandidate{lastModifiedCandidate(testLastModified)},
		},
		{
			name:     "repeated values keep the first",
			headers:  map[string][]string{types.LastModifiedHeader: {testLastModified, "Thu, 06 Mar 2024 08:00:00 GMT"}},
			expected: []types.DateCandidate{lastModifiedCandidate(testLastModified)},
		},
		{
			name:     "no header means no candidate",
			headers:  originHeaders("Cache-Control", "max-age=60"),
			expected: []types.DateCandidate{},
		},
		{
			name:     "nil headers mean no candidate",
			headers:  nil,
			expected: []types.DateCandidate{},
		},
		{
			// A cache directive is not a content date and must not be captured.
			name:     "Expires is not captured",
			headers:  originHeaders("Expires", testLastModified),
			expected: []types.DateCandidate{},
		},
		{
			name:     "empty value is captured as evidence",
			headers:  originHeaders(types.LastModifiedHeader, ""),
			expected: []types.DateCandidate{lastModifiedCandidate("")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProcessContent(context.Background(), html, 200, tt.headers, "https://example.com/page", false, 1, nil, logger)

			require.NotNil(t, result.PageSEO)
			assert.Equal(t, tt.expected, result.PageSEO.Dates)
		})
	}
}

// TestProcessContent_LastModifiedPickIsDeterministic guards the Go map-iteration trap: an
// origin sending the same header under several spellings must resolve to one answer.
func TestProcessContent_LastModifiedPickIsDeterministic(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Title", "<p>Body</p>")
	headers := map[string][]string{
		"Last-Modified": {"Wed, 05 Mar 2024 08:00:00 GMT"},
		"last-modified": {"Thu, 06 Mar 2024 08:00:00 GMT"},
		"LAST-MODIFIED": {"Fri, 07 Mar 2024 08:00:00 GMT"},
	}

	// "LAST-MODIFIED" sorts first, so its value is the pick on every run.
	expected := []types.DateCandidate{lastModifiedCandidate("Fri, 07 Mar 2024 08:00:00 GMT")}
	for i := 0; i < 50; i++ {
		result := ProcessContent(context.Background(), html, 200, headers, "https://example.com/page", false, 1, nil, logger)
		require.Equal(t, expected, result.PageSEO.Dates)
	}
}

func TestProcessContent_LastModifiedTruncated(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Title", "<p>Body</p>")
	overlong := strings.Repeat("x", types.MaxDateRawLength+10)

	result := ProcessContent(context.Background(), html, 200, originHeaders(types.LastModifiedHeader, overlong),
		"https://example.com/page", false, 1, nil, logger)

	require.Len(t, result.PageSEO.Dates, 1)
	assert.Equal(t, strings.Repeat("x", types.MaxDateRawLength), result.PageSEO.Dates[0].Raw)
}

// TestProcessContent_EmptyBodyStillCapturesHeader covers a body that yields no markup at
// all. It does not reach the parse-failure branch: goquery over a bytes.Reader parses any
// input, so that branch stays defensive and unreachable from here.
func TestProcessContent_EmptyBodyStillCapturesHeader(t *testing.T) {
	logger := zaptest.NewLogger(t)

	result := ProcessContent(context.Background(), nil, 200, originHeaders("last-modified", testLastModified),
		"https://example.com/page", false, 1, nil, logger)

	require.NotNil(t, result.PageSEO)
	assert.Equal(t, []types.DateCandidate{lastModifiedCandidate(testLastModified)}, result.PageSEO.Dates)
}

// TestProcessContent_HeaderCandidateExemptFromCap pins the documented 21st slot: markup
// candidates are capped, the origin header is appended afterwards.
func TestProcessContent_HeaderCandidateExemptFromCap(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var metaTags strings.Builder
	for i := 0; i < types.MaxDateCandidates+5; i++ {
		fmt.Fprintf(&metaTags, `<meta property="article:published_time" content="2024-03-%02d">`, i+1)
	}
	html := []byte(`<!DOCTYPE html><html><head>` + metaTags.String() + `</head><body></body></html>`)

	result := ProcessContent(context.Background(), html, 200, originHeaders(types.LastModifiedHeader, testLastModified),
		"https://example.com/page", false, 1, nil, logger)

	dates := result.PageSEO.Dates
	require.Len(t, dates, types.MaxDateCandidates+1)
	assert.Equal(t, types.DateSourceMeta, dates[types.MaxDateCandidates-1].Source)
	assert.Equal(t, lastModifiedCandidate(testLastModified), dates[types.MaxDateCandidates])
}

// TestProcessContent_JSONLDDatesFollowedByHeader pins the group order the contract
// promises: markup candidates first, the origin header last.
func TestProcessContent_JSONLDDatesFollowedByHeader(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := []byte(`<!DOCTYPE html><html><head><script type="application/ld+json">` +
		`{"@type":"BlogPosting","datePublished":"2024-03-05T10:00:00+02:00","dateModified":"2024-04-01"}` +
		`</script></head><body></body></html>`)

	result := ProcessContent(context.Background(), html, 200, originHeaders("last-modified", testLastModified),
		"https://example.com/page", false, 1, nil, logger)

	assert.Equal(t, []types.DateCandidate{
		{Source: types.DateSourceJSONLD, Field: types.DateFieldPublished, Raw: "2024-03-05T10:00:00+02:00", Context: "BlogPosting"},
		{Source: types.DateSourceJSONLD, Field: types.DateFieldModified, Raw: "2024-04-01", Context: "BlogPosting"},
		lastModifiedCandidate(testLastModified),
	}, result.PageSEO.Dates)
}
