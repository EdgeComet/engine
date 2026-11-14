package htmlprocessor

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/edgecomet/engine/pkg/types"
)

const (
	itemScopeAttribute = "itemscope"
	itemTypeAttribute  = "itemtype"
	itemPropAttribute  = "itemprop"
	typeOfAttribute    = "typeof"
	vocabAttribute     = "vocab"

	templateTag = "template"

	schemaOrgCURIEPrefix = "schema:"
	curieSeparator       = ":"
	iriPathSeparator     = "/"
	iriSuffixDelimiters  = "?#"
)

// schemaOrgIRIPrefixes are the spellings of the schema.org vocabulary IRI, matched
// case-insensitively.
var schemaOrgIRIPrefixes = []string{
	"https://schema.org/",
	"http://schema.org/",
	"https://www.schema.org/",
	"http://www.schema.org/",
}

// extractMicrodataTypes returns the schema.org types of the page's top-level microdata
// items and RDFa typed resources as local names ("Product"), each once, in document order.
//
// An element that also carries itemprop (microdata) or property (RDFa) is the value of
// another item's property, an Offer inside a Product, so it is not top-level. An RDFa term
// resolves through the nearest vocab attribute on the element or an ancestor; a CURIE
// counts only with the schema: prefix. Types of other vocabularies are left out. Template
// contents are skipped: they are inert and never part of the document a reader sees.
func extractMicrodataTypes(doc *goquery.Document) []string {
	c := &microdataCollector{seen: make(map[string]struct{})}
	for _, n := range doc.Nodes {
		c.walk(n, false)
	}
	return c.types
}

type microdataCollector struct {
	types []string
	seen  map[string]struct{}
}

// walk visits the subtree depth first. schemaVocab reports whether the RDFa vocab in
// scope is schema.org.
func (c *microdataCollector) walk(n *html.Node, schemaVocab bool) {
	if len(c.types) >= types.MaxMicrodataTypes {
		return
	}
	if n.Type == html.ElementNode {
		if n.Data == templateTag {
			return
		}
		schemaVocab = c.collect(n, schemaVocab)
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		c.walk(child, schemaVocab)
	}
}

// collect records the element's own top-level types and returns the RDFa vocab state
// for its subtree.
func (c *microdataCollector) collect(n *html.Node, schemaVocab bool) bool {
	var itemType, typeOf string
	var itemScope, itemProp, hasTypeOf, property bool
	for _, a := range n.Attr {
		switch a.Key {
		case itemScopeAttribute:
			itemScope = true
		case itemTypeAttribute:
			itemType = a.Val
		case itemPropAttribute:
			itemProp = true
		case typeOfAttribute:
			typeOf, hasTypeOf = a.Val, true
		case propertyAttribute:
			property = true
		case vocabAttribute:
			schemaVocab = isSchemaOrgVocab(a.Val)
		}
	}

	if itemScope && !itemProp {
		for _, value := range strings.Fields(itemType) {
			if name, ok := schemaOrgLocalName(value); ok {
				c.add(name)
			}
		}
	}
	if hasTypeOf && !property {
		for _, value := range strings.Fields(typeOf) {
			if name, ok := rdfaSchemaOrgType(value, schemaVocab); ok {
				c.add(name)
			}
		}
	}
	return schemaVocab
}

func (c *microdataCollector) add(name string) {
	if len(name) > types.MaxMicrodataTypeLength || len(c.types) >= types.MaxMicrodataTypes {
		return
	}
	if _, dup := c.seen[name]; dup {
		return
	}
	c.seen[name] = struct{}{}
	c.types = append(c.types, name)
}

// schemaOrgLocalName strips a schema.org IRI prefix or the schema: CURIE prefix. It
// reports false for any other value and for a bare prefix.
func schemaOrgLocalName(value string) (string, bool) {
	for _, prefix := range schemaOrgIRIPrefixes {
		if name, ok := cutPrefixFold(value, prefix); ok {
			return typeName(name)
		}
	}
	if name, ok := cutPrefixFold(value, schemaOrgCURIEPrefix); ok {
		return typeName(name)
	}
	return "", false
}

// typeName drops a query, a fragment and one trailing slash from what follows the prefix,
// so https://schema.org/Product/ reads Product. A name that still holds a slash names a
// path, not a type. Case is kept: schema.org names are case-sensitive.
func typeName(name string) (string, bool) {
	if i := strings.IndexAny(name, iriSuffixDelimiters); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimSuffix(name, iriPathSeparator)
	if name == "" || strings.Contains(name, iriPathSeparator) {
		return "", false
	}
	return name, true
}

// rdfaSchemaOrgType resolves one typeof token: an absolute schema.org IRI or a schema:
// CURIE always, a term only under a schema.org vocab.
func rdfaSchemaOrgType(value string, schemaVocab bool) (string, bool) {
	if name, ok := schemaOrgLocalName(value); ok {
		return name, true
	}
	if schemaVocab && !strings.Contains(value, curieSeparator) {
		return value, true
	}
	return "", false
}

// isSchemaOrgVocab reports whether an RDFa vocab value names schema.org. The form without
// the trailing slash is common in the wild and accepted.
func isSchemaOrgVocab(value string) bool {
	value = strings.TrimSpace(value)
	for _, prefix := range schemaOrgIRIPrefixes {
		if strings.EqualFold(value, prefix) || strings.EqualFold(value, strings.TrimSuffix(prefix, iriPathSeparator)) {
			return true
		}
	}
	return false
}

// cutPrefixFold is strings.CutPrefix with a case-insensitive prefix. It reports false
// when nothing follows the prefix.
func cutPrefixFold(value, prefix string) (string, bool) {
	if len(value) <= len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return "", false
	}
	return value[len(prefix):], true
}
