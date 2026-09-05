package chrome

import (
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/internal/common/urlutil"
	"github.com/edgecomet/engine/pkg/types"
)

func TestRenderResponse_Structure(t *testing.T) {
	resp := &types.RenderResponse{
		RequestID:  "test-req-1",
		Success:    true,
		HTML:       "<html><body>Test</body></html>",
		RenderTime: 1500,
		HTMLSize:   30,
		Timestamp:  time.Now(),
		ChromeID:   "chrome-0",
		Metrics: types.PageMetrics{
			StatusCode: 200,
			FinalURL:   "https://example.com",
			LifecycleEvents: []types.LifecycleEvent{
				{Name: "init", Time: 0.05},
				{Name: "DOMContentLoaded", Time: 0.5},
				{Name: "load", Time: 1.0},
			},
			ConsoleMessages: []types.ConsoleError{},
		},
	}

	assert.Equal(t, "test-req-1", resp.RequestID)
	assert.True(t, resp.Success)
	assert.NotEmpty(t, resp.HTML)
	assert.Equal(t, 200, resp.Metrics.StatusCode)
	assert.Len(t, resp.Metrics.LifecycleEvents, 3)
	assert.Empty(t, resp.Metrics.ConsoleMessages)
}

func TestConsoleMessageCapture(t *testing.T) {
	t.Run("size limit on message field only", func(t *testing.T) {
		ce := types.ConsoleError{
			Type:           types.ConsoleTypeError,
			SourceURL:      "https://example.com/very-long-url-that-should-not-count.js",
			SourceLocation: "100:200",
			Message:        "short msg",
		}

		// Size should be len("short msg") = 9, not the full struct size
		assert.Equal(t, 9, len(ce.Message))
	})

	t.Run("console type constants", func(t *testing.T) {
		assert.Equal(t, "error", types.ConsoleTypeError)
		assert.Equal(t, "warning", types.ConsoleTypeWarning)
	})
}

func TestRenderKeyInjectionScope(t *testing.T) {
	// Mirrors the predicate at the fetch interception in buildTasks: a paused request carries the
	// forwarded client headers and the render key only when its host is same-origin with the render
	// target. Subdomains qualify - see the call site for why.
	const targetHost = "example.com"

	sameOrigin := func(requestURL string) bool {
		return urlutil.IsSameOrigin(targetHost, urlutil.ExtractHost(requestURL))
	}

	t.Run("exact host", func(t *testing.T) {
		assert.True(t, sameOrigin("https://example.com/api/data"))
	})

	t.Run("sibling subdomain", func(t *testing.T) {
		assert.True(t, sameOrigin("https://platform.example.com/api/v2/casino/producer"))
	})

	t.Run("parent domain of a subdomain target", func(t *testing.T) {
		assert.True(t, urlutil.IsSameOrigin("www.example.com", urlutil.ExtractHost("https://example.com/data")))
	})

	t.Run("port and scheme are ignored", func(t *testing.T) {
		assert.True(t, sameOrigin("https://example.com:8443/api/data"))
		assert.True(t, sameOrigin("http://example.com/api/data"))
	})

	t.Run("unrelated host", func(t *testing.T) {
		assert.False(t, sameOrigin("https://cdn.other.com/lib.js"))
	})

	t.Run("host that merely suffixes the target", func(t *testing.T) {
		assert.False(t, sameOrigin("https://notexample.com/data"))
	})

	t.Run("empty target host", func(t *testing.T) {
		assert.False(t, urlutil.IsSameOrigin("", urlutil.ExtractHost("https://example.com/api/data")))
	})

	t.Run("unparseable request URL", func(t *testing.T) {
		assert.False(t, sameOrigin("://not a url"))
	})
}

// lifecycleEvent builds the CDP event the recorder listens for.
func lifecycleEvent(name, frameID, loaderID string) *page.EventLifecycleEvent {
	return &page.EventLifecycleEvent{
		FrameID:  cdp.FrameID(frameID),
		LoaderID: cdp.LoaderID(loaderID),
		Name:     name,
	}
}

// newTestRecorder builds a recorder and its listener without a browser, which is the seam that
// makes the matching and signalling rules testable at all: installing the real listener needs a
// chromedp context with a live target.
func newTestRecorder(t *testing.T, metrics *types.PageMetrics, targetEvent string) (*lifecycleRecorder, func(ev interface{}), *bool) {
	t.Helper()

	stopped := false
	recorder := newLifecycleRecorder(metrics, func() { stopped = true })

	return recorder, recorder.listener(testFrameID, testLoaderID, targetEvent, time.Now().UnixMilli()), &stopped
}

const (
	testFrameID  = "frame-1"
	testLoaderID = "loader-1"
)

