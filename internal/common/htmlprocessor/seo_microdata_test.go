package htmlprocessor

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

func microdataPage(body string) string {
	return `<html><head><title>t</title></head><body>` + body + `</body></html>`
}

func TestExtractMicrodataTypes(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected []string
	}{
		{
			name:     "top-level microdata item",
			body:     `<div itemscope itemtype="https://schema.org/Product"><span itemprop="name">Bike</span></div>`,
			expected: []string{"Product"},
		},
		{
			name:     "http, www and mixed case prefixes",
			body:     `<div itemscope itemtype="http://schema.org/Product"></div><div itemscope itemtype="HTTPS://WWW.Schema.org/Organization"></div>`,
			expected: []string{"Product", "Organization"},
		},
		{
			name: "nested item with itemprop is not top-level",
			body: `<div itemscope itemtype="https://schema.org/Product">
				<div itemprop="offers" itemscope itemtype="https://schema.org/Offer"></div>
				<div itemprop="aggregateRating" itemscope itemtype="https://schema.org/AggregateRating"></div>
			</div>`,
			expected: []string{"Product"},
		},
		{
			name:     "item that is a property of a page item",
			body:     `<main itemscope itemtype="https://schema.org/ItemPage"><div itemprop="mainEntity" itemscope itemtype="https://schema.org/Product"></div></main>`,
			expected: []string{"ItemPage"},
		},
		{
			name:     "itemprop without an enclosing item still marks a property",
			body:     `<div itemprop="mainEntity" itemscope itemtype="https://schema.org/Product"></div>`,
			expected: nil,
		},
		{
			name:     "every URL in itemtype gives one value",
			body:     `<div itemscope itemtype=" https://schema.org/Product  https://schema.org/Vehicle "></div>`,
			expected: []string{"Product", "Vehicle"},
		},
		{
			name:     "other vocabularies are left out",
			body:     `<div itemscope itemtype="http://data-vocabulary.org/Breadcrumb"></div><div itemscope itemtype="https://schema.org/BreadcrumbList"></div>`,
			expected: []string{"BreadcrumbList"},
		},
		{
			name:     "itemtype without itemscope is not an item",
			body:     `<div itemtype="https://schema.org/Product"></div>`,
			expected: nil,
		},
		{
			name:     "itemscope without itemtype has no type",
			body:     `<div itemscope><span itemprop="name">x</span></div>`,
			expected: nil,
		},
		{
			name:     "a bare vocabulary prefix is no type",
			body:     `<div itemscope itemtype="https://schema.org/"></div>`,
			expected: nil,
		},
		{
			name: "a trailing slash, a query and a fragment are dropped",
			body: `<div itemscope itemtype="https://schema.org/Product/"></div><div itemscope itemtype="https://schema.org/Offer?x=1"></div>
				<div itemscope itemtype="https://schema.org/Event#e"></div><div typeof="schema:Place/"></div>`,
			expected: []string{"Product", "Offer", "Event", "Place"},
		},
		{
			name:     "a path or an empty name after the prefix is no type",
			body:     `<div itemscope itemtype="https://schema.org/Product/Bike"></div><div itemscope itemtype="https://schema.org/#x"></div>`,
			expected: nil,
		},
		{
			name:     "case is kept",
			body:     `<div itemscope itemtype="http://schema.org/product"></div>`,
			expected: []string{"product"},
		},
		{
			name: "each type once, in document order",
			body: `<header itemscope itemtype="https://schema.org/Organization"></header>
				<div itemscope itemtype="https://schema.org/Product"></div>
				<footer itemscope itemtype="http://schema.org/Organization"></footer>
				<ol itemscope itemtype="https://schema.org/BreadcrumbList"></ol>`,
			expected: []string{"Organization", "Product", "BreadcrumbList"},
		},
		{
			name:     "RDFa term under a schema.org vocab",
			body:     `<div vocab="https://schema.org/" typeof="Product"><span property="name">Bike</span></div>`,
			expected: []string{"Product"},
		},
		{
			name:     "RDFa vocab without the trailing slash, on an ancestor",
			body:     `<section vocab="http://schema.org"><div typeof="Event"></div></section>`,
			expected: []string{"Event"},
		},
		{
			name:     "RDFa schema: CURIE needs no vocab",
			body:     `<div typeof="schema:Product"></div>`,
			expected: []string{"Product"},
		},
		{
			name:     "RDFa absolute schema.org IRI",
			body:     `<div typeof="http://schema.org/Recipe"></div>`,
			expected: []string{"Recipe"},
		},
		{
			name:     "RDFa term without a vocab is left out",
			body:     `<div typeof="Product"></div>`,
			expected: nil,
		},
		{
			name:     "RDFa term under another vocab is left out",
			body:     `<div vocab="http://xmlns.com/foaf/0.1/" typeof="Person"></div>`,
			expected: nil,
		},
		{
			name:     "RDFa CURIE of another prefix is left out",
			body:     `<div vocab="https://schema.org/" typeof="foaf:Person og:article"></div>`,
			expected: nil,
		},
		{
			name: "RDFa resource with property is not top-level",
			body: `<div vocab="https://schema.org/" typeof="Product">
				<div property="offers" typeof="Offer"></div>
			</div>`,
			expected: []string{"Product"},
		},
		{
			name:     "an inner vocab replaces the outer one",
			body:     `<div vocab="https://schema.org/"><div vocab=""><div typeof="Product"></div></div><div typeof="Place"></div></div>`,
			expected: []string{"Place"},
		},
		{
			name:     "several RDFa types on one element",
			body:     `<div vocab="https://schema.org/" typeof="Product schema:IndividualProduct"></div>`,
			expected: []string{"Product", "IndividualProduct"},
		},
		{
			name: "microdata and RDFa share one ordered list",
			body: `<div typeof="schema:WebSite"></div>
				<div itemscope itemtype="https://schema.org/Product"></div>
				<div vocab="https://schema.org/" typeof="Product"></div>`,
			expected: []string{"WebSite", "Product"},
		},
		{
			name:     "template contents are skipped",
			body:     `<template><div itemscope itemtype="https://schema.org/Product"></div><div typeof="schema:Offer"></div></template>`,
			expected: nil,
		},
		{
			name:     "a type name over the length cap is dropped",
			body:     `<div itemscope itemtype="https://schema.org/` + strings.Repeat("A", types.MaxMicrodataTypeLength+1) + `"></div><div itemscope itemtype="https://schema.org/Product"></div>`,
			expected: []string{"Product"},
		},
		{
			name:     "no markup",
			body:     `<p>plain</p>`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseGoQueryDoc(t, microdataPage(tt.body))
			assert.Equal(t, tt.expected, extractMicrodataTypes(doc))
		})
	}
}

