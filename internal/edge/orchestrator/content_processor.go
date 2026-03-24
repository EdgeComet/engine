package orchestrator

import (
	"context"

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
	HTML     []byte
	RuleIDs  []uint32
	Override *ResponseOverride
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
}
