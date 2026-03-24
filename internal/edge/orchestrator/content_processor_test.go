package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockContentProcessor struct {
	output *ContentOutput
	err    error
}

func (m *mockContentProcessor) ProcessContent(_ context.Context, _ *ContentInput) (*ContentOutput, error) {
	return m.output, m.err
}

func TestContentProcessorInterfaceCompliance(t *testing.T) {
	mock := &mockContentProcessor{
		output: &ContentOutput{
			HTML:    []byte("<html></html>"),
			RuleIDs: []uint32{1, 2, 3},
		},
	}

	var processor ContentProcessor = mock
	result, err := processor.ProcessContent(context.Background(), &ContentInput{
		StatusCode: 200,
		URL:        "https://example.com",
		HostID:     1,
	})

	require.NoError(t, err)
	assert.Equal(t, []byte("<html></html>"), result.HTML)
	assert.Equal(t, []uint32{1, 2, 3}, result.RuleIDs)
}

func TestContentOutputNilHTMLMeansNoModification(t *testing.T) {
	output := &ContentOutput{
		HTML:    nil,
		RuleIDs: []uint32{1, 2},
	}

	assert.Nil(t, output.HTML)
	assert.Equal(t, []uint32{1, 2}, output.RuleIDs)
}

func TestResponseOverrideRedirect(t *testing.T) {
	override := &ResponseOverride{
		StatusCode: 301,
		Location:   "https://example.com/new",
	}

	assert.Equal(t, 301, override.StatusCode)
	assert.Equal(t, "https://example.com/new", override.Location)
}

func TestResponseOverrideStatusOnly(t *testing.T) {
	override := &ResponseOverride{
		StatusCode: 410,
		Location:   "",
	}

	assert.Equal(t, 410, override.StatusCode)
	assert.Empty(t, override.Location)
}

func TestProcessedContentZeroValue(t *testing.T) {
	var pc ProcessedContent

	assert.Nil(t, pc.PageSEO)
	assert.Nil(t, pc.Override)
	assert.Nil(t, pc.RuleIDs)
	assert.Empty(t, pc.HTML)
	assert.Nil(t, pc.OriginalPageSEO)
}
