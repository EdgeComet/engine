package acceptance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/edgecomet/engine/internal/edge/cache"
	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
)

const (
	wireSpellingLabel = "wire-spelling"

	wireDesktopDimensionID = 1
	wireDesktopDimension   = "desktop"
	wireRecacheTimeout     = 60 * time.Second

	// wireCommaRest and wireEncodedRest are the two spellings of the reported meblini URL tail:
	// one cache key, and the origin treats only the escaped one as canonical.
	wireCommaRest   = "a,b/53"
	wireEncodedRest = "a%2Cb/53"

	wireConcurrentRequests = 5
	wireOriginDelay        = 2 * time.Second

	wireSourceBypass      = "bypass"
	wireSourceBypassCache = "bypass_cache"
	wireSourceRender      = "render"
	wireSourceRenderCache = "render_cache"

	// wireSeededFetchURLField and wireSeededFetchURLRIDField name the hash fields that carry the
	// spelling that produced an entry and the request that wrote it.
	wireSeededFetchURLField    = "fetch_url"
	wireSeededFetchURLRIDField = "fetch_url_rid"
)

func wireToken(id string) string {
	return fmt.Sprintf("%s-%d", id, time.Now().UnixNano())
}

func wireURL(path string) string {
	return testEnv.Config.TestPagesURL() + path
}

// requestWire sends path and asserts the gateway answered at all.
func requestWire(request func(string) *TestResponse, path string) *TestResponse {
	resp := request(path)
	Expect(resp.Error).NotTo(HaveOccurred())
	return resp
}

// expectEncodedArrival requests rest under an ok page through the encoded integration and asserts
// the origin received the request-target ending in expectedRest, once.
func expectEncodedArrival(id, kind, rest, expectedRest string) {
	token := wireToken(id)
	requestWire(testEnv.RequestRenderEncoded, testutil.WirePath(kind, token, testutil.WireModeOK, rest))
	Expect(testEnv.TestServer.WireRequests(token)).To(Equal(
		[]string{testutil.WirePath(kind, token, testutil.WireModeOK, expectedRest)}))
}

// seededRedirect is a metadata-only redirect entry as the gateway writes it.
func seededRedirect(cacheKey, entryURL, location, source string, expiresAt time.Time) *cache.CacheMetadata {
	now := time.Now().UTC()
	return &cache.CacheMetadata{
		Key:        cacheKey,
		URL:        entryURL,
		HostID:     testEnv.Config.Test.HostID,
		Dimension:  wireDesktopDimension,
		RequestID:  "seed-" + cacheKey,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		LastAccess: now,
		Source:     source,
		StatusCode: http.StatusMovedPermanently,
		Headers:    map[string][]string{"Location": {location}},
	}
}

func wireCacheKey(path string) string {
	cacheKey, err := testEnv.GetCacheKey(wireURL(path), wireDesktopDimension)
	Expect(err).NotTo(HaveOccurred())
	return cacheKey
}

