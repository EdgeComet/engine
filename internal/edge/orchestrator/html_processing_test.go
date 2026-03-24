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

func TestProcessContent_BasicParsing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Test Title", "<p>Hello world</p>")

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, nil, logger)

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
	result := ProcessContent(context.Background(), nil, 200, "https://example.com/page", false, 1, nil, logger)

	require.NotNil(t, result)
	require.NotNil(t, result.PageSEO)
	assert.Equal(t, types.IndexStatusIndexable, result.PageSEO.IndexStatus)
}

func TestProcessContent_StripScripts(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := []byte(`<!DOCTYPE html><html><head><title>Test</title><script type="application/ld+json">{"@type":"Article"}</script></head><body><p>Content</p><script>alert('xss')</script></body></html>`)

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", true, 1, nil, logger)

	require.NotNil(t, result)
	resultHTML := string(result.HTML)
	assert.NotContains(t, resultHTML, "alert('xss')")
	assert.Contains(t, resultHTML, "application/ld+json")
	assert.Contains(t, resultHTML, "Article")
}

func TestProcessContent_NoStripScripts(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := []byte(`<!DOCTYPE html><html><head><title>Test</title><script type="application/ld+json">{"@type":"Article"}</script></head><body><p>Content</p><script>alert('xss')</script></body></html>`)

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, nil, logger)

	require.NotNil(t, result)
	resultHTML := string(result.HTML)
	assert.Contains(t, resultHTML, "alert('xss')")
	assert.Contains(t, resultHTML, "application/ld+json")
}

func TestProcessContent_NilContentProcessor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Title", "<p>Body</p>")

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, nil, logger)

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

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, cp, logger)

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

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, cp, logger)

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

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	assert.Equal(t, modifiedHTML, result.HTML)
	assert.Equal(t, "New Title", result.PageSEO.Title)
	require.NotNil(t, result.OriginalPageSEO)
	assert.Equal(t, "Old Title", result.OriginalPageSEO.Title)
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

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	require.NotNil(t, result.Override)
	assert.Equal(t, 301, result.Override.StatusCode)
	assert.Equal(t, "https://example.com/new", result.Override.Location)
	assert.Equal(t, []uint32{42}, result.RuleIDs)
	require.NotNil(t, result.OriginalPageSEO)
}

func TestProcessContent_ContentProcessorRuleIDsOnly(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := sampleHTML("Title", "<p>Body</p>")

	cp := &mockContentProcessor{
		output: &ContentOutput{
			RuleIDs: []uint32{1, 2, 3},
		},
	}

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, cp, logger)

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

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, cp, logger)

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

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, cp, logger)

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

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	assert.Contains(t, string(result.HTML), "New Title")
	assert.Equal(t, "New Title", result.PageSEO.Title)
	require.NotNil(t, result.OriginalPageSEO)
	assert.Equal(t, "Old Title", result.OriginalPageSEO.Title)
	assert.Equal(t, []uint32{5}, result.RuleIDs)
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

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", false, 1, cp, logger)

	require.NotNil(t, result)
	assert.Contains(t, string(result.HTML), "Same Title")
	assert.Equal(t, "Same Title", result.PageSEO.Title)
	require.NotNil(t, result.OriginalPageSEO)
	assert.Equal(t, "Same Title", result.OriginalPageSEO.Title)
}

func TestProcessContent_PageSEOExtractedBeforeScriptStripping(t *testing.T) {
	logger := zaptest.NewLogger(t)
	html := []byte(`<!DOCTYPE html><html><head><title>Test</title><script type="application/ld+json">{"@type":"Article","name":"Test"}</script></head><body><p>Content</p></body></html>`)

	result := ProcessContent(context.Background(), html, 200, "https://example.com/page", true, 1, nil, logger)

	require.NotNil(t, result)
	require.NotNil(t, result.PageSEO)
	assert.NotEmpty(t, result.PageSEO.StructuredDataTypes)
	assert.Contains(t, result.PageSEO.StructuredDataTypes, "Article")
	// Verify script stripping happened
	assert.True(t, strings.Contains(string(result.HTML), "application/ld+json"),
		"JSON-LD script should be preserved after stripping")
}
