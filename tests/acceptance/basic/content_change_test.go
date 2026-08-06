package acceptance_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
)

// Similarity between two page versions is a pure function of their two texts, so these
// specs compare fingerprints of DIFFERENT fixture URLs with controlled word overlap
// instead of staging a cache expiry to simulate one page changing over time. Every
// observed count below is a constant: the algorithm is deterministic and the corpus is a
// set of committed files.
//
// SHINGLE ARITHMETIC BEHIND THE BANDS - recalibrate these if you touch the corpus.
//
// The corpus is 600 body words, so S = 598 three-word shingles. Replacing m scattered
// single words, each at least three positions from the next, destroys 3m shingles and
// creates 3m new ones:
//
//	J = (S - 3m) / (S + 3m)
//
// Word-level change is amplified roughly threefold. small-change replaces m = 6 words
// (1 percent of the text) at positions 25, 125, ... 525, giving J = 580/616 = 0.94.
//
// A contiguous half-replacement is a different shape entirely: the untouched first half
// still contributes S/2 - 2 = 298 intact shingles inside a union of 898, giving
// J = 0.33. Scattering 300 replacements over the same text would destroy every shingle
// and drive J to ~0 instead, which is why half-change replaces a CONTIGUOUS block. A
// future fixture author who "simplifies" it into scattered replacements moves the count
// out of the band.
//
// full-change draws from a vocabulary with no word in common with the base corpus, so no
// shingle can be shared and J is exactly 0.
//
// boilerplate-only differs from base only inside nav, header, aside and footer, which the
// body-text extractor removes before shingling, so its body text is byte-identical.
//
// Observed match counts against base on the committed corpus, render path (the bypass
// path reproduces them; half-change measures 5 there too):
//
//	identical         24 of 24   J = 1.00
//	boilerplate-only  24 of 24   J = 1.00
//	small-change      23 of 24   J = 0.94, predicted 22.6
//	half-change        5 of 24   J = 0.33, predicted 8.0
//	full-change        0 of 24   J = 0.00
//
// The gap between predicted and observed on half-change is estimator noise, not drift: 24
// slots sample the Jaccard with a standard deviation near 2.3 slots, and every count above
// is a fixed number for these fixture bytes rather than a value that varies between runs.
// The bands are sized for that noise, which is why they are far wider than the predictions.
const (
	fingerprintSlotCount = 24

	// fingerprintValueCeiling is the exclusive upper bound of a slot: values are
	// truncated to their low 32 bits before they leave the extractor.
	fingerprintValueCeiling = uint64(1) << 32

	// Bands are wide and non-overlapping, so a violation is fixture or algorithm drift
	// rather than flakiness.
	smallChangeMinSlots = 18
	halfChangeMinSlots  = 3
	halfChangeMaxSlots  = 17
	fullChangeMaxSlots  = 3
)

