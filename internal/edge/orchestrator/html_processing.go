package orchestrator

import (
	"context"

	"github.com/edgecomet/engine/internal/common/htmlprocessor"
	"github.com/edgecomet/engine/pkg/types"
	"go.uber.org/zap"
)

func ProcessContent(
	ctx context.Context,
	html []byte,
	statusCode int,
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
		return &ProcessedContent{
			HTML:    html,
			PageSEO: &types.PageSEO{IndexStatus: types.IndexStatusIndexable},
		}
	}

	pageSEO := doc.ExtractPageSEO(statusCode, targetURL)

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

	if output.Override != nil {
		result.Override = output.Override
		result.OriginalPageSEO = pageSEO
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
	}

	return result
}