func TestExtractMicrodataTypes_HeadItems(t *testing.T) {
	doc := parseGoQueryDoc(t, `<html itemscope itemtype="https://schema.org/WebPage"><head>
		<meta itemprop="name" content="x"></head><body><div itemscope itemtype="https://schema.org/Product"></div></body></html>`)
	assert.Equal(t, []string{"WebPage", "Product"}, extractMicrodataTypes(doc))
}

func TestExtractMicrodataTypes_Cap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < types.MaxMicrodataTypes+5; i++ {
		fmt.Fprintf(&b, `<div itemscope itemtype="https://schema.org/Type%d"></div>`, i)
	}
	doc := parseGoQueryDoc(t, microdataPage(b.String()))

	got := extractMicrodataTypes(doc)

	require.Len(t, got, types.MaxMicrodataTypes)
	assert.Equal(t, "Type0", got[0])
	assert.Equal(t, fmt.Sprintf("Type%d", types.MaxMicrodataTypes-1), got[len(got)-1])
}

func TestExtractPageSEO_MicrodataTypes(t *testing.T) {
	page := `<html><head><title>Bike</title>
		<script type="application/ld+json">{"@context":"https://schema.org","@type":"BreadcrumbList"}</script>
		</head><body>
		<div itemscope itemtype="https://schema.org/Product">
			<div itemprop="offers" itemscope itemtype="https://schema.org/Offer"></div>
		</div>
		</body></html>`
	doc, err := ParseWithDOM([]byte(page))
	require.NoError(t, err)

	seo := doc.ExtractPageSEO(200, "https://example.com/bike")

	assert.Equal(t, []string{"Product"}, seo.MicrodataTypes)
	assert.Equal(t, []string{"BreadcrumbList"}, seo.StructuredDataTypes, "JSON-LD types stay apart from microdata")

	data, err := json.Marshal(seo)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"microdata_types":["Product"]`)
}

func TestExtractPageSEO_NoMicrodataOmitsKey(t *testing.T) {
	doc, err := ParseWithDOM([]byte(microdataPage(`<p>plain</p>`)))
	require.NoError(t, err)

	seo := doc.ExtractPageSEO(200, "https://example.com/")

	assert.Nil(t, seo.MicrodataTypes)
	data, err := json.Marshal(seo)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "microdata_types")
}
