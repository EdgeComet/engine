package orchestrator

import (
	"context"

	"github.com/edgecomet/engine/internal/common/htmlprocessor"
	"github.com/edgecomet/engine/pkg/types"
	"go.uber.org/zap"
)

// ProcessContent extracts SEO metadata from the response body, optionally strips scripts
// and hands the document to the content processor.
//
// responseHeaders carries the origin response headers exactly as received, before
// safe_response filtering: capture must see a date signal that the headers config would
// hide from the client.
func ProcessContent(
	ctx context.Context,
	html []byte,
	statusCode int,
	responseHeaders map[string][]string,
	targetURL string,
	stripScripts bool,
	hostID int,
	cp ContentProcessor,
	logger *zap.Logger,
) *ProcessedContent {
	doc, err := htmlprocessor.ParseWithDOM(html)
	if err != nil {
		logger.Warn("Failed to parse HTML for content processing",
			zap.String("url", targetURL),
			zap.Error(err),
		)
		// Parsing failed but the processor ran, so the page carries evidence of having
		// been inspected: an initialized (empty) date slice, plus the header candidate,
		// which does not depend on the HTML.
		unparsedSEO := &types.PageSEO{
			IndexStatus: types.IndexStatusIndexable,
			Dates:       []types.DateCandidate{},
		}
		appendLastModifiedDate(unparsedSEO, responseHeaders)
		return &ProcessedContent{
			HTML:    html,
			PageSEO: unparsedSEO,
		}
	}

	pageSEO := doc.ExtractPageSEO(statusCode, targetURL)
	appendLastModifiedDate(pageSEO, responseHeaders)

	processedHTML := html
	if stripScripts {
		if doc.CleanScripts() {
			processedHTML = doc.HTML()
		}
	}

	result := &ProcessedContent{
		HTML:    processedHTML,
		PageSEO: pageSEO,
	}

	if cp == nil {
		return result
	}

	input := &ContentInput{
		Doc:        doc,
		PageSEO:    pageSEO,
		StatusCode: statusCode,
		URL:        targetURL,
		HostID:     hostID,
	}

	output, err := cp.ProcessContent(ctx, input)
	if err != nil {
		logger.Warn("Content processor failed, using unmodified content",
			zap.String("url", targetURL),
			zap.Error(err),
		)
		return result
	}

	if output == nil {
		return result
	}

	result.RuleIDs = output.RuleIDs
	result.Extraction = output.Extraction

	if output.Override != nil {
		result.Override = output.Override
		result.OriginalPageSEO = pageSEO
		return result
	}

	if output.Modified {
		result.HTML = doc.HTML()
		result.OriginalPageSEO = pageSEO
		result.PageSEO = doc.ExtractPageSEO(statusCode, targetURL)
		appendLastModifiedDate(result.PageSEO, responseHeaders)
		return result
	}

	if output.HTML != nil {
		result.HTML = output.HTML
		result.OriginalPageSEO = pageSEO

		reDoc, err := htmlprocessor.ParseWithDOM(output.HTML)
		if err != nil {
			logger.Warn("Failed to re-parse modified HTML from content processor",
				zap.String("url", targetURL),
				zap.Error(err),
			)
			return result
		}

		result.PageSEO = reDoc.ExtractPageSEO(statusCode, targetURL)
		appendLastModifiedDate(result.PageSEO, responseHeaders)
	}

	return result
}

// appendLastModifiedDate records the origin's Last-Modified value as a date candidate.
// Context stores the canonical spelling rather than the one received, so an HTTP/2 origin
// sending "last-modified" is indistinguishable downstream from an HTTP/1.1 one. Nothing is
// appended when the origin sent no such header.
func appendLastModifiedDate(seo *types.PageSEO, responseHeaders map[string][]string) {
	value, ok := firstHeaderValueSorted(responseHeaders, types.LastModifiedHeader)
	if !ok {
		return
	}
	htmlprocessor.AppendDateCandidate(
		seo,
		types.DateSourceHTTPHeader,
		types.DateFieldModified,
		value,
		types.LastModifiedHeader,
	)
}