var _ = Describe("Page Content Fingerprint", Serial, func() {

	// fingerprintOf requests one fixture variant and returns the fingerprint the gateway
	// logged for it, asserting the pipeline that produced it along the way.
	fingerprintOf := func(prefix, variant, expectedSource string) []uint64 {
		resp := testEnv.RequestRender(prefix + variant)
		Expect(resp.Error).To(BeNil(), "Request for %s should not error", variant)
		Expect(resp.StatusCode).To(Equal(200), "Fixture %s should serve 200", variant)
		Expect(resp.Headers.Get("EC-Source")).To(Equal(expectedSource),
			"Fixture %s should be served through the %s pipeline", variant, expectedSource)

		return eventForResponse(resp).PageMinHash
	}

	renderedFingerprint := func(variant string) []uint64 {
		return fingerprintOf(testutil.ContentChangeRenderPath, variant, "render")
	}

	bypassedFingerprint := func(variant string) []uint64 {
		return fingerprintOf(testutil.ContentChangeBypassPath, variant, "bypass")
	}

	Context("Render path", func() {
		It("should place each variant's similarity to the base text in its calibrated band", func() {
			By("Fingerprinting the reference corpus")
			base := renderedFingerprint(testutil.ContentChangeBase)
			Expect(base).To(HaveLen(fingerprintSlotCount),
				"The base fixture must carry a full signature; a nil one means the corpus never reached the extractor")

			By("A byte-identical body text reproducing the fingerprint exactly")
			Expect(testutil.MatchingSlots(base, renderedFingerprint(testutil.ContentChangeIdentical))).
				To(Equal(fingerprintSlotCount),
					"Identical body text must match on every slot")

			By("Boilerplate-only edits leaving the fingerprint untouched")
			Expect(testutil.MatchingSlots(base, renderedFingerprint(testutil.ContentChangeBoilerplateOnly))).
				To(Equal(fingerprintSlotCount),
					"nav, header, aside and footer are stripped before shingling, so rewriting them must not move a single slot")

			By("A one percent scattered edit staying close to the base")
			smallChange := testutil.MatchingSlots(base, renderedFingerprint(testutil.ContentChangeSmall))
			Expect(smallChange).To(BeNumerically(">=", smallChangeMinSlots),
				"Replacing six of 600 words destroys about 18 of 598 shingles, which is an estimated similarity of 0.94")
			Expect(smallChange).To(BeNumerically("<", fingerprintSlotCount),
				"A real word change must still be visible; an exact match means the edit never reached the extractor")

			By("A contiguous half rewrite landing near one third similarity")
			halfChange := testutil.MatchingSlots(base, renderedFingerprint(testutil.ContentChangeHalf))
			Expect(halfChange).To(BeNumerically(">=", halfChangeMinSlots),
				"The untouched half still shares 298 of a 898 shingle union")
			Expect(halfChange).To(BeNumerically("<=", halfChangeMaxSlots),
				"Half the text is new, so this must sit far below the change threshold")

			By("A fully rewritten text sharing almost nothing")
			Expect(testutil.MatchingSlots(base, renderedFingerprint(testutil.ContentChangeFull))).
				To(BeNumerically("<=", fullChangeMaxSlots),
					"A disjoint vocabulary shares no shingle, so matches can only come from permutation collisions")
		})
	})

	Context("Bypass path", func() {
		It("should carry fingerprints with the same magnitudes as the render path", func() {
			By("Fingerprinting the reference corpus without rendering it")
			base := bypassedFingerprint(testutil.ContentChangeBase)
			Expect(base).To(HaveLen(fingerprintSlotCount),
				"A bypassed HTML response must be fingerprinted like any other page")

			By("Agreeing with the rendered fingerprint of the same markup")
			Expect(testutil.MatchingSlots(base, renderedFingerprint(testutil.ContentChangeBase))).
				To(Equal(fingerprintSlotCount),
					"The same body text must fingerprint identically whether Chrome or the origin fetch produced the HTML")

			By("Reproducing the identical-text result")
			Expect(testutil.MatchingSlots(base, bypassedFingerprint(testutil.ContentChangeIdentical))).
				To(Equal(fingerprintSlotCount),
					"Identical body text must match on every slot on the bypass path too")

			By("Reproducing the contiguous half rewrite band")
			halfChange := testutil.MatchingSlots(base, bypassedFingerprint(testutil.ContentChangeHalf))
			Expect(halfChange).To(BeNumerically(">=", halfChangeMinSlots))
			Expect(halfChange).To(BeNumerically("<=", halfChangeMaxSlots),
				"The bypass pipeline must measure the same magnitude of change as the render pipeline")
		})
	})

	Context("JavaScript-rendered content", func() {
		It("should fingerprint the post-JavaScript DOM rather than the origin HTML", func() {
			By("Fingerprinting the statically served corpus")
			base := renderedFingerprint(testutil.ContentChangeBase)
			Expect(base).To(HaveLen(fingerprintSlotCount))

			By("Fingerprinting a page whose origin HTML has an empty body and fetches the same corpus")
			injected := renderedFingerprint(testutil.ContentChangeJSInjected)
			Expect(injected).To(HaveLen(fingerprintSlotCount),
				"An empty signature means Chrome captured the page before the AJAX response arrived")

			Expect(testutil.MatchingSlots(base, injected)).To(Equal(fingerprintSlotCount),
				"The fingerprint runs on the rendered DOM, so injecting the corpus must reproduce the static page slot for slot")
		})
	})

	Context("Signature shape", func() {
		It("should emit exactly 24 truncated values per page", func() {
			signature := renderedFingerprint(testutil.ContentChangeBase)

			Expect(signature).To(HaveLen(fingerprintSlotCount),
				"The signature width is part of the stored format")
			for slot, value := range signature {
				Expect(value).To(BeNumerically("<", fingerprintValueCeiling),
					"Slot %d must be truncated to its low 32 bits", slot)
			}
		})

		It("should omit the fingerprint for a page with too little text to shingle", func() {
			By("Rendering a page whose body holds two words")
			signature := renderedFingerprint(testutil.ContentChangeTwoWords)

			Expect(signature).To(BeNil(),
				"Two words form no 3-word shingle, so the event must carry an empty fingerprint field")
		})
	})
})
