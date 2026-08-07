package acceptance_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
)

// The render path must present the host's render key to the origin the same way bypass
// does, because origins gate on it. The blast radius is the constraint: the key belongs on
// same-origin requests only, never on the third-party domains a page happens to reference.
var _ = Describe("Render Key Forwarding To Origin", Serial, func() {

	Context("Rendered Page", func() {

		It("should send X-Render-Key on the main document and same-host XHR but not third-party", func() {
			expectedKey := testEnv.Config.Test.ValidAPIKey

			By("Rendering the page whose origin echoes the received render key")
			resp := testEnv.RequestRender(testutil.RenderKeyPagePath)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp.Headers.Get("EC-Source")).To(Equal("render"), "Should be freshly rendered")

			By("Verifying the main document carried the render key")
			Expect(resp.Body).To(ContainSubstring(testutil.RenderKeyDocumentMarker+expectedKey),
				"Main document request should carry X-Render-Key")

			By("Verifying the same-host XHR carried the render key")
			Expect(resp.Body).To(ContainSubstring(testutil.RenderKeySameHostMarker+expectedKey),
				"Same-host XHR should carry X-Render-Key")

			By("Verifying the third-party subresource did NOT carry the render key")
			Expect(resp.Body).To(ContainSubstring(testutil.RenderKeyThirdPartyMarker+testutil.RenderKeyAbsent),
				"Third-party request must not carry X-Render-Key")
			Expect(resp.Body).NotTo(ContainSubstring(testutil.RenderKeyThirdPartyMarker+expectedKey),
				"Render key must never leak to a third-party origin")
		})

		It("should not leave the render key absent on any same-origin request", func() {
			By("Rendering the page again")
			resp := testEnv.RequestRender(testutil.RenderKeyPagePath)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying neither same-origin marker reports an absent key")
			Expect(resp.Body).NotTo(ContainSubstring(testutil.RenderKeyDocumentMarker+testutil.RenderKeyAbsent),
				"Main document should never be fetched without the render key")
			Expect(resp.Body).NotTo(ContainSubstring(testutil.RenderKeySameHostMarker+testutil.RenderKeyAbsent),
				"Same-host XHR should never be fetched without the render key")
		})
	})
})
