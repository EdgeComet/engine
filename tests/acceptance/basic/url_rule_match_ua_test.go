package acceptance_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/common/hash"
	ecredis "github.com/edgecomet/engine/internal/common/redis"
	"github.com/edgecomet/engine/pkg/types"
	"github.com/edgecomet/engine/tests/acceptance/basic/testutil"
)

// Hosts 4 and 5 in fixtures/configs-local/hosts.d carry the rules these specs exercise.
const (
	matchUAHostID         = 4
	matchUADomain         = "matchua.localhost"
	matchUARenderKey      = "sk_test_matchua_44444"
	matchUABlockDomain    = "matchua-block.localhost"
	matchUABlockRenderKey = "sk_test_matchua_block_55555"

	matchUADesktopDimensionID = 1
	matchUAMobileDimensionID  = 2
	matchUADesktopDimension   = "desktop"

	matchUAClaudeBotUA = "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0; +claudebot@anthropic.com)"
	matchUAGooglebotUA = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	matchUAMetaUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36 (compatible; meta-externalagent/1.1 (+https://developers.facebook.com/docs/sharing/webmasters/crawler))"
	matchUAStorebotUA  = "Mozilla/5.0 (X11; Linux x86_64; Storebot-Google/1.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/79.0.3945.88 Safari/537.36"
	matchUAAmazonbotUA = "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Amazonbot/0.1; +https://developer.amazon.com/support/amazonbot) Chrome/119.0.6045.214 Safari/537.36"
	// Matches no dimension on either match_ua host.
	matchUAProbeUA   = "MatchUAProbe/1.0"
	matchUASemrushUA = "SemrushBot/7.0"

	matchUARuleIDSuffix        = "+ua"
	matchUAQueryRuleIDSuffix   = "?..." + matchUARuleIDSuffix
	matchUASiteWideRuleIDTail  = ":*" + matchUARuleIDSuffix
	matchUARetryAfterValue     = "3600"
	matchUAOriginUnreachable   = "Origin unreachable"
	matchUAUnmatchedReason     = "unmatched_user_agent"
	matchUALastBotHitField     = "last_bot_hit"
	matchUACreatedAtField      = "created_at"
	matchUASourceField         = "source"
	matchUACacheSourceRender   = "render"
	matchUACacheSourceBypass   = "bypass"
	matchUACacheBustParam      = "cb"
	matchUACreatedAtResolution = 1100 * time.Millisecond
	matchUAInternalTimeout     = 60 * time.Second
	matchUARedisTimeout        = 5 * time.Second
	matchUABypassMetric        = "eg_bypass_total"
	matchUAControlPath         = "/match-ua/bypass-cached/page"
)

var _ = Describe("URL rule match_ua", Serial, func() {

	Context("Site-wide status rule", func() {

		It("should block ClaudeBot on / and on a deep path and mark the rule ID with +ua", func() {
			By("Requesting / as ClaudeBot")
			root := requestRenderWithUserAgent(matchUAURL("/"), matchUAClaudeBotUA, matchUARenderKey)
			Expect(root.Error).To(BeNil())
			Expect(root.StatusCode).To(Equal(http.StatusForbidden),
				"The match_ua rule on * sorts above the exact / rule, so it covers /")
			Expect(root.Headers.Get(types.HeaderSource)).To(Equal(types.SourceStatus))
			Expect(root.Headers.Get(types.HeaderMatchedRule)).To(HaveSuffix(matchUASiteWideRuleIDTail))

			By("Requesting a deep path as ClaudeBot")
			deep := requestRenderWithUserAgent(matchUAURL("/match-ua/a/b/c/deep.html"), matchUAClaudeBotUA, matchUARenderKey)
			Expect(deep.Error).To(BeNil())
			Expect(deep.StatusCode).To(Equal(http.StatusForbidden))
			Expect(deep.Headers.Get(types.HeaderMatchedRule)).To(HaveSuffix(matchUASiteWideRuleIDTail))

			By("Requesting / as Googlebot")
			googleRoot := requestRenderWithUserAgent(matchUAURL(matchUAUniquePath("/")), matchUAGooglebotUA, matchUARenderKey)
			Expect(googleRoot.Error).To(BeNil())
			Expect(googleRoot.StatusCode).To(Equal(http.StatusOK))
			Expect(googleRoot.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypass), "The exact / rule applies")
			Expect(googleRoot.Headers.Get(types.HeaderMatchedRule)).To(HaveSuffix(":/"))

			By("Requesting the deep path as Googlebot")
			googleDeep := requestRenderWithUserAgent(matchUAURL(matchUAUniquePath("/match-ua/a/b/c/deep.html")), matchUAGooglebotUA, matchUARenderKey)
			Expect(googleDeep.Error).To(BeNil())
			Expect(googleDeep.StatusCode).To(Equal(http.StatusOK))
			Expect(googleDeep.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRender))
			Expect(googleDeep.Body).To(ContainSubstring(testutil.MatchUARenderedMarker))
		})

		It("should decide on the last User-Agent header when a request carries two", func() {
			By("Sending Googlebot first and ClaudeBot last")
			blocked := matchUARequestWithTwoUserAgents(matchUAURL("/match-ua/two-headers.html"), matchUAGooglebotUA, matchUAClaudeBotUA)
			Expect(blocked.Error).To(BeNil())
			Expect(blocked.StatusCode).To(Equal(http.StatusForbidden))

			By("Sending ClaudeBot first and Googlebot last")
			served := matchUARequestWithTwoUserAgents(matchUAURL(matchUAUniquePath("/")), matchUAClaudeBotUA, matchUAGooglebotUA)
			Expect(served.Error).To(BeNil())
			Expect(served.StatusCode).To(Equal(http.StatusOK))
			Expect(served.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypass))
		})
	})

	Context("Status rule scoped by match_query", func() {

		It("should answer 429 with Retry-After to Meta on a filter URL only", func() {
			By("Requesting a filter URL as Meta")
			filtered := requestRenderWithUserAgent(matchUAURL("/match-ua/list.html?inches=55"), matchUAMetaUA, matchUARenderKey)
			Expect(filtered.Error).To(BeNil())
			Expect(filtered.StatusCode).To(Equal(http.StatusTooManyRequests))
			Expect(filtered.Headers.Get("Retry-After")).To(Equal(matchUARetryAfterValue))
			Expect(filtered.Headers.Get(types.HeaderMatchedRule)).To(HaveSuffix(matchUAQueryRuleIDSuffix))

			By("Requesting a plain URL as Meta")
			plain := requestRenderWithUserAgent(matchUAURL(matchUAUniquePath("/match-ua/list.html")), matchUAMetaUA, matchUARenderKey)
			Expect(plain.Error).To(BeNil())
			Expect(plain.StatusCode).To(Equal(http.StatusOK))
			Expect(plain.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRender))

			By("Requesting the filter URL as Googlebot")
			google := requestRenderWithUserAgent(matchUAURL(matchUAUniquePath("/match-ua/list.html?inches=55")), matchUAGooglebotUA, matchUARenderKey)
			Expect(google.Error).To(BeNil())
			Expect(google.StatusCode).To(Equal(http.StatusOK))
			Expect(google.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRender))
		})
	})

	Context("Cache-only rule", func() {

		It("should serve a fresh render that another bot cached, without scheduling a recache", func() {
			targetURL := matchUAURL(matchUAUniquePath("/match-ua/cache-only/page"))

			By("Rendering the page as Googlebot")
			google := requestRenderWithUserAgent(targetURL, matchUAGooglebotUA, matchUARenderKey)
			Expect(google.Error).To(BeNil())
			Expect(google.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRender))

			By("Requesting the cached page as ClaudeBot")
			claude := requestRenderWithUserAgent(targetURL, matchUAClaudeBotUA, matchUARenderKey)
			Expect(claude.Error).To(BeNil())
			Expect(claude.StatusCode).To(Equal(http.StatusOK))
			Expect(claude.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRenderCache))
			Expect(claude.Body).To(ContainSubstring(testutil.MatchUARenderedMarker))

			By("Verifying the rule's bothit_recache switch kept the hit from scheduling a render")
			Expect(matchUAAutorecacheScheduled(targetURL)).To(BeFalse())
			meta, err := testEnv.GetCacheMetadata(matchUACacheKey(targetURL, matchUADesktopDimensionID))
			Expect(err).NotTo(HaveOccurred())
			Expect(meta).NotTo(HaveKey(matchUALastBotHitField))
		})

		It("should give origin HTML on a miss and store nothing", func() {
			targetURL := matchUAURL(matchUAUniquePath("/match-ua/cache-only/miss"))
			cacheKey := matchUACacheKey(targetURL, matchUADesktopDimensionID)

			By("Requesting an uncached page as ClaudeBot")
			claude := requestRenderWithUserAgent(targetURL, matchUAClaudeBotUA, matchUARenderKey)
			Expect(claude.Error).To(BeNil())
			Expect(claude.StatusCode).To(Equal(http.StatusOK))
			Expect(claude.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypass))
			Expect(claude.Body).To(ContainSubstring(testutil.MatchUAOriginVersionMarker))
			Expect(claude.Body).NotTo(ContainSubstring(testutil.MatchUARenderedMarker), "Origin HTML, not a render")

			By("Verifying neither a metadata key nor a cache file was written")
			Expect(testEnv.CacheExists(cacheKey)).To(BeFalse())
			Expect(matchUACacheFiles(targetURL)).To(BeEmpty())

			By("Requesting a page under an otherwise identical rule without the cache switch as ClaudeBot")
			Expect(matchUAControlStores(matchUAClaudeBotUA)).To(Equal(matchUACacheSourceBypass),
				"The host's bypass cache stores for a ClaudeBot rule that does not switch it off")

			By("Requesting the same page as Googlebot")
			google := requestRenderWithUserAgent(targetURL, matchUAGooglebotUA, matchUARenderKey)
			Expect(google.Error).To(BeNil())
			Expect(google.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRender), "Nothing was cached for Googlebot to hit")
			Expect(google.Body).To(ContainSubstring(testutil.MatchUARenderedMarker))
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())
		})

		It("should schedule a recache on a cache hit when the rule leaves the host's bothit_recache on", func() {
			targetURL := matchUAURL(matchUAUniquePath("/match-ua/cache-only-recache/page"))

			By("Rendering the page as Googlebot")
			google := requestRenderWithUserAgent(targetURL, matchUAGooglebotUA, matchUARenderKey)
			Expect(google.Error).To(BeNil())
			Expect(google.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRender))

			By("Requesting the cached page as ClaudeBot")
			claude := requestRenderWithUserAgent(targetURL, matchUAClaudeBotUA, matchUARenderKey)
			Expect(claude.Error).To(BeNil())
			Expect(claude.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRenderCache))

			By("Verifying the hit scheduled a background recache")
			Expect(matchUAAutorecacheScheduled(targetURL)).To(BeTrue())
			meta, err := testEnv.GetCacheMetadata(matchUACacheKey(targetURL, matchUADesktopDimensionID))
			Expect(err).NotTo(HaveOccurred())
			Expect(meta).To(HaveKey(matchUALastBotHitField))
		})
	})

	Context("Cache-only rule on the unmatched-dimension path", func() {

		It("should store nothing and bypass with reason unmatched_user_agent", func() {
			targetURL := matchUAURL(matchUAUniquePath("/match-ua/unmatched/page"))
			before, _ := matchUABypassReasonCount(matchUAUnmatchedReason)

			By("Requesting the page with a UA that matches no dimension")
			resp := requestRenderWithUserAgent(targetURL, matchUAProbeUA, matchUARenderKey)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypass))
			Expect(resp.Body).NotTo(ContainSubstring(testutil.MatchUARenderedMarker))

			By("Verifying nothing was stored in the unmatched dimension's slot")
			Expect(testEnv.CacheExists(matchUACacheKey(targetURL, matchUADesktopDimensionID))).To(BeFalse())
			Expect(matchUACacheFiles(targetURL)).To(BeEmpty())

			By("Verifying the bypass was recorded with the unmatched reason")
			after, found := matchUABypassReasonCount(matchUAUnmatchedReason)
			Expect(found).To(BeTrue(), "no %s series with host=%q reason=%q on %s",
				matchUABypassMetric, matchUADomain, matchUAUnmatchedReason, testutil.EGMetricsPath)
			Expect(after - before).To(Equal(1.0))

			By("Requesting a page under an otherwise identical rule without the cache switch with the same UA")
			Expect(matchUAControlStores(matchUAProbeUA)).To(Equal(matchUACacheSourceBypass),
				"The host's bypass cache stores on the unmatched-dimension path for a rule that does not switch it off")
		})
	})

	Context("Cache-only rule next to a bypass-cached path", func() {

		AfterEach(func() {
			testEnv.TestServer.SetMatchUAOriginVersion(testutil.MatchUADefaultOriginVersion)
		})

		It("should give Storebot the current origin body and leave the cached entry to other bots", func() {
			targetURL := matchUAURL(matchUAUniquePath("/match-ua/bypass-only/price"))
			cacheKey := matchUACacheKey(targetURL, matchUADesktopDimensionID)
			const oldVersion, newVersion = "price-v1", "price-v2"

			By("Filling the bypass cache as Googlebot")
			testEnv.TestServer.SetMatchUAOriginVersion(oldVersion)
			first := requestRenderWithUserAgent(targetURL, matchUAGooglebotUA, matchUARenderKey)
			Expect(first.Error).To(BeNil())
			Expect(first.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypass))
			Expect(first.Body).To(ContainSubstring(testutil.MatchUAOriginVersionMarker + oldVersion))
			Expect(testEnv.GetCacheSource(cacheKey)).To(Equal(matchUACacheSourceBypass))
			createdAt := matchUAMetaField(cacheKey, matchUACreatedAtField)
			Expect(createdAt).NotTo(BeEmpty())

			By("Changing the origin body")
			testEnv.TestServer.SetMatchUAOriginVersion(newVersion)
			// created_at has one-second resolution: a rewrite after this would carry a later value
			time.Sleep(matchUACreatedAtResolution)

			By("Requesting the page as Storebot")
			storebot := requestRenderWithUserAgent(targetURL, matchUAStorebotUA, matchUARenderKey)
			Expect(storebot.Error).To(BeNil())
			Expect(storebot.StatusCode).To(Equal(http.StatusOK))
			Expect(storebot.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypass))
			Expect(storebot.Body).To(ContainSubstring(testutil.MatchUAOriginVersionMarker + newVersion))

			By("Verifying the bypass entry was not rewritten")
			Expect(matchUAMetaField(cacheKey, matchUACreatedAtField)).To(Equal(createdAt))

			By("Requesting the page as Googlebot again")
			second := requestRenderWithUserAgent(targetURL, matchUAGooglebotUA, matchUARenderKey)
			Expect(second.Error).To(BeNil())
			Expect(second.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypassCache))
			Expect(second.Body).To(ContainSubstring(testutil.MatchUAOriginVersionMarker + oldVersion))
		})

		It("should give Storebot the synthetic 502 when the origin is down, not the stale entry", func() {
			const downVersion = "down-v1"
			origin := httptest.NewServer(testutil.MatchUAPageHandler(downVersion))
			DeferCleanup(origin.Close)

			originPort := origin.Listener.Addr().(*net.TCPAddr).Port
			targetURL := fmt.Sprintf("http://%s:%d%s", matchUADomain, originPort, matchUAUniquePath("/match-ua/bypass-only/down"))
			cacheKey := matchUACacheKey(targetURL, matchUADesktopDimensionID)

			By("Filling the bypass cache as Googlebot")
			first := requestRenderWithUserAgent(targetURL, matchUAGooglebotUA, matchUARenderKey)
			Expect(first.Error).To(BeNil())
			Expect(first.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypass))
			Expect(testEnv.CacheExists(cacheKey)).To(BeTrue())

			By("Making the entry stale and taking the origin down")
			Expect(testEnv.ExpireCache(cacheKey)).To(Succeed())
			origin.Close()

			By("Requesting the page as Storebot")
			storebot := requestRenderWithUserAgent(targetURL, matchUAStorebotUA, matchUARenderKey)
			Expect(storebot.Error).To(BeNil())
			Expect(storebot.StatusCode).To(Equal(http.StatusBadGateway))
			Expect(storebot.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypass))
			Expect(storebot.Body).To(ContainSubstring(matchUAOriginUnreachable))

			By("Verifying the stale entry is still there for a bot without the rule")
			google := requestRenderWithUserAgent(targetURL, matchUAGooglebotUA, matchUARenderKey)
			Expect(google.Error).To(BeNil())
			Expect(google.StatusCode).To(Equal(http.StatusOK))
			Expect(google.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypassStale))
			Expect(google.Body).To(ContainSubstring(testutil.MatchUAOriginVersionMarker + downVersion))
		})
	})

	Context("Site-wide match_ua rule shadowing a path rule", func() {

		It("should give the bot origin HTML where the path rule answers 404 for everyone else", func() {
			targetURL := matchUAURL("/match-ua/admin/x.html")

			By("Requesting the admin page as Amazonbot")
			amazon := requestRenderWithUserAgent(targetURL, matchUAAmazonbotUA, matchUARenderKey)
			Expect(amazon.Error).To(BeNil())
			Expect(amazon.StatusCode).To(Equal(http.StatusOK))
			Expect(amazon.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypass))
			Expect(amazon.Body).To(ContainSubstring(testutil.MatchUAOriginVersionMarker))

			By("Requesting the admin page as Googlebot")
			google := requestRenderWithUserAgent(targetURL, matchUAGooglebotUA, matchUARenderKey)
			Expect(google.Error).To(BeNil())
			Expect(google.StatusCode).To(Equal(http.StatusNotFound))
			Expect(google.Headers.Get(types.HeaderSource)).To(Equal(types.SourceStatus))
		})
	})

	Context("Render dimension override", func() {

		It("should store the bot's render in the overridden dimension's slot", func() {
			targetURL := matchUAURL(matchUAUniquePath("/match-ua/products/item"))
			mobileKey := matchUACacheKey(targetURL, matchUAMobileDimensionID)
			desktopKey := matchUACacheKey(targetURL, matchUADesktopDimensionID)

			By("Rendering the page as ClaudeBot")
			claude := requestRenderWithUserAgent(targetURL, matchUAClaudeBotUA, matchUARenderKey)
			Expect(claude.Error).To(BeNil())
			Expect(claude.StatusCode).To(Equal(http.StatusOK))
			Expect(claude.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRender))

			By("Verifying the render landed in the mobile slot only")
			Expect(testEnv.CacheExists(mobileKey)).To(BeTrue())
			Expect(testEnv.CacheExists(desktopKey)).To(BeFalse())

			By("Requesting the same URL as desktop Googlebot")
			google := requestRenderWithUserAgent(targetURL, matchUAGooglebotUA, matchUARenderKey)
			Expect(google.Error).To(BeNil())
			Expect(google.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRender), "The desktop slot was empty")
			Expect(testEnv.CacheExists(desktopKey)).To(BeTrue())
		})
	})

	Context("Callers without a client", func() {

		It("should precache a URL that a site-wide match_ua status rule covers", func() {
			targetURL := matchUAURL(matchUAUniquePath("/match-ua/precache/page"))
			cacheKey := matchUACacheKey(targetURL, matchUADesktopDimensionID)

			By("Asking the Edge Gateway to precache the page")
			resp := matchUAPrecache(targetURL)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusOK), "Recache should succeed: "+resp.Body)

			By("Verifying a render record was stored")
			Expect(testEnv.GetCacheSource(cacheKey)).To(Equal(matchUACacheSourceRender))

			By("Verifying ClaudeBot is still blocked on the precached URL")
			claude := requestRenderWithUserAgent(targetURL, matchUAClaudeBotUA, matchUARenderKey)
			Expect(claude.Error).To(BeNil())
			Expect(claude.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("should render a URL that a site-wide match_ua status rule covers through HAR debug", func() {
			targetURL := matchUAURL(matchUAUniquePath("/match-ua/har/page"))

			By("Requesting a HAR render with a ClaudeBot User-Agent")
			resp := matchUAHARRender(targetURL)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusOK), "HAR render should succeed: "+resp.Body)

			var har map[string]interface{}
			Expect(json.Unmarshal([]byte(resp.Body), &har)).To(Succeed())
			Expect(har).To(HaveKey("log"))

			By("Verifying ClaudeBot is blocked on the same URL")
			claude := requestRenderWithUserAgent(targetURL, matchUAClaudeBotUA, matchUARenderKey)
			Expect(claude.Error).To(BeNil())
			Expect(claude.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Context("Dimension block and unmatched block run before URL rules", func() {

		It("should block a UA in a block dimension that a match_ua render rule names", func() {
			resp := requestRenderWithUserAgent(matchUABlockURL("/match-ua/page"), matchUASemrushUA, matchUABlockRenderKey)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(resp.Headers.Get(types.HeaderMatchedRule)).To(BeEmpty())
		})

		It("should block an unmatched UA that a match_ua render rule names", func() {
			resp := requestRenderWithUserAgent(matchUABlockURL("/match-ua/page"), matchUAProbeUA, matchUABlockRenderKey)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(resp.Headers.Get(types.HeaderMatchedRule)).To(BeEmpty())
		})

		It("should apply the same match_ua render rule to a UA neither check blocks", func() {
			resp := requestRenderWithUserAgent(matchUABlockURL(matchUAUniquePath("/match-ua/page")), matchUAGooglebotUA, matchUABlockRenderKey)
			Expect(resp.Error).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Headers.Get(types.HeaderSource)).To(Equal(types.SourceRender))
			Expect(resp.Headers.Get(types.HeaderMatchedRule)).To(Equal("rule_0" + matchUASiteWideRuleIDTail))
		})
	})

	Context("Config test tool", func() {

		It("should print the match_ua rule with -ua and the path rule without it", func() {
			configPath := filepath.Join(testEnv.TempConfigDir, "edge-gateway.yaml")
			targetURL := matchUAURL("/match-ua/admin/x.html")

			By("Testing the URL with a ClaudeBot User-Agent given after the URL")
			withUA, err := createConfigTestCommand("-c", configPath, "-t", targetURL, "-ua", matchUAClaudeBotUA).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(withUA))
			Expect(string(withUA)).To(ContainSubstring("User-Agent: " + matchUAClaudeBotUA))
			Expect(string(withUA)).To(ContainSubstring("Matched Pattern: *\n"))
			Expect(string(withUA)).To(ContainSubstring(matchUASiteWideRuleIDTail))
			Expect(string(withUA)).To(ContainSubstring("Action: status_403"))

			By("Testing the URL without a User-Agent")
			withoutUA, err := createConfigTestCommand("-c", configPath, "-t", targetURL).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(withoutUA))
			Expect(string(withoutUA)).To(ContainSubstring("Matched Pattern: /match-ua/admin/*"))
			Expect(string(withoutUA)).To(ContainSubstring("Action: status_404"))
			Expect(string(withoutUA)).NotTo(ContainSubstring(matchUARuleIDSuffix))
			Expect(string(withoutUA)).NotTo(ContainSubstring("User-Agent: "))

			By("Testing the URL with a stray argument between the URL and -ua")
			stray, err := createConfigTestCommand("-c", configPath, "-t", targetURL, "stray", "-ua", matchUAClaudeBotUA).CombinedOutput()
			Expect(err).To(HaveOccurred(), string(stray))
			Expect(string(stray)).To(ContainSubstring(`unexpected argument "stray"`))
			Expect(string(stray)).NotTo(ContainSubstring("Matched Pattern:"))
		})
	})

	Context("Validation", func() {

		DescribeTable("should refuse to load a host whose match_ua rule is invalid",
			func(ruleYAML, wantErr string) {
				err := matchUALoadHostWithRule(ruleYAML)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(wantErr))
			},
			Entry("wildcard-only pattern", `
      - match: "*"
        match_ua: ["*"]
        action: "status_403"`, "match_ua[0] '*' matches every client; omit match_ua instead"),
			Entry("empty list", `
      - match: "*"
        match_ua: []
        action: "status_403"`, "match_ua must list at least one pattern; omit it to match every client"),
			Entry("tracking_params on the rule", `
      - match: "*"
        match_ua: ["$AnthropicBot"]
        action: "bypass"
        tracking_params:
          params_add: ["sessionid"]`, "tracking_params is not allowed on a rule with match_ua; set it at host level"),
		)
	})
})

