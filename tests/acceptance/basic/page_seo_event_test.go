package acceptance_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/pkg/types"
	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
)

const (
	eventLogPollTimeout  = 10 * time.Second
	eventLogPollInterval = 100 * time.Millisecond
)

// The extractor's output has to survive four hops before anything downstream can read
// it: ExtractPageSEO builds the struct, ProcessContent carries it, BuildRequestEvent
// converts it, and the emitter writes it. Unit tests cover each hop in isolation. These
// specs cover the join, against the live pipeline, by reading values that only the
// extractor could have produced back out of the emitted event.
//
// title and index_status are the two PageSEO-sourced fields the event log exposes
// today. Every other extracted field, dates included, rides the same struct through the
// same conversion, so this pins the path they all travel.
var _ = Describe("PageSEO Event Flow", Serial, func() {

	// eventForResponse polls until the gateway has written the event for this request.
	// Emission happens after the response is served, so the line lands slightly after
	// the client sees the body.
	eventForResponse := func(resp *TestResponse) testutil.EventLogEntry {
		requestID := resp.Headers.Get(types.HeaderRequestID)
		Expect(requestID).NotTo(BeEmpty(), "Gateway must echo a request ID to correlate the event")

		var entry testutil.EventLogEntry
		Eventually(func() (bool, error) {
			found, ok, err := testutil.FindEventByRequestID(
				testutil.EventLogPath(testEnv.TempConfigDir), requestID)
			if err != nil || !ok {
				return false, err
			}
			entry = found
			return true, nil
		}, eventLogPollTimeout, eventLogPollInterval).Should(BeTrue(),
			"An event carrying request ID %s should reach the log", requestID)

		return entry
	}

	Context("Render path", func() {
		It("should carry extracted SEO values into the emitted event", func() {
			By("Rendering a page whose title only the extractor could report")
			resp := testEnv.RequestRender("/dates-test/render/article")
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp.Headers.Get("EC-Source")).To(Equal("render"), "Should be freshly rendered")

			By("Reading the emitted event back out of the log")
			entry := eventForResponse(resp)

			Expect(entry.EventType).To(Equal("render"), "Event type should record a fresh render")
			Expect(entry.URL).To(ContainSubstring("/dates-test/render/article"), "Event should name the requested URL")

			By("Verifying the values came from PageSEO, not from the request")
			Expect(entry.Title).To(Equal(testutil.DatesTestTitle),
				"Event title must be the <title> ExtractPageSEO read from the rendered DOM")
			Expect(entry.IndexStatus).To(Equal(int(types.IndexStatusIndexable)),
				"Event index status must be the value ExtractPageSEO computed")
		})
	})

	Context("Bypass path", func() {
		It("should carry extracted SEO values into the emitted event", func() {
			By("Bypassing the same page")
			resp := testEnv.RequestRender("/dates-test/bypass/article")
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp.Headers.Get("EC-Source")).To(Equal("bypass"), "Should be served without rendering")

			By("Reading the emitted event back out of the log")
			entry := eventForResponse(resp)

			Expect(entry.EventType).To(Equal("bypass"), "Event type should record a bypass")

			By("Verifying the bypass path extracts SEO from the fetched origin HTML")
			Expect(entry.Title).To(Equal(testutil.DatesTestTitle),
				"Event title must be the <title> ExtractPageSEO read from the fetched HTML")
			Expect(entry.IndexStatus).To(Equal(int(types.IndexStatusIndexable)),
				"Event index status must be the value ExtractPageSEO computed")
		})
	})
})
