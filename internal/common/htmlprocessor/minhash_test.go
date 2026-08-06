package htmlprocessor

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expectedTruncationMask deliberately duplicates the production mask instead of
// referencing it, so that a change to the stored value width fails here.
const expectedTruncationMask uint64 = 0xFFFFFFFF

const (
	// Match bands for the similarity tests. They are wide and non-overlapping:
	// the algorithm is deterministic, so a violation means the algorithm drifted,
	// never that the test flaked.
	disjointMaxMatches  = 2
	smallEditMinMatches = 20
	halfRewriteMaxMatch = 20

	similarityTextWords = 500
)

// matchingSlots counts positionally equal slots, the estimator for Jaccard
// similarity between two fingerprints.
func matchingSlots(a, b []uint64) int {
	if len(a) != len(b) {
		return 0
	}
	matches := 0
	for i := range a {
		if a[i] == b[i] {
			matches++
		}
	}
	return matches
}

// generateWords builds a deterministic word stream whose tokens are unique within
// the stream, so two streams with different prefixes share no shingle at all.
func generateWords(prefix string, count int) []string {
	words := make([]string, count)
	for i := range words {
		words[i] = prefix + strconv.Itoa(i)
	}
	return words
}

// referenceMinHash recomputes the signature with a straightforward per-shingle
// strings.Join, serving as an independent oracle for the buffer-reusing production
// loop.
func referenceMinHash(words []string) []uint64 {
	if len(words) < minhashShingleSize {
		return nil
	}

	signature := make([]uint64, minhashSignatureSize)
	for i := range signature {
		signature[i] = math.MaxUint64
	}

	for start := 0; start+minhashShingleSize <= len(words); start++ {
		h := xxhash.Sum64String(strings.Join(words[start:start+minhashShingleSize], " "))
		for i, permutation := range minhashPermutations {
			permuted := permutation[0]*h + permutation[1]
			if permuted < signature[i] {
				signature[i] = permuted
			}
		}
	}

	for i := range signature {
		signature[i] &= expectedTruncationMask
	}
	return signature
}

// TestComputePageMinHash_ConformanceVector derives every slot from first principles
// for a single-shingle input. It pins the join separator and the operator order of
// the permutation, independently of how the production loop is written.
//
// It says nothing about where the truncation sits relative to the minimum: over a
// single value the minimum is the identity, so masking before or after it gives the
// same slot. That ordering is pinned by TestComputePageMinHash_TruncationAfterMinimum.
func TestComputePageMinHash_ConformanceVector(t *testing.T) {
	words := []string{"alpha", "beta", "gamma"}

	h := xxhash.Sum64String("alpha beta gamma")

	signature := computePageMinHash(words)
	require.Len(t, signature, minhashSignatureSize)

	for i, permutation := range minhashPermutations {
		expected := (permutation[0]*h + permutation[1]) & expectedTruncationMask
		assert.Equal(t, expected, signature[i], "slot %d", i)
	}
}

// TestComputePageMinHash_TruncationAfterMinimum extends the conformance vector to the
// smallest input that can tell the two orderings apart: two shingles whose full 64-bit
// argmin differs from their low-32-bit argmin. Expected values are derived from first
// principles - minimum over the untruncated permuted values, mask applied afterwards.
//
// The guard only bites while the two orderings actually disagree somewhere, so the
// test also asserts that the chosen words keep them apart. If that assertion ever
// fails, replace the words rather than dropping it.
func TestComputePageMinHash_TruncationAfterMinimum(t *testing.T) {
	words := []string{"alpha", "beta", "gamma", "delta"}

	firstHash := xxhash.Sum64String("alpha beta gamma")
	secondHash := xxhash.Sum64String("beta gamma delta")

	expected := make([]uint64, minhashSignatureSize)
	maskedBeforeMinimum := make([]uint64, minhashSignatureSize)
	for i, permutation := range minhashPermutations {
		firstPermuted := permutation[0]*firstHash + permutation[1]
		secondPermuted := permutation[0]*secondHash + permutation[1]

		expected[i] = min(firstPermuted, secondPermuted) & expectedTruncationMask
		maskedBeforeMinimum[i] = min(firstPermuted&expectedTruncationMask, secondPermuted&expectedTruncationMask)
	}

	require.NotEqual(t, expected, maskedBeforeMinimum,
		"conformance vector no longer discriminates between the two orderings")
	assert.Equal(t, expected, computePageMinHash(words))
}

// goldenWords is the frozen input of the golden-value test below.
var goldenWords = []string{
	"the", "quick", "brown", "fox", "jumps", "over", "the", "lazy", "dog.",
	"pack", "my", "box", "with", "five", "dozen", "liquor", "jugs.",
	"how", "vexingly", "quick", "daft", "zebras", "jump!",
}