func TestLifecycleRecorder(t *testing.T) {
	t.Run("records every event of the navigation", func(t *testing.T) {
		var metrics types.PageMetrics
		_, listen, _ := newTestRecorder(t, &metrics, types.LifecycleEventNetworkIdle)

		for _, name := range []string{"init", types.LifecycleEventDOMContentLoaded, types.LifecycleEventLoad} {
			listen(lifecycleEvent(name, testFrameID, testLoaderID))
		}

		require.Len(t, metrics.LifecycleEvents, 3)
		assert.Equal(t, "init", metrics.LifecycleEvents[0].Name)
		assert.Equal(t, types.LifecycleEventLoad, metrics.LifecycleEvents[2].Name)
		for _, ev := range metrics.LifecycleEvents {
			assert.GreaterOrEqual(t, ev.Time, 0.0)
		}
	})

	t.Run("ignores other navigations and other event types", func(t *testing.T) {
		var metrics types.PageMetrics
		recorder, listen, _ := newTestRecorder(t, &metrics, types.LifecycleEventLoad)

		listen(lifecycleEvent(types.LifecycleEventLoad, "other-frame", testLoaderID))
		listen(lifecycleEvent(types.LifecycleEventLoad, testFrameID, "other-loader"))
		listen(&page.EventJavascriptDialogClosed{})

		assert.Empty(t, metrics.LifecycleEvents)
		assertNotSignaled(t, recorder)
	})

	t.Run("signals and stops the listener on the target event", func(t *testing.T) {
		var metrics types.PageMetrics
		recorder, listen, stopped := newTestRecorder(t, &metrics, types.LifecycleEventNetworkIdle)

		listen(lifecycleEvent(types.LifecycleEventLoad, testFrameID, testLoaderID))
		assertNotSignaled(t, recorder)

		listen(lifecycleEvent(types.LifecycleEventNetworkIdle, testFrameID, testLoaderID))

		select {
		case <-recorder.signal():
		default:
			t.Fatal("target event did not signal")
		}
		assert.True(t, *stopped, "the listener context is cancelled as soon as the target arrives")
	})

	t.Run("a repeated target event does not panic", func(t *testing.T) {
		var metrics types.PageMetrics
		recorder, listen, _ := newTestRecorder(t, &metrics, types.LifecycleEventNetworkIdle)

		listen(lifecycleEvent(types.LifecycleEventNetworkIdle, testFrameID, testLoaderID))
		listen(lifecycleEvent(types.LifecycleEventNetworkIdle, testFrameID, testLoaderID))

		assert.Len(t, metrics.LifecycleEvents, 2, "the repeat is still recorded")
		select {
		case <-recorder.signal():
		default:
			t.Fatal("target event did not signal")
		}
	})

	t.Run("an empty target records without ever signalling", func(t *testing.T) {
		var metrics types.PageMetrics
		recorder, listen, stopped := newTestRecorder(t, &metrics, "")

		for _, name := range []string{types.LifecycleEventLoad, types.LifecycleEventNetworkIdle} {
			listen(lifecycleEvent(name, testFrameID, testLoaderID))
		}

		assert.Len(t, metrics.LifecycleEvents, 2, "a render on a readiness wait keeps its lifecycle analytics")
		assertNotSignaled(t, recorder)
		assert.False(t, *stopped)
	})

	t.Run("a wait records its own entry alongside the events", func(t *testing.T) {
		var metrics types.PageMetrics
		recorder, listen, _ := newTestRecorder(t, &metrics, "")

		listen(lifecycleEvent(types.LifecycleEventLoad, testFrameID, testLoaderID))
		recorder.record(types.WaitForPrerenderContentReady, 4.4)

		require.Len(t, metrics.LifecycleEvents, 2)
		assert.Equal(t, types.LifecycleEvent{Name: types.WaitForPrerenderContentReady, Time: 4.4},
			metrics.LifecycleEvents[1])
	})
}

func assertNotSignaled(t *testing.T, recorder *lifecycleRecorder) {
	t.Helper()

	select {
	case <-recorder.signal():
		t.Fatal("wait signalled when it should not have")
	default:
	}
}

func TestConvertCDPHeaders(t *testing.T) {
	t.Run("nil input returns nil so an existing capture is left alone", func(t *testing.T) {
		assert.Nil(t, convertCDPHeaders(nil))
	})

	t.Run("single value", func(t *testing.T) {
		headers := convertCDPHeaders(network.Headers{"content-type": "text/html"})

		assert.Equal(t, []string{"text/html"}, headers["content-type"])
	})

	t.Run("newline-separated repeats are split", func(t *testing.T) {
		headers := convertCDPHeaders(network.Headers{"set-cookie": "a=1\nb=2"})

		assert.Equal(t, []string{"a=1", "b=2"}, headers["set-cookie"])
	})

	t.Run("array repeats", func(t *testing.T) {
		headers := convertCDPHeaders(network.Headers{"set-cookie": []interface{}{"a=1", "b=2"}})

		assert.Equal(t, []string{"a=1", "b=2"}, headers["set-cookie"])
	})

	t.Run("non-string values are skipped", func(t *testing.T) {
		headers := convertCDPHeaders(network.Headers{"x-count": 42})

		assert.NotContains(t, headers, "x-count")
	})

	t.Run("redirect header block", func(t *testing.T) {
		headers := convertCDPHeaders(network.Headers{
			"location":      "https://example.com/new",
			"cache-control": "no-store",
			"set-cookie":    "session=abc\npath=/",
		})

		assert.Equal(t, []string{"https://example.com/new"}, headers["location"])
		assert.Equal(t, []string{"no-store"}, headers["cache-control"])
		assert.Equal(t, []string{"session=abc", "path=/"}, headers["set-cookie"])
	})
}
