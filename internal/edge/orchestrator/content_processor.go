package orchestrator

import (
	"context"
	"encoding/json"

	"github.com/edgecomet/engine/internal/common/htmlprocessor"
	"github.com/edgecomet/engine/pkg/types"
)

// ContentProcessor processes rendered or bypassed content, applying
// transformations such as SEO rule matching and response overrides.
// Implementations must be safe for concurrent use.
type ContentProcessor interface {
	ProcessContent(ctx context.Context, input *ContentInput) (*ContentOutput, error)
}

type ContentInput struct {
	Doc        htmlprocessor.Document
	PageSEO    *types.PageSEO
	StatusCode int
	URL        string
	HostID     int
}

type ContentOutput struct {
	HTML       []byte
	RuleIDs    []uint32
	Override   *ResponseOverride
	Modified   bool
	Extraction json.RawMessage
	// PageSEOSnapshot is a capture the processor took itself from the edited Doc, as
	// Doc.ExtractPageSEO(StatusCode, URL) and nothing more: the caller adds the
	// Last-Modified candidate and logs a dropped capture. It must be a new value, never
	// the input PageSEO. Read only with Modified set.
	PageSEOSnapshot *types.PageSEO
	// UnchangedSinceSnapshot reports that the processor did not change Doc after it took
	// PageSEOSnapshot. Only then does the caller use the snapshot in place of its own
	// re-capture of the modified document.
	UnchangedSinceSnapshot bool
}

type ResponseOverride struct {
	StatusCode int
	Location   string
}

type ProcessedContent struct {
	HTML            []byte
	PageSEO         *types.PageSEO
	RuleIDs         []uint32
	Override        *ResponseOverride
	OriginalPageSEO *types.PageSEO
	Extraction      json.RawMessage
}