func matchUAURL(path string) string {
	return fmt.Sprintf("http://%s:%d%s", matchUADomain, testEnv.Config.TestServer.Port, path)
}

func matchUABlockURL(path string) string {
	return fmt.Sprintf("http://%s:%d%s", matchUABlockDomain, testEnv.Config.TestServer.Port, path)
}

// matchUAUniquePath appends a cache-busting parameter: cache entries and recache queue members
// outlive a spec, and cache files outlive the suite.
func matchUAUniquePath(path string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + matchUACacheBustParam + "=" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func matchUANormalize(targetURL string) (string, uint64) {
	normalizer := hash.NewURLNormalizer()
	result, err := normalizer.Normalize(targetURL, nil)
	Expect(err).NotTo(HaveOccurred())
	return result.NormalizedURL, normalizer.Hash(result.NormalizedURL)
}

func matchUACacheKey(targetURL string, dimensionID int) string {
	_, urlHash := matchUANormalize(targetURL)
	return ecredis.NewKeyGenerator().GenerateCacheKey(matchUAHostID, dimensionID, urlHash).String()
}

// matchUACacheFiles lists the host's cache files for the URL in any dimension. Files are named
// <url_hash>_<dimension_id>.html with an optional compression suffix.
func matchUACacheFiles(targetURL string) []string {
	_, urlHash := matchUANormalize(targetURL)
	prefix := strconv.FormatUint(urlHash, 10) + "_"

	basePath, err := filepath.Abs(testEnv.Config.EdgeGateway.Storage.BasePath)
	Expect(err).NotTo(HaveOccurred())
	hostDir := filepath.Join(basePath, strconv.Itoa(matchUAHostID))

	var found []string
	err = filepath.WalkDir(hostDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return filepath.SkipDir
			}
			return walkErr
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			found = append(found, path)
		}
		return nil
	})
	Expect(err).NotTo(HaveOccurred())
	return found
}

