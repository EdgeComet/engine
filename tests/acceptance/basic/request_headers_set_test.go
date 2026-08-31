package acceptance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
)

// headers.request_headers_set sends headers to the origin that the bot never sent, which is the
// only way an origin gating on a shared key or a tenant id can be served. Nothing about it is
// visible in a response to the bot, so every spec here reads the echo origin: what it reports is
// what EdgeComet actually put on the wire.
var _ = Describe("Request Headers Set", Serial, func() {
	const (
		internalBaseURL = "http://localhost:10071"
		internalAuthKey = "test-auth-key-12345"

		desktopDimensionID = 1
		forwardedAPIKey    = "forwarded-by-the-bot"
	)

	// Expected entries, spelled once so a value change in the host fixture fails in one place.
	hostAPIKey := testutil.HeaderEchoEntry(testutil.SetHeaderAPIKey, testutil.SetHeaderHostAPIKeyValue)
	ruleAPIKey := testutil.HeaderEchoEntry(testutil.SetHeaderAPIKey, testutil.SetHeaderRuleAPIKeyValue)
	hostTenant := testutil.HeaderEchoEntry(testutil.SetHeaderTenantID, testutil.SetHeaderTenantValue)
	globalMarker := testutil.HeaderEchoEntry(testutil.SetHeaderGlobal, testutil.SetHeaderGlobalValue)
	absentAPIKey := testutil.HeaderEchoEntry(testutil.SetHeaderAPIKey, testutil.HeaderEchoAbsent)
	absentTenant := testutil.HeaderEchoEntry(testutil.SetHeaderTenantID, testutil.HeaderEchoAbsent)
	absentGlobal := testutil.HeaderEchoEntry(testutil.SetHeaderGlobal, testutil.HeaderEchoAbsent)

	// Cache TTL on this host is minutes, so every spec asks for its own URL.
	uniquePath := func(basePath string) string {
		return fmt.Sprintf("%s?cb=%d", basePath, time.Now().UnixNano())
	}

	// requestRenderWithClientHeader drives the public render endpoint with one extra header from
	// the bot, which is how the forwarded-versus-configured collision is reproduced.
	requestRenderWithClientHeader := func(path, headerName, headerValue string) *TestResponse {
		targetURL := testEnv.Config.TestPagesURL() + path
		egURL := testEnv.Config.EGBaseURL() + "/render?url=" + url.QueryEscape(targetURL)

		req, err := http.NewRequest("GET", egURL, nil)
		if err != nil {
			return &TestResponse{Error: err}
		}
		req.Header.Set("X-Render-Key", testEnv.Config.Test.ValidAPIKey)
		req.Header.Set("User-Agent", "Googlebot/2.1 (+http://www.google.com/bot.html)")
		req.Header.Set(headerName, headerValue)

		resp, err := testEnv.HTTPClient.Do(req)
		if err != nil {
			return &TestResponse{Error: err}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		return &TestResponse{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       string(body),
			Error:      err,
		}
	}

	// requestRecache asks the Edge Gateway to precache one URL, the way the cache daemon does.
	requestRecache := func(targetURL string) *TestResponse {
		payload, err := json.Marshal(map[string]interface{}{
			"url":          targetURL,
			"host_id":      testEnv.Config.Test.HostID,
			"dimension_id": desktopDimensionID,
		})
		if err != nil {
			return &TestResponse{Error: err}
		}

		req, err := http.NewRequest("POST", internalBaseURL+"/internal/cache/recache", bytes.NewReader(payload))
		if err != nil {
			return &TestResponse{Error: err}
		}
		req.Header.Set("X-Internal-Auth", internalAuthKey)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return &TestResponse{Error: err}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		return &TestResponse{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       string(body),
			Error:      err,
		}
	}

	// requestHARRender drives the HAR debug endpoint, which answers with a HAR rather than the
	// page, so what the origin received has to be read from the origin's own record.
	requestHARRender := func(targetURL string) *TestResponse {
		params := url.Values{}
		params.Set("url", targetURL)
		params.Set("dimension", "desktop")

		req, err := http.NewRequest("GET", internalBaseURL+"/debug/har/render?"+params.Encode(), nil)
		if err != nil {
			return &TestResponse{Error: err}
		}
		req.Header.Set("X-Internal-Auth", internalAuthKey)

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return &TestResponse{Error: err}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		return &TestResponse{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       string(body),
			Error:      err,
		}
	}

	Context("Rendered Page", func() {

		It("should send the configured headers on the document and the same-host XHR but not third-party", func() {
			By("Rendering the page whose origin echoes the request headers it received")
			resp := testEnv.RequestRender(uniquePath(testutil.SetHeadersRenderPath))
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp.Headers.Get("EC-Source")).To(Equal("render"), "Should be freshly rendered")

			By("Verifying the main document carried the host's configured headers")
			Expect(resp.Body).To(ContainSubstring(testutil.SetHeadersDocumentMarker + hostAPIKey + hostTenant))

			By("Verifying the same-host XHR carried them as well")
			Expect(resp.Body).To(ContainSubstring(testutil.SetHeadersSameHostMarker + hostAPIKey + hostTenant))

			By("Verifying the third-party subresource carried none of them")
			Expect(resp.Body).To(ContainSubstring(
				testutil.SetHeadersThirdPartyMarker + absentAPIKey + absentTenant + absentGlobal))
			Expect(resp.Body).NotTo(ContainSubstring(testutil.SetHeadersThirdPartyMarker+hostAPIKey),
				"A configured value must never reach a third-party origin")
		})

		It("should let a URL rule override one header while the host's others are inherited", func() {
			By("Rendering a page matched by the overriding URL rule")
			resp := testEnv.RequestRender(uniquePath(testutil.SetHeadersOverridePath))
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying the rule's value replaced the host's, once, and the other key survived")
			Expect(resp.Body).To(ContainSubstring(testutil.SetHeadersDocumentMarker + ruleAPIKey + hostTenant))
			Expect(resp.Body).NotTo(ContainSubstring(testutil.SetHeaderHostAPIKeyValue),
				"The overridden host value must not reach the origin at all")
		})

		It("should send a globally configured header to a host that never asked for it", func() {
			By("Rendering a page of a host whose own configuration never names the global header")
			resp := testEnv.RequestRender(uniquePath(testutil.SetHeadersRenderPath))
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying the global value reached this host's origin")
			// This is the multi-tenant behaviour the load-time warning is about: a value set
			// globally goes to every host's origin. Pinned here so it cannot change silently.
			Expect(resp.Body).To(ContainSubstring(testutil.SetHeadersDocumentMarker + hostAPIKey + hostTenant + globalMarker))
		})
	})

	Context("Bypassed Request", func() {

		It("should send the configured headers on the origin fetch", func() {
			By("Requesting a page matched by a bypass URL rule")
			resp := testEnv.RequestRender(uniquePath(testutil.SetHeadersBypassPath))
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")
			Expect(resp.Headers.Get("EC-Source")).To(Equal("bypass"), "Should be a direct origin fetch")

			By("Verifying the origin fetch carried the configured headers")
			Expect(resp.Body).To(ContainSubstring(
				testutil.SetHeadersDocumentMarker + hostAPIKey + hostTenant + globalMarker))
		})

		It("should replace a forwarded header of the same name with the configured value", func() {
			By("Requesting with the bot sending its own value for a configured header")
			resp := requestRenderWithClientHeader(
				uniquePath(testutil.SetHeadersBypassPath), testutil.SetHeaderAPIKey, forwardedAPIKey)
			Expect(resp.Error).To(BeNil(), "Request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Status code should be 200")

			By("Verifying the origin received the configured value exactly once")
			// The forwarded header is in this host's safe_request_add list, so without the
			// replacement the origin would receive the bot's value as well as the configured
			// one. A real client cannot make the two names differ in case - both fasthttp and
			// net/http canonicalise header names on the wire - so the case-insensitive half of
			// the rule is pinned by the unit tests instead.
			Expect(resp.Body).To(ContainSubstring(testutil.SetHeadersDocumentMarker + hostAPIKey))
			Expect(resp.Body).NotTo(ContainSubstring(forwardedAPIKey),
				"The bot's value must not reach the origin alongside the configured one")
		})
	})

	Context("Precache", func() {

		It("should send the configured headers on a precached render", func() {
			path := uniquePath(testutil.SetHeadersRenderPath)
			targetURL := testEnv.Config.TestPagesURL() + path

			By("Asking the Edge Gateway to precache the page")
			resp := requestRecache(targetURL)
			Expect(resp.Error).To(BeNil(), "Recache request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Recache should succeed: "+resp.Body)

			By("Verifying the origin received the configured headers")
			// Precache has no incoming request, so this is the case a safe_request_add
			// workaround structurally cannot reach.
			received := testEnv.TestServer.ReceivedHeaders(path)
			Expect(received).NotTo(BeNil(), "The origin should have been fetched")
			Expect(received.Values(testutil.SetHeaderAPIKey)).To(Equal([]string{testutil.SetHeaderHostAPIKeyValue}))
			Expect(received.Get(testutil.SetHeaderTenantID)).To(Equal(testutil.SetHeaderTenantValue))
			Expect(received.Get(testutil.SetHeaderGlobal)).To(Equal(testutil.SetHeaderGlobalValue))
		})

		It("should send the configured headers on a precached bypass fetch", func() {
			path := uniquePath(testutil.SetHeadersBypassCachePath)
			targetURL := testEnv.Config.TestPagesURL() + path

			By("Asking the Edge Gateway to precache a URL its rules mark bypass")
			resp := requestRecache(targetURL)
			Expect(resp.Error).To(BeNil(), "Recache request should not error")
			Expect(resp.StatusCode).To(Equal(200), "Recache should succeed: "+resp.Body)

			By("Verifying the origin received the configured headers")
			received := testEnv.TestServer.ReceivedHeaders(path)
			Expect(received).NotTo(BeNil(), "The origin should have been fetched")
			Expect(received.Values(testutil.SetHeaderAPIKey)).To(Equal([]string{testutil.SetHeaderHostAPIKeyValue}))
			Expect(received.Get(testutil.SetHeaderTenantID)).To(Equal(testutil.SetHeaderTenantValue))
		})
	})

	Context("HAR Debug Render", func() {

		It("should send the configured headers on the debug render", func() {
			path := uniquePath(testutil.SetHeadersRenderPath)
			targetURL := testEnv.Config.TestPagesURL() + path

			By("Rendering the page through the HAR debug endpoint")
			resp := requestHARRender(targetURL)
			Expect(resp.Error).To(BeNil(), "HAR render should not error")
			Expect(resp.StatusCode).To(Equal(200), "HAR render should succeed: "+resp.Body)

			By("Verifying the origin received the configured headers")
			// The debug render has to make the same request production does, or it answers a
			// different question than the one an operator is debugging.
			received := testEnv.TestServer.ReceivedHeaders(path)
			Expect(received).NotTo(BeNil(), "The origin should have been fetched")
			Expect(received.Values(testutil.SetHeaderAPIKey)).To(Equal([]string{testutil.SetHeaderHostAPIKeyValue}))
			Expect(received.Get(testutil.SetHeaderTenantID)).To(Equal(testutil.SetHeaderTenantValue))
			Expect(received.Get(testutil.SetHeaderGlobal)).To(Equal(testutil.SetHeaderGlobalValue))
		})
	})
})