// requestWireRecache asks the gateway to precache one URL, as the cache daemon does.
func requestWireRecache(targetURL string) *TestResponse {
	payload, err := json.Marshal(map[string]interface{}{
		"url":          targetURL,
		"host_id":      testEnv.Config.Test.HostID,
		"dimension_id": wireDesktopDimensionID,
	})
	Expect(err).NotTo(HaveOccurred())

	req, err := http.NewRequest(http.MethodPost, testutil.EGInternalBaseURL+"/internal/cache/recache", bytes.NewReader(payload))
	Expect(err).NotTo(HaveOccurred())
	req.Header.Set(testutil.EGInternalAuthHeader, testutil.EGInternalAuthKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: wireRecacheTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return &TestResponse{Error: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	return &TestResponse{StatusCode: resp.StatusCode, Headers: resp.Header, Body: string(body), Error: err}
}

func escapeEntries(id string, kinds, escapes []string) []TableEntry {
	var entries []TableEntry
	for _, kind := range kinds {
		for _, escape := range escapes {
			entries = append(entries, Entry(fmt.Sprintf("%s %s %s", id, kind, escape), id, kind, escape))
		}
	}
	return entries
}

var _ = Describe("URL wire spelling", Serial, Label(wireSpellingLabel), func() {
	bothKinds := []string{testutil.WireKindBypass, testutil.WireKindRender}
	renderKind := []string{testutil.WireKindRender}

	Context("RED: the origin receives the bot's spelling", func() {

		It("RF-01 bypass: an encoded %2C path arrives byte-identical", func() {
			token := wireToken("rf01")
			path := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, wireEncodedRest)

			resp := requestWire(testEnv.RequestRenderEncoded, path)

			Expect(testEnv.TestServer.WireRequests(token)).To(Equal([]string{path}))
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("RF-02 render: an encoded %2C path arrives byte-identical through Chrome", func() {
			token := wireToken("rf02")
			path := testutil.WirePath(testutil.WireKindRender, token, testutil.WireModeOK, wireEncodedRest)

			resp := requestWire(testEnv.RequestRenderEncoded, path)

			Expect(testEnv.TestServer.WireRequests(token)).To(Equal([]string{path}))
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		DescribeTable("RF-03 escaped reserved characters arrive byte-identical",
			func(id, kind, escape string) {
				expectEncodedArrival(id, kind, "x"+escape+"y", "x"+escape+"y")
			},
			escapeEntries("RF-03", bothKinds, []string{"%24", "%26", "%2B", "%2C", "%3A", "%3B", "%3D", "%40"}),
		)

		DescribeTable("RF-04 render: escapes Chrome would send literal arrive byte-identical",
			func(id, kind, escape string) {
				expectEncodedArrival(id, kind, "x"+escape+"y", "x"+escape+"y")
			},
			escapeEntries("RF-04", renderKind, []string{"%21", "%27", "%28", "%29", "%2A", "%5B", "%5D", "%25zz"}),
		)

		DescribeTable("RF-05 lowercase hex arrives with its case kept",
			func(id, kind, escape string) {
				expectEncodedArrival(id, kind, "a"+escape+"b", "a"+escape+"b")
			},
			escapeEntries("RF-05", bothKinds, []string{"%2c"}),
		)

		DescribeTable("RF-06 %3F and %23 in the path arrive byte-identical",
			func(id, kind, escape string) {
				expectEncodedArrival(id, kind, "x"+escape+"y", "x"+escape+"y")
			},
			escapeEntries("RF-06", bothKinds, []string{"%3F", "%23"}),
		)

		It("RF-07 bypass: %2541 arrives as %2541", func() {
			expectEncodedArrival("rf07", testutil.WireKindBypass, "50%2541", "50%2541")
		})

		It("RF-08 bypass: a path ending in %0A reaches the origin and its 404 is served", func() {
			token := wireToken("rf08")
			path := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeStatus, "p%0A")
			testEnv.TestServer.SetWireResponse(token, http.StatusNotFound, "")

			resp := requestWire(testEnv.RequestRenderEncoded, path)

			Expect(testEnv.TestServer.WireRequests(token)).To(Equal([]string{path}))
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("RF-09 bypass: the canonical %2C spelling is served 200 twice", func() {
			token := wireToken("rf09")
			path := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeCanon, wireEncodedRest)
			testEnv.TestServer.SetWireCanonical(token, path)

			first := requestWire(testEnv.RequestRenderEncoded, path)
			Expect(first.StatusCode).To(Equal(http.StatusOK))

			second := requestWire(testEnv.RequestRenderEncoded, path)
			Expect(second.StatusCode).To(Equal(http.StatusOK))
		})

		It("RF-10 bypass: a comma redirect cached first does not loop the %2C spelling", func() {
			token := wireToken("rf10")
			commaPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeCanon, wireCommaRest)
			encodedPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeCanon, wireEncodedRest)
			testEnv.TestServer.SetWireCanonical(token, encodedPath)

			By("Requesting the comma spelling, which the origin redirects to %2C")
			comma := requestWire(testEnv.RequestRenderEncoded, commaPath)
			Expect(comma.StatusCode).To(Equal(http.StatusMovedPermanently))
			Expect(comma.Headers.Get("Location")).To(Equal(wireURL(encodedPath)))

			By("Requesting the %2C spelling")
			encoded := requestWire(testEnv.RequestRenderEncoded, encodedPath)
			Expect(encoded.StatusCode).To(Equal(http.StatusOK))

			By("Requesting the comma spelling again")
			again := requestWire(testEnv.RequestRenderEncoded, commaPath)
			Expect(again.StatusCode).To(Equal(http.StatusOK))
			Expect(again.Headers.Get("EC-Source")).To(Equal(wireSourceBypassCache))
		})

		It("RF-11 bypass: a pre-fix poisoned redirect heals on the %2C request", func() {
			token := wireToken("rf11")
			commaPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeCanon, wireCommaRest)
			encodedPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeCanon, wireEncodedRest)
			testEnv.TestServer.SetWireCanonical(token, encodedPath)

			cacheKey := wireCacheKey(encodedPath)
			Expect(testEnv.SeedCacheEntry(cacheKey, seededRedirect(cacheKey, wireURL(commaPath), wireURL(encodedPath),
				cache.SourceBypass, time.Now().UTC().Add(time.Hour)), nil)).To(Succeed())

			first := requestWire(testEnv.RequestRenderEncoded, encodedPath)
			Expect(first.StatusCode).To(Equal(http.StatusOK))

			second := requestWire(testEnv.RequestRenderEncoded, encodedPath)
			Expect(second.StatusCode).To(Equal(http.StatusOK))
			Expect(second.Headers.Get("EC-Source")).To(Equal(wireSourceBypassCache))
			Expect(testEnv.TestServer.WireCount(token)).To(Equal(1))
		})

		It("RF-12 bypass: a recache of the normalized URL fetches the spelling served live", func() {
			token := wireToken("rf12")
			commaPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, wireCommaRest)
			encodedPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, wireEncodedRest)

			live := requestWire(testEnv.RequestRenderEncoded, encodedPath)
			Expect(live.StatusCode).To(Equal(http.StatusOK))

			recache := requestWireRecache(wireURL(commaPath))
			Expect(recache.Error).NotTo(HaveOccurred())
			Expect(recache.StatusCode).To(Equal(http.StatusOK), recache.Body)

			Expect(testEnv.TestServer.WireRequests(token)).To(Equal([]string{encodedPath, encodedPath}))
		})

		It("RF-13 render: the canonical %2C spelling renders 200", func() {
			token := wireToken("rf13")
			path := testutil.WirePath(testutil.WireKindRender, token, testutil.WireModeCanon, wireEncodedRest)
			testEnv.TestServer.SetWireCanonical(token, path)

			resp := requestWire(testEnv.RequestRenderEncoded, path)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Headers.Get("EC-Source")).To(Equal(wireSourceRender))
		})

		It("RF-14 Fastly form: an encoded %2C path arrives byte-identical (RF-01)", func() {
			token := wireToken("rf14a")
			path := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, wireEncodedRest)

			resp := requestWire(testEnv.RequestRenderFastly, path)

			Expect(testEnv.TestServer.WireRequests(token)).To(Equal([]string{path}))
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("RF-14 Fastly form: the canonical %2C spelling is served 200 twice (RF-09)", func() {
			token := wireToken("rf14b")
			path := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeCanon, wireEncodedRest)
			testEnv.TestServer.SetWireCanonical(token, path)

			first := requestWire(testEnv.RequestRenderFastly, path)
			Expect(first.StatusCode).To(Equal(http.StatusOK))

			second := requestWire(testEnv.RequestRenderFastly, path)
			Expect(second.StatusCode).To(Equal(http.StatusOK))
		})

		It("RF-15 render: a subresource redirect to the page is not taken for the document's", func() {
			token := wireToken("rf15")
			path := testutil.WirePath(testutil.WireKindRender, token, testutil.WireModeFetchSelf, wireEncodedRest)
			commaPath := testutil.WirePath(testutil.WireKindRender, token, testutil.WireModeFetchSelf, wireCommaRest)
			testEnv.TestServer.SetWireCanonical(token, path)

			resp := requestWire(testEnv.RequestRenderEncoded, path)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(testEnv.TestServer.WireRequests(token)).To(ContainElement(commaPath),
				"the page script must have fetched the comma spelling")
		})

		It("RF-16 render: lock waiters do not serve a poisoned redirect the holder rejected", func() {
			token := wireToken("rf16")
			commaPath := testutil.WirePath(testutil.WireKindRender, token, testutil.WireModeStatus, wireCommaRest)
			encodedPath := testutil.WirePath(testutil.WireKindRender, token, testutil.WireModeStatus, wireEncodedRest)
			testEnv.TestServer.SetWireResponse(token, http.StatusOK, "")
			testEnv.TestServer.SetWireDelay(token, wireOriginDelay)

			cacheKey := wireCacheKey(encodedPath)
			Expect(testEnv.SeedCacheEntry(cacheKey, seededRedirect(cacheKey, wireURL(commaPath), wireURL(encodedPath),
				cache.SourceRender, time.Now().UTC().Add(time.Hour)), nil)).To(Succeed())

			statuses := make([]int, wireConcurrentRequests)
			var wg sync.WaitGroup
			for i := 0; i < wireConcurrentRequests; i++ {
				wg.Add(1)
				go func(i int) {
					defer GinkgoRecover()
					defer wg.Done()
					statuses[i] = testEnv.RequestRenderEncoded(encodedPath).StatusCode
				}(i)
			}
			wg.Wait()

			Expect(statuses).To(HaveEach(http.StatusOK))
			Expect(testEnv.TestServer.WireCount(token)).To(Equal(1))
		})

		It("RF-17 bypass-stale: a stale poisoned redirect is not served when the origin fails", func() {
			token := wireToken("rf17")
			commaPath := testutil.WirePath(testutil.WireKindBypassStale, token, testutil.WireModeStatus, wireCommaRest)
			encodedPath := testutil.WirePath(testutil.WireKindBypassStale, token, testutil.WireModeStatus, wireEncodedRest)
			testEnv.TestServer.SetWireResponse(token, http.StatusServiceUnavailable, "")

			cacheKey := wireCacheKey(encodedPath)
			Expect(testEnv.SeedCacheEntry(cacheKey, seededRedirect(cacheKey, wireURL(commaPath), wireURL(encodedPath),
				cache.SourceBypass, time.Now().UTC().Add(-5*time.Second)), nil)).To(Succeed())

			resp := requestWire(testEnv.RequestRenderEncoded, encodedPath)

			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})

		It("RF-18 bypass: a spelling written by a different request is treated as unknown", func() {
			token := wireToken("rf18")
			commaPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeCanon, wireCommaRest)
			encodedPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeCanon, wireEncodedRest)
			testEnv.TestServer.SetWireCanonical(token, encodedPath)

			cacheKey := wireCacheKey(encodedPath)
			entry := seededRedirect(cacheKey, wireURL(commaPath), wireURL(encodedPath),
				cache.SourceBypass, time.Now().UTC().Add(time.Hour))
			Expect(testEnv.SeedCacheEntry(cacheKey, entry, map[string]string{
				wireSeededFetchURLField:    wireURL(encodedPath),
				wireSeededFetchURLRIDField: entry.RequestID + "-overwritten",
			})).To(Succeed())

			resp := requestWire(testEnv.RequestRenderEncoded, encodedPath)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		DescribeTable("RF-19 render: %3F, %23 and %2541 arrive byte-identical through Chrome",
			func(id, kind, escape string) {
				expectEncodedArrival(id, kind, "x"+escape+"y", "x"+escape+"y")
			},
			escapeEntries("RF-19", renderKind, []string{"%3F", "%23", "%2541"}),
		)
	})

	Context("GREEN: everything else keeps today's wire and cache behaviour", func() {

		DescribeTable("GP-01 a plain ASCII path arrives identical",
			func(kind string) {
				expectEncodedArrival("gp01", kind, "plain/path_1.html", "plain/path_1.html")
			},
			Entry("GP-01 bypass", testutil.WireKindBypass),
			Entry("GP-01 render", testutil.WireKindRender),
		)

		It("GP-02 bypass: the query arrives normalized as on master", func() {
			expectEncodedArrival("gp02", testutil.WireKindBypass, "q?b=2&a=1,2&c=", "q?a=1%2C2&b=2&c")
		})

		DescribeTable("GP-03 utm_source is stripped on the wire",
			func(kind string) {
				expectEncodedArrival("gp03", kind, "p?utm_source=x&a=1", "p?a=1")
			},
			Entry("GP-03 bypass", testutil.WireKindBypass),
			Entry("GP-03 render", testutil.WireKindRender),
		)

		DescribeTable("GP-04 an escaped UTF-8 path arrives identical",
			func(kind string) {
				expectEncodedArrival("gp04", kind, "caf%C3%A9", "caf%C3%A9")
			},
			Entry("GP-04 bypass", testutil.WireKindBypass),
			Entry("GP-04 render", testutil.WireKindRender),
		)

		DescribeTable("GP-05 structural paths arrive as on master",
			func(kind, rest, expectedRest string) {
				expectEncodedArrival("gp05", kind, rest, expectedRest)
			},
			Entry("GP-05 bypass //", testutil.WireKindBypass, "a//b", "a/b"),
			Entry("GP-05 bypass /./", testutil.WireKindBypass, "a/./b", "a/b"),
			Entry("GP-05 bypass /../", testutil.WireKindBypass, "a/../b", "b"),
			Entry("GP-05 bypass %2F", testutil.WireKindBypass, "a%2Fb", "a/b"),
			Entry("GP-05 bypass %2e%2e", testutil.WireKindBypass, "a/%2e%2e/b", "b"),
			Entry("GP-05 bypass %5C", testutil.WireKindBypass, "a%5Cb", "a%5Cb"),
			Entry("GP-05 render //", testutil.WireKindRender, "a//b", "a/b"),
			Entry("GP-05 render /./", testutil.WireKindRender, "a/./b", "a/b"),
			Entry("GP-05 render /../", testutil.WireKindRender, "a/../b", "b"),
			Entry("GP-05 render %2F", testutil.WireKindRender, "a%2Fb", "a/b"),
			Entry("GP-05 render %2e%2e", testutil.WireKindRender, "a/%2e%2e/b", "b"),
			Entry("GP-05 render %5C", testutil.WireKindRender, "a%5Cb", "a/b"),
		)

		DescribeTable("GP-06 raw sub-delims in the path arrive as on master",
			func(kind, expectedRest string) {
				expectEncodedArrival("gp06", kind, "x!y'z(w)v*u", expectedRest)
			},
			Entry("GP-06 bypass", testutil.WireKindBypass, "x%21y%27z%28w%29v%2Au"),
			Entry("GP-06 render", testutil.WireKindRender, "x!y'z(w)v*u"),
		)

		DescribeTable("GP-07 escapes the transport re-quotes arrive identical",
			func(id, kind, escape string) {
				expectEncodedArrival(id, kind, "x"+escape+"y", "x"+escape+"y")
			},
			escapeEntries("GP-07", []string{testutil.WireKindBypass}, []string{"%21", "%27", "%28", "%29", "%2A", "%5B", "%5D", "%7C"}),
			// Chrome escapes a decoded | itself, so %7C is unchanged on the render path today.
			escapeEntries("GP-07", renderKind, []string{"%7C"}),
		)

		It("GP-08 %2C, %2c and a comma share one cache key", func() {
			token := wireToken("gp08")
			encodedPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, "a%2Cb")
			lowerPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, "a%2cb")
			commaPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, "a,b")

			first := requestWire(testEnv.RequestRenderEncoded, encodedPath)
			Expect(first.StatusCode).To(Equal(http.StatusOK))
			Expect(first.Headers.Get("EC-Source")).To(Equal(wireSourceBypass))

			for _, path := range []string{lowerPath, commaPath} {
				hit := requestWire(testEnv.RequestRenderEncoded, path)
				Expect(hit.StatusCode).To(Equal(http.StatusOK))
				Expect(hit.Headers.Get("EC-Source")).To(Equal(wireSourceBypassCache), "request %s", path)
			}

			const masterCacheKey = "cache:1:1:3985893300925881863"
			for _, rest := range []string{"a%2Cb", "a%2cb", "a,b"} {
				fixed := testutil.WirePath(testutil.WireKindBypass, "gp08", testutil.WireModeOK, rest)
				Expect(wireCacheKey(fixed)).To(Equal(masterCacheKey), "spelling %s", rest)
			}
		})

		DescribeTable("GP-09 a cached redirect to another path is served from cache",
			func(kind, cacheSource string) {
				token := wireToken("gp09")
				path := testutil.WirePath(kind, token, testutil.WireModeStatus, "a")
				// Another token, so a target Chrome follows is not counted as a second origin fetch.
				location := wireURL(testutil.WirePath(kind, wireToken("gp09-target"), testutil.WireModeOK, "elsewhere"))
				testEnv.TestServer.SetWireResponse(token, http.StatusMovedPermanently, location)

				first := requestWire(testEnv.RequestRenderEncoded, path)
				Expect(first.StatusCode).To(Equal(http.StatusMovedPermanently))
				Expect(first.Headers.Get("Location")).To(Equal(location))

				second := requestWire(testEnv.RequestRenderEncoded, path)
				Expect(second.StatusCode).To(Equal(http.StatusMovedPermanently))
				Expect(second.Headers.Get("EC-Source")).To(Equal(cacheSource))
				Expect(second.Headers.Get("Location")).To(Equal(location))
				Expect(testEnv.TestServer.WireCount(token)).To(Equal(1))
			},
			Entry("GP-09 bypass", testutil.WireKindBypass, wireSourceBypassCache),
			// The document's own redirect must still be captured once subresource redirects are ignored.
			Entry("GP-09 render", testutil.WireKindRender, wireSourceRenderCache),
		)

		It("GP-10 render-stale: a non-cacheable status deletes the stale entry", func() {
			token := wireToken("gp10")
			path := testutil.WirePath(testutil.WireKindRenderStale, token, testutil.WireModeStatus, "p")
			testEnv.TestServer.SetWireResponse(token, http.StatusOK, "")

			first := requestWire(testEnv.RequestRenderEncoded, path)
			Expect(first.StatusCode).To(Equal(http.StatusOK))

			cacheKey := wireCacheKey(path)
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())
			Expect(testEnv.MakeCacheStale(cacheKey, 3*time.Second)).To(Succeed())

			testEnv.TestServer.SetWireResponse(token, http.StatusGone, "")
			second := requestWire(testEnv.RequestRenderEncoded, path)
			Expect(second.StatusCode).To(Equal(http.StatusGone))
			Expect(second.Headers.Get("EC-Source")).To(Equal(wireSourceRender))
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())
		})

		It("GP-11 the event log url of a %2C request is the normalized URL", func() {
			token := wireToken("gp11")
			path := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, wireEncodedRest)

			resp := requestWire(testEnv.RequestRenderEncoded, path)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			entry := eventForResponse(resp)
			Expect(entry.URL).To(Equal(wireURL(testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, wireCommaRest))))
		})

		It("GP-12 a recache of a URL with no metadata fetches the queue URL as on master", func() {
			token := wireToken("gp12")
			commaPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, wireCommaRest)

			recache := requestWireRecache(wireURL(commaPath))
			Expect(recache.Error).NotTo(HaveOccurred())
			Expect(recache.StatusCode).To(Equal(http.StatusOK), recache.Body)

			Expect(testEnv.TestServer.WireRequests(token)).To(Equal([]string{commaPath}))
		})

		DescribeTable("GP-13 render: %20 and UTF-8 paths arrive identical",
			func(rest string) {
				expectEncodedArrival("gp13", testutil.WireKindRender, rest, rest)
			},
			Entry("GP-13 render %20", "a%20b"),
			Entry("GP-13 render UTF-8", "caf%C3%A9"),
		)

		DescribeTable("GP-14 escapes of unreserved characters arrive decoded",
			func(kind, rest, expectedRest string) {
				expectEncodedArrival("gp14", kind, rest, expectedRest)
			},
			Entry("GP-14 bypass %7E", testutil.WireKindBypass, "x%7Ey", "x~y"),
			Entry("GP-14 bypass %2D", testutil.WireKindBypass, "x%2Dy", "x-y"),
			Entry("GP-14 bypass %5F", testutil.WireKindBypass, "x%5Fy", "x_y"),
			Entry("GP-14 bypass %41", testutil.WireKindBypass, "x%41y", "xAy"),
			Entry("GP-14 render %7E", testutil.WireKindRender, "x%7Ey", "x~y"),
			Entry("GP-14 render %2D", testutil.WireKindRender, "x%2Dy", "x-y"),
			Entry("GP-14 render %5F", testutil.WireKindRender, "x%5Fy", "x_y"),
			Entry("GP-14 render %41", testutil.WireKindRender, "x%41y", "xAy"),
		)

		DescribeTable("GP-15 bypass: a genuine self-redirect is cached and served from cache",
			func(rest string) {
				token := wireToken("gp15")
				path := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeStatus, rest)
				testEnv.TestServer.SetWireResponse(token, http.StatusMovedPermanently, wireURL(path))

				first := requestWire(testEnv.RequestRenderEncoded, path)
				Expect(first.StatusCode).To(Equal(http.StatusMovedPermanently))

				second := requestWire(testEnv.RequestRenderEncoded, path)
				Expect(second.StatusCode).To(Equal(http.StatusMovedPermanently))
				Expect(second.Headers.Get("EC-Source")).To(Equal(wireSourceBypassCache))
				Expect(testEnv.TestServer.WireCount(token)).To(Equal(1))
			},
			Entry("GP-15 encoded path", wireEncodedRest),
			// The spelling equals the entry URL, so the entry stores no fetch_url.
			Entry("GP-15 plain path", wireCommaRest),
		)

		It("GP-16 bypass: a legacy-spelling request is served a cached same-key redirect", func() {
			token := wireToken("gp16")
			path := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeStatus, "a//b%2Cc")
			normalizedPath := testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeStatus, "a/b,c")
			location := wireURL(testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeStatus, "a/b%2Cc"))

			cacheKey := wireCacheKey(path)
			Expect(testEnv.SeedCacheEntry(cacheKey, seededRedirect(cacheKey, wireURL(normalizedPath), location,
				cache.SourceBypass, time.Now().UTC().Add(time.Hour)), nil)).To(Succeed())

			resp := requestWire(testEnv.RequestRenderEncoded, path)

			Expect(resp.StatusCode).To(Equal(http.StatusMovedPermanently))
			Expect(resp.Headers.Get("EC-Source")).To(Equal(wireSourceBypassCache))
			Expect(testEnv.TestServer.WireCount(token)).To(Equal(0))
		})

		DescribeTable("GP-17 unencoded ingress keeps today's wire",
			func(rest, expectedRest string) {
				token := wireToken("gp17")
				requestWire(testEnv.RequestRenderUnencoded, testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, rest))
				Expect(testEnv.TestServer.WireRequests(token)).To(Equal(
					[]string{testutil.WirePath(testutil.WireKindBypass, token, testutil.WireModeOK, expectedRest)}))
			},
			Entry("GP-17 bypass %2C", "a%2Cb", "a,b"),
			Entry("GP-17 bypass +", "a+b", "a%20b"),
			Entry("GP-17 bypass %20", "a%20b", "a%20b"),
			Entry("GP-17 bypass query", "q?x=1&y=2", "q?x=1"),
		)
	})
})