func matchUAMetaField(cacheKey, field string) string {
	meta, err := testEnv.GetCacheMetadata(cacheKey)
	Expect(err).NotTo(HaveOccurred())
	return meta[field]
}

func matchUAAutorecacheScheduled(targetURL string) bool {
	normalizedURL, _ := matchUANormalize(targetURL)
	queueKey := ecredis.NewKeyGenerator().RecacheQueueKey(matchUAHostID, ecredis.PriorityAutorecache)

	ctx, cancel := context.WithTimeout(context.Background(), matchUARedisTimeout)
	defer cancel()
	members, err := testEnv.RedisClient.ZRange(ctx, queueKey, 0, -1).Result()
	Expect(err).NotTo(HaveOccurred())

	for _, raw := range members {
		var member types.RecacheMember
		Expect(json.Unmarshal([]byte(raw), &member)).To(Succeed())
		if member.URL == normalizedURL {
			return true
		}
	}
	return false
}

// matchUAControlStores requests a fresh URL under the switch-less control rule and returns the
// source of the cache record the request left in the desktop slot, "" when none.
func matchUAControlStores(userAgent string) string {
	controlURL := matchUAURL(matchUAUniquePath(matchUAControlPath))
	resp := requestRenderWithUserAgent(controlURL, userAgent, matchUARenderKey)
	Expect(resp.Error).To(BeNil())
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	Expect(resp.Headers.Get(types.HeaderSource)).To(Equal(types.SourceBypass))

	return matchUAMetaField(matchUACacheKey(controlURL, matchUADesktopDimensionID), matchUASourceField)
}