// goldenSignature is the fingerprint of goldenWords.
// FROZEN: a mismatch means the fingerprint format changed and every stored
// fingerprint became incomparable. Do not regenerate these values to make the test
// pass; revert the change that moved them instead.
var goldenSignature = []uint64{
	3253288800, 1769780479, 4276382834, 2678387471,
	1882304182, 2476634949, 1019282428, 3847123141,
	1591113385, 191780076, 1921509379, 1175186342,
	1100470579, 2367102894, 4083047119, 2294656446,
	1671253749, 2636154873, 1609115157, 1971915825,
	1193872193, 1820019246, 3481626754, 212408708,
}

func TestComputePageMinHash_GoldenValues(t *testing.T) {
	assert.Equal(t, goldenSignature, computePageMinHash(goldenWords))
}

func TestComputePageMinHash_Deterministic(t *testing.T) {
	first := computePageMinHash(goldenWords)
	second := computePageMinHash(append([]string(nil), goldenWords...))

	require.Len(t, first, minhashSignatureSize)
	assert.Equal(t, minhashSignatureSize, matchingSlots(first, second))
}

func TestComputePageMinHash_ShortInput(t *testing.T) {
	assert.Nil(t, computePageMinHash(nil))
	assert.Nil(t, computePageMinHash([]string{}))
	assert.Nil(t, computePageMinHash([]string{"one"}))
	assert.Nil(t, computePageMinHash([]string{"one", "two"}))

	assert.Len(t, computePageMinHash([]string{"one", "two", "three"}), minhashSignatureSize)
}

func TestComputePageMinHash_DisjointTexts(t *testing.T) {
	first := computePageMinHash(generateWords("alpha", similarityTextWords))
	second := computePageMinHash(generateWords("omega", similarityTextWords))

	assert.LessOrEqual(t, matchingSlots(first, second), disjointMaxMatches)
}

func TestComputePageMinHash_ChangeMagnitude(t *testing.T) {
	base := generateWords("word", similarityTextWords)
	baseSignature := computePageMinHash(base)

	// A single replaced word destroys the three shingles covering it, so the
	// signature must stay nearly intact.
	smallEdit := append([]string(nil), base...)
	smallEdit[similarityTextWords/2] = "replacement"
	smallEditMatches := matchingSlots(baseSignature, computePageMinHash(smallEdit))
	assert.GreaterOrEqual(t, smallEditMatches, smallEditMinMatches)

	// Replacing every second word leaves no original shingle intact.
	halfRewrite := append([]string(nil), base...)
	for i := 0; i < len(halfRewrite); i += 2 {
		halfRewrite[i] = "rewritten" + strconv.Itoa(i)
	}
	halfRewriteMatches := matchingSlots(baseSignature, computePageMinHash(halfRewrite))
	assert.Less(t, halfRewriteMatches, halfRewriteMaxMatch)
	assert.Less(t, halfRewriteMatches, smallEditMatches)
}

// TestComputePageMinHash_DuplicateShingles uses periodic sequences: two and three
// repetitions of the same triple yield the identical shingle set with different
// multiplicities, and the minimum is idempotent, so the signatures must be equal.
// Appending a duplicated sentence instead would create bridge shingles across the
// junction and legitimately change the signature.
func TestComputePageMinHash_DuplicateShingles(t *testing.T) {
	period := []string{"x", "y", "z"}

	twice := append(append([]string(nil), period...), period...)
	thrice := append(append(append([]string(nil), period...), period...), period...)

	assert.Equal(t, computePageMinHash(twice), computePageMinHash(thrice))
}

func TestComputePageMinHash_ValueRange(t *testing.T) {
	inputs := [][]string{
		goldenWords,
		{"one", "two", "three"},
		generateWords("token", similarityTextWords),
	}

	for _, words := range inputs {
		signature := computePageMinHash(words)
		require.Len(t, signature, minhashSignatureSize)
		for i, value := range signature {
			assert.LessOrEqual(t, value, expectedTruncationMask, "slot %d exceeds 32 bits", i)
		}
	}
}

// TestComputePageMinHash_BufferReuse guards the reused shingle buffer: descending
// word lengths mean a shorter shingle follows a longer one, which would expose
// trailing bytes of the previous shingle if the buffer were not reset.
func TestComputePageMinHash_BufferReuse(t *testing.T) {
	words := []string{
		"extraordinarily", "considerable", "substantial", "moderate", "small", "a",
		"b", "microscopic", "infinitesimally", "c",
	}

	assert.Equal(t, referenceMinHash(words), computePageMinHash(words))
}

// goldenDocumentURL is arbitrary; the fingerprint never reads it.
const goldenDocumentURL = "https://example.com/golden"

