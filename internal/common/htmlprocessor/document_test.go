package htmlprocessor

import (
	"strings"
	"testing"

	"github.com/edgecomet/engine/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocument_Title(t *testing.T) {
	implementations := []struct {
		name  string
		parse func([]byte) (Document, error)
	}{
		{"DOM", ParseWithDOM},
	}

	for _, impl := range implementations {
		t.Run(impl.name, func(t *testing.T) {
			t.Run("basic title", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><title>Hello World</title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, "Hello World", doc.Title())
			})

			t.Run("whitespace trimmed", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><title>  Spaced  </title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, "Spaced", doc.Title())
			})

			t.Run("long title truncated to 200 runes", func(t *testing.T) {
				longTitle := strings.Repeat("a", 250)
				html := `<!DOCTYPE html><html><head><title>` + longTitle + `</title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				result := doc.Title()
				assert.Len(t, []rune(result), 200)
				assert.Equal(t, strings.Repeat("a", 200), result)
			})

			t.Run("no title tag returns empty", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, "", doc.Title())
			})

			t.Run("empty title returns empty", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><title></title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, "", doc.Title())
			})

			t.Run("whitespace only title returns empty", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><title>   </title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, "", doc.Title())
			})

			t.Run("HTML entities decoded", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><title>A &amp; B</title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, "A & B", doc.Title())
			})

			t.Run("title outside head ignored", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head></head><body><title>Body Title</title></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, "", doc.Title())
			})

			t.Run("unicode multibyte not truncated at 200 runes", func(t *testing.T) {
				// 200 Japanese characters - each is 3 bytes in UTF-8
				unicodeTitle := strings.Repeat("あ", 200)
				html := `<!DOCTYPE html><html><head><title>` + unicodeTitle + `</title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				result := doc.Title()
				assert.Len(t, []rune(result), 200)
				assert.Equal(t, unicodeTitle, result)
			})

			t.Run("unicode multibyte truncated correctly at rune boundary", func(t *testing.T) {
				// 250 Japanese characters - should truncate to 200
				unicodeTitle := strings.Repeat("あ", 250)
				html := `<!DOCTYPE html><html><head><title>` + unicodeTitle + `</title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				result := doc.Title()
				assert.Len(t, []rune(result), 200)
				assert.Equal(t, strings.Repeat("あ", 200), result)
			})

			t.Run("no head tag returns empty", func(t *testing.T) {
				html := `<!DOCTYPE html><html><body><title>No Head</title></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, "", doc.Title())
			})

			t.Run("first title in head used", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><title>First</title><title>Second</title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, "First", doc.Title())
			})

			t.Run("newlines and tabs trimmed", func(t *testing.T) {
				html := "<!DOCTYPE html><html><head><title>\n\t  Title With Whitespace  \t\n</title></head><body></body></html>"
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, "Title With Whitespace", doc.Title())
			})
		})
	}
}

func TestDocument_IndexationStatus(t *testing.T) {
	implementations := []struct {
		name  string
		parse func([]byte) (Document, error)
	}{
		{"DOM", ParseWithDOM},
	}

	for _, impl := range implementations {
		t.Run(impl.name, func(t *testing.T) {
			t.Run("status 200 clean HTML is indexable", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><title>Test</title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, types.IndexStatusIndexable, doc.IndexationStatus(200, "https://example.com/page"))
			})

			t.Run("status 404 returns Non200", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><title>Not Found</title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, types.IndexStatusNon200, doc.IndexationStatus(404, "https://example.com/page"))
			})

			t.Run("status 500 returns Non200", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><title>Error</title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, types.IndexStatusNon200, doc.IndexationStatus(500, "https://example.com/page"))
			})

			t.Run("status 301 returns Non200", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><title>Redirect</title></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, types.IndexStatusNon200, doc.IndexationStatus(301, "https://example.com/page"))
			})

			t.Run("status 200 robots noindex returns BlockedByMeta", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><meta name="robots" content="noindex"></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, types.IndexStatusBlockedByMeta, doc.IndexationStatus(200, "https://example.com/page"))
			})

			t.Run("status 200 canonical mismatch returns NonCanonical", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><link rel="canonical" href="https://example.com/other"></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, types.IndexStatusNonCanonical, doc.IndexationStatus(200, "https://example.com/page"))
			})

			t.Run("status 200 blocked AND non-canonical returns BlockedByMeta (priority)", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><meta name="robots" content="noindex"><link rel="canonical" href="https://example.com/other"></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, types.IndexStatusBlockedByMeta, doc.IndexationStatus(200, "https://example.com/page"))
			})

			t.Run("status 404 blocked returns Non200 (priority)", func(t *testing.T) {
				html := `<!DOCTYPE html><html><head><meta name="robots" content="noindex"></head><body></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, types.IndexStatusNon200, doc.IndexationStatus(404, "https://example.com/page"))
			})

			t.Run("no head tag returns Indexable (graceful handling)", func(t *testing.T) {
				html := `<!DOCTYPE html><html><body><p>Content</p></body></html>`
				doc, err := impl.parse([]byte(html))
				require.NoError(t, err)
				assert.Equal(t, types.IndexStatusIndexable, doc.IndexationStatus(200, "https://example.com/page"))
			})
		})
	}
}