// matchUABypassReasonCount sums the Edge Gateway's bypass counter over every series for host 4
// and one reason, whatever other labels or label order the series has. found is false when no
// such series is exposed.
func matchUABypassReasonCount(reason string) (total float64, found bool) {
	resp, err := testEnv.HTTPClient.Get("http://localhost" + testutil.EGMetricsListen + testutil.EGMetricsPath)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())

	for _, line := range strings.Split(string(body), "\n") {
		name, labels, value, ok := matchUAParseSample(line)
		if !ok || name != matchUABypassMetric || labels["host"] != matchUADomain || labels["reason"] != reason {
			continue
		}
		total += value
		found = true
	}
	return total, found
}

// matchUAParseSample parses one Prometheus text exposition sample: name{k="v",...} value.
func matchUAParseSample(line string) (string, map[string]string, float64, bool) {
	if strings.HasPrefix(line, "#") {
		return "", nil, 0, false
	}
	open := strings.IndexByte(line, '{')
	end := strings.LastIndexByte(line, '}')
	if open <= 0 || end < open {
		return "", nil, 0, false
	}

	labels := make(map[string]string)
	rest := line[open+1 : end]
	for rest != "" {
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			return "", nil, 0, false
		}
		quoted, err := strconv.QuotedPrefix(rest[eq+1:])
		if err != nil {
			return "", nil, 0, false
		}
		value, err := strconv.Unquote(quoted)
		if err != nil {
			return "", nil, 0, false
		}
		labels[rest[:eq]] = value
		rest = strings.TrimPrefix(rest[eq+1+len(quoted):], ",")
	}

	fields := strings.Fields(line[end+1:])
	if len(fields) == 0 {
		return "", nil, 0, false
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", nil, 0, false
	}
	return line[:open], labels, value, true
}