// goldenDocument pins the tokenizer together with the arithmetic. The golden-value
// test above starts from a []string, so it freezes computePageMinHash and nothing
// else; every step that turns HTML into that slice - the boilerplate selector, the
// whitespace split, the lowercasing, entity decoding - is equally part of the stored
// format, and this is what holds those still.
//
// The document deliberately carries every behavior that is easy to change by
// accident: text inside each removed subtree, words fused across inline markup, a
// <br>, a removed subtree with no whitespace around it, adjacent list items, decoded
// entities needing unicode lowercasing, runs of mixed whitespace, and attached
// punctuation.
//
// FROZEN: the document and both expectations below. A diff in any of them means the
// fingerprint format moved and every stored fingerprint became incomparable. Do not
// regenerate these to make the test pass; revert whatever moved them.
const goldenDocument = `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<title>Golden Fingerprint Document</title>
	<meta name="description" content="Head text never reaches the fingerprint">
</head>
<body>
	<header><h1>Masthead heading excluded</h1></header>
	<nav><a href="/one">Navigation one</a> <a href="/two">Navigation two</a></nav>
	<main>
		<h2>Frozen Tokenizer Document</h2>
		<p>The Quick Brown Fox jumps over the LAZY dog.</p>
		<p>Inline markup fuses: hyper<b>text</b>markup and 19<em>.99</em> are single tokens.</p>
		<p>A stripped subtree fuses its neighbours: left<nav>NAVIGATION</nav>right.</p>
		<p>Line breaks fuse as well: one<br>two.</p>
		<p>Whitespace   collapses	across
		   newlines and tabs.</p>
		<p>Entities decode: caf&eacute; na&iuml;ve r&eacute;sum&eacute; &amp; more.</p>
		<ul><li>List item one</li><li>List item two</li></ul>
		<form><label>Form label excluded</label></form>
		<p>Punctuation stays attached: price: $19.99, really?</p>
	</main>
	<aside><p>Sidebar copy excluded</p></aside>
	<script>var excluded = "script text";</script>
	<style>.excluded { color: red }</style>
	<noscript>Noscript fallback excluded</noscript>
	<footer><p>Colophon excluded</p></footer>
</body>
</html>`

// goldenDocumentWords is the token stream goldenDocument produces. It is asserted
// alongside the signature because a signature mismatch on its own cannot say what
// moved, while a token diff names it: hypertextmarkup and 19.99 pin inline fusion,
// leftright. pins fusion across a removed subtree, onetwo. pins the <br>, onelist
// pins the list-item boundary, and the accented tokens pin unicode lowercasing.
var goldenDocumentWords = []string{
	"frozen", "tokenizer", "document", "the", "quick", "brown",
	"fox", "jumps", "over", "the", "lazy", "dog.",
	"inline", "markup", "fuses:", "hypertextmarkup", "and", "19.99",
	"are", "single", "tokens.", "a", "stripped", "subtree",
	"fuses", "its", "neighbours:", "leftright.", "line", "breaks",
	"fuse", "as", "well:", "onetwo.", "whitespace", "collapses",
	"across", "newlines", "and", "tabs.", "entities", "decode:",
	"café", "naïve", "résumé", "&", "more.", "list",
	"item", "onelist", "item", "two", "punctuation", "stays",
	"attached:", "price:", "$19.99,", "really?",
}

var goldenDocumentSignature = []uint64{
	411583956, 1769780479, 4276382834, 2678387471,
	1738993558, 3472138180, 2614037829, 2340300577,
	317985285, 1071023191, 3443797967, 953613602,
	68839313, 667554346, 368821631, 2690304359,
	474036695, 978556424, 2209315895, 4159815557,
	1193872193, 1648723075, 3581608988, 2663634423,
}

// goldenExcludedWords appear only inside subtrees the extractor removes. Asserting
// their absence states the selector's job directly, so widening or narrowing it fails
// with a readable message rather than only as a shifted signature.
var goldenExcludedWords = []string{
	"masthead", "navigation", "sidebar", "colophon",
	"excluded", "noscript", "fallback", "color",
}

func TestExtractPageSEO_GoldenDocument(t *testing.T) {
	doc, err := ParseWithDOM([]byte(goldenDocument))
	require.NoError(t, err)

	words := extractBodyWords(doc.GoQueryDoc())
	require.Equal(t, goldenDocumentWords, words,
		"the token stream is frozen; a diff here is a breaking change to the fingerprint format")

	for _, excluded := range goldenExcludedWords {
		for _, word := range words {
			assert.NotContains(t, word, excluded,
				"%q comes from a subtree the extractor must remove", excluded)
		}
	}

	seo := doc.ExtractPageSEO(200, goldenDocumentURL)
	assert.Equal(t, goldenDocumentSignature, seo.PageMinHash,
		"the fingerprint of a fixed document is frozen end to end, tokenizer included")
	assert.Equal(t, len(goldenDocumentWords), seo.WordCount)
}
