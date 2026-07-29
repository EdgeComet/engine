package htmlprocessor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/edgecomet/engine/pkg/types"
)

const (
	benchLinkCount = 10000
	benchNestDepth = 500
	benchPageURL   = "https://example.com/"
)

// linkPageFixture builds the pathological page shape for link capture: every anchor
// sits under a chain of classed (therefore significant) containers, so an unmemoized
// ancestor walk costs depth steps per anchor, and the anchor count runs far past
// MaxPageLinks so most walks feed a placement that is dropped anyway.
func linkPageFixture(depth, links int) string {
	var b strings.Builder
	b.WriteString("<html><body>")
	for i := 0; i < depth; i++ {
		fmt.Fprintf(&b, `<div class="level-%d wrapper">`, i)
	}
	for i := 0; i < links; i++ {
		fmt.Fprintf(&b, `<a href="/page-%d">Link %d</a>`, i, i)
	}
	b.WriteString(strings.Repeat("</div>", depth))
	b.WriteString("</body></html>")
	return b.String()
}

func benchmarkLinkMetrics(b *testing.B, htmlStr string) {
	b.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		b.Fatalf("failed to parse fixture: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractLinkMetrics(doc, benchPageURL, benchPageURL, &types.PageSEO{})
	}
}

func BenchmarkExtractLinkMetrics_DeepNested(b *testing.B) {
	benchmarkLinkMetrics(b, linkPageFixture(benchNestDepth, benchLinkCount))
}

func BenchmarkExtractLinkMetrics_Flat(b *testing.B) {
	benchmarkLinkMetrics(b, linkPageFixture(0, benchLinkCount))
}
