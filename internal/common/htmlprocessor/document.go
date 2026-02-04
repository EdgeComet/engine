package htmlprocessor

import "github.com/edgecomet/engine/pkg/types"

const maxTitleLength = 200

// Document provides methods for processing HTML documents.
type Document interface {
	// Title extracts the page title from <title> tag.
	// Returns empty string if not found.
	// Truncates to 200 characters (runes, not bytes).
	Title() string

	// IndexationStatus determines page indexability with priority:
	// non-200 > blocked by meta > non-canonical > indexable
	IndexationStatus(statusCode int, finalURL string) types.IndexStatus

	// CleanScripts removes executable script elements.
	// Returns true if any were removed.
	CleanScripts() bool

	// HTML returns current HTML as bytes (re-serialized from DOM).
	HTML() []byte

	// ExtractPageSEO extracts comprehensive SEO metadata from the document.
	// statusCode and pageURL are needed for IndexationStatus calculation.
	ExtractPageSEO(statusCode int, pageURL string) *types.PageSEO
}