// matchUARequestWithTwoUserAgents writes the request by hand: net/http sends only the first
// User-Agent value of a request.
func matchUARequestWithTwoUserAgents(targetURL, firstUA, secondUA string) *TestResponse {
	egURL, err := url.Parse(testEnv.Config.EGBaseURL())
	if err != nil {
		return &TestResponse{Error: err}
	}

	conn, err := net.DialTimeout("tcp", egURL.Host, testEnv.Config.HTTPClientTimeout())
	if err != nil {
		return &TestResponse{Error: err}
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(testEnv.Config.HTTPClientTimeout())); err != nil {
		return &TestResponse{Error: err}
	}

	var request bytes.Buffer
	fmt.Fprintf(&request, "GET /render?url=%s HTTP/1.1\r\n", url.QueryEscape(targetURL))
	fmt.Fprintf(&request, "Host: %s\r\n", egURL.Host)
	fmt.Fprintf(&request, "%s: %s\r\n", types.HeaderRenderKey, matchUARenderKey)
	fmt.Fprintf(&request, "User-Agent: %s\r\n", firstUA)
	fmt.Fprintf(&request, "User-Agent: %s\r\n", secondUA)
	request.WriteString("Connection: close\r\n\r\n")
	if _, err := conn.Write(request.Bytes()); err != nil {
		return &TestResponse{Error: err}
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
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

// matchUAInternalRequest sends a ClaudeBot User-Agent: internal handlers must resolve without it.
func matchUAInternalRequest(method, path string, body io.Reader) *TestResponse {
	req, err := http.NewRequest(method, testutil.EGInternalBaseURL+path, body)
	if err != nil {
		return &TestResponse{Error: err}
	}
	req.Header.Set(testutil.EGInternalAuthHeader, testutil.EGInternalAuthKey)
	req.Header.Set("User-Agent", matchUAClaudeBotUA)

	client := &http.Client{Timeout: matchUAInternalTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return &TestResponse{Error: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	return &TestResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       string(respBody),
		Error:      err,
	}
}

// matchUAPrecache asks the Edge Gateway to precache one URL, the way the cache daemon does.
func matchUAPrecache(targetURL string) *TestResponse {
	payload, err := json.Marshal(map[string]interface{}{
		"url":          targetURL,
		"host_id":      matchUAHostID,
		"dimension_id": matchUADesktopDimensionID,
	})
	if err != nil {
		return &TestResponse{Error: err}
	}
	return matchUAInternalRequest(http.MethodPost, "/internal/cache/recache", bytes.NewReader(payload))
}

func matchUAHARRender(targetURL string) *TestResponse {
	params := url.Values{}
	params.Set("url", targetURL)
	params.Set("dimension", matchUADesktopDimension)
	return matchUAInternalRequest(http.MethodGet, "/debug/har/render?"+params.Encode(), nil)
}

const matchUAValidationEGConfig = `
eg_id: "eg-match-ua-validation"
internal:
  listen: "localhost:10071"
  auth_key: "test-auth-key-12345"
server:
  listen: ":10070"
  timeout: 120s
redis:
  addr: "localhost:6379"
storage:
  base_path: "/tmp/cache"
log:
  level: "info"
  console:
    enabled: true
    format: "json"
metrics:
  enabled: false
hosts:
  include: "hosts.d/"
`

const matchUAValidationHostConfig = `
hosts:
  - id: 1
    domain: "validation.localhost"
    render_key: "validation-key"
    enabled: true
    unmatched_dimension: "bypass"
    dimensions:
      desktop:
        id: 1
        width: 1920
        height: 1080
        match_ua: ["*Googlebot*"]
    render:
      timeout: 30s
      cache:
        ttl: 1h
    url_rules:`

// matchUALoadHostWithRule loads an Edge Gateway configuration whose only host carries the rule,
// through the same path a starting gateway takes.
func matchUALoadHostWithRule(ruleYAML string) error {
	dir := GinkgoT().TempDir()
	hostsDir := filepath.Join(dir, "hosts.d")
	Expect(os.MkdirAll(hostsDir, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(hostsDir, "host.yaml"), []byte(matchUAValidationHostConfig+ruleYAML+"\n"), 0o644)).To(Succeed())

	configPath := filepath.Join(dir, "edge-gateway.yaml")
	Expect(os.WriteFile(configPath, []byte(matchUAValidationEGConfig), 0o644)).To(Succeed())

	_, err := config.NewEGConfigManager(configPath, zap.NewNop())
	return err
}
