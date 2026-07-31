package events

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/config"
	"github.com/edgecomet/engine/internal/edge/bypass"
	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/pkg/types"
)

const (
	testOriginLastModified = "Wed, 05 Mar 2024 08:00:00 GMT"
	emptyDatesJSON         = `"dates":[]`
	datesKeyJSON           = `"dates"`
)

// TestBuildRequestEvent_DatesSerializeAsEmptyArray pins the distinction consumers depend
// on: an empty array says the page was inspected and carries no date signal, while an
// absent key carries no evidence at all. A nil slice would marshal as null and lose both
// readings. Both blobs here carry the initialized empty slice that content processing
// always produces.
func TestBuildRequestEvent_DatesSerializeAsEmptyArray(t *testing.T) {
	renderCtx := createTestRenderContext()
	result := &orchestrator.RenderResult{
		Source:     orchestrator.ServedFromRender,
		StatusCode: 200,
		PageSEO: &types.PageSEO{
			Title:       "No dates here",
			IndexStatus: types.IndexStatusIndexable,
			Dates:       []types.DateCandidate{},
		},
		OriginalPageSEO: &types.PageSEO{
			Title: "No dates here either",
			Dates: []types.DateCandidate{},
		},
	}

	event := BuildRequestEvent(renderCtx, result, time.Second, "eg-1")

	require.NotNil(t, event.PageSEO)
	require.NotNil(t, event.PageSEO.Dates)
	assert.Empty(t, event.PageSEO.Dates)

	encoded, err := json.Marshal(event)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), emptyDatesJSON)
	assert.NotContains(t, string(encoded), `"dates":null`)

	pageSEOJSON, err := json.Marshal(event.PageSEO)
	require.NoError(t, err)
	assert.Contains(t, string(pageSEOJSON), emptyDatesJSON)

	originalJSON, err := json.Marshal(event.PageSEOOriginal)
	require.NoError(t, err)
	assert.Contains(t, string(originalJSON), emptyDatesJSON)
}

// TestBuildRequestEvent_CacheServedEventOmitsDates pins the other half of that
// distinction. A cache-served request rebuilds page_seo from cache metadata, which holds
// a title and an index status and nothing else, so no page was inspected and the blob
// must not carry the empty array that would claim otherwise.
func TestBuildRequestEvent_CacheServedEventOmitsDates(t *testing.T) {
	tests := []struct {
		name      string
		source    orchestrator.ResponseSource
		eventType string
	}{
		{"render cache", orchestrator.ServedFromCache, EventTypeCacheHit},
		{"bypass cache", orchestrator.ServedFromBypassCache, EventTypeBypassCache},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &orchestrator.RenderResult{
				Source:     tt.source,
				StatusCode: 200,
				// The shape pageSEOFromCacheMetadata produces: no Dates slice.
				PageSEO: &types.PageSEO{
					Title:       "Cached title",
					IndexStatus: types.IndexStatusIndexable,
				},
			}

			event := BuildRequestEvent(createTestRenderContext(), result, time.Second, "eg-1")

			require.Equal(t, tt.eventType, event.EventType)
			require.NotNil(t, event.PageSEO)
			assert.Nil(t, event.PageSEO.Dates)

			encoded, err := json.Marshal(event.PageSEO)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), datesKeyJSON,
				"a page the engine never inspected must not report an inspection result")

			eventJSON, err := json.Marshal(event)
			require.NoError(t, err)
			assert.NotContains(t, string(eventJSON), datesKeyJSON)
		})
	}
}

func TestBuildRequestEvent_DatesRoundTripInOrder(t *testing.T) {
	candidates := []types.DateCandidate{
		{Source: types.DateSourceJSONLD, Field: types.DateFieldPublished, Raw: "2024-03-05T10:00:00+02:00", Context: "BlogPosting"},
		{Source: types.DateSourceJSONLD, Field: types.DateFieldExpires, Raw: "2026-08-01", Context: "JobPosting"},
		{Source: types.DateSourceMeta, Field: types.DateFieldPublished, Raw: "2024-03-05", Context: "article:published_time"},
		{Source: types.DateSourceTimeElement, Field: types.DateFieldUnknown, Raw: "2024-03-05", Context: ""},
		{Source: types.DateSourceHTTPHeader, Field: types.DateFieldModified, Raw: testOriginLastModified, Context: types.LastModifiedHeader},
	}

	renderCtx := createTestRenderContext()
	result := &orchestrator.RenderResult{
		Source:          orchestrator.ServedFromRender,
		StatusCode:      200,
		PageSEO:         &types.PageSEO{Title: "Dated page", Dates: candidates},
		OriginalPageSEO: &types.PageSEO{Title: "Dated page", Dates: candidates},
	}

	event := BuildRequestEvent(renderCtx, result, time.Second, "eg-1")

	encoded, err := json.Marshal(event.PageSEO)
	require.NoError(t, err)

	var decoded struct {
		Dates []DateCandidateEvent `json:"dates"`
	}
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	require.Len(t, decoded.Dates, len(candidates))
	for i, want := range candidates {
		assert.Equal(t, want.Source, decoded.Dates[i].Source, "candidate %d source", i)
		assert.Equal(t, want.Field, decoded.Dates[i].Field, "candidate %d field", i)
		assert.Equal(t, want.Raw, decoded.Dates[i].Raw, "candidate %d raw", i)
		assert.Equal(t, want.Context, decoded.Dates[i].Context, "candidate %d context", i)
	}

	require.NotNil(t, event.PageSEOOriginal)
	require.Len(t, event.PageSEOOriginal.Dates, len(candidates))
	assert.Equal(t, types.DateSourceHTTPHeader, event.PageSEOOriginal.Dates[len(candidates)-1].Source)
}

// TestBuildRequestEvent_BypassPathCapturesOriginDates walks the whole bypass path against
// a real origin: fasthttp header capture, content processing, event construction and
// serialization. It is the closest the repository can get to the end-to-end contract - the
// acceptance harness has no sink that carries page_seo.
func TestBuildRequestEvent_BypassPathCapturesOriginDates(t *testing.T) {
	const originHTML = `<!DOCTYPE html><html><head>` +
		`<title>Dated article</title>` +
		`<script type="application/ld+json">{"@type":"BlogPosting","datePublished":"2024-03-05T10:00:00+02:00","dateModified":"2024-04-01"}</script>` +
		`<meta property="article:published_time" content="2024-03-05">` +
		`</head><body><time datetime="2024-03-05">March 5</time></body></html>`

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set(types.LastModifiedHeader, testOriginLastModified)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(originHTML))
	}))
	defer origin.Close()

	ssrfOff := false
	svc := bypass.NewBypassService(&config.GlobalBypassConfig{
		UserAgent:      "EdgeCometTest/1.0",
		SSRFProtection: &ssrfOff,
	}, zap.NewNop())

	resp, err := svc.FetchContent(origin.URL, nil, "", zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	processed := orchestrator.ProcessContent(
		context.Background(),
		resp.Body,
		resp.StatusCode,
		resp.Headers,
		origin.URL,
		false,
		1,
		nil,
		zap.NewNop(),
	)

	renderCtx := createTestRenderContext()
	event := BuildRequestEvent(renderCtx, &orchestrator.RenderResult{
		Source:     orchestrator.ServedFromBypass,
		StatusCode: resp.StatusCode,
		PageSEO:    processed.PageSEO,
	}, time.Second, "eg-1")

	require.NotNil(t, event.PageSEO)
	assert.Equal(t, []DateCandidateEvent{
		{Source: types.DateSourceJSONLD, Field: types.DateFieldPublished, Raw: "2024-03-05T10:00:00+02:00", Context: "BlogPosting"},
		{Source: types.DateSourceJSONLD, Field: types.DateFieldModified, Raw: "2024-04-01", Context: "BlogPosting"},
		{Source: types.DateSourceMeta, Field: types.DateFieldPublished, Raw: "2024-03-05", Context: "article:published_time"},
		{Source: types.DateSourceTimeElement, Field: types.DateFieldUnknown, Raw: "2024-03-05", Context: ""},
		{Source: types.DateSourceHTTPHeader, Field: types.DateFieldModified, Raw: testOriginLastModified, Context: types.LastModifiedHeader},
	}, event.PageSEO.Dates)

	encoded, err := json.Marshal(event.PageSEO)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"source":"http_header"`)
	assert.Contains(t, string(encoded), `"context":"Last-Modified"`)
}

// TestBuildRequestEvent_BypassPathWithoutOriginDates covers the other half of the same
// path: an origin sending neither date markup nor Last-Modified still emits the key.
func TestBuildRequestEvent_BypassPathWithoutOriginDates(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Undated</title></head><body><p>No dates</p></body></html>`))
	}))
	defer origin.Close()

	ssrfOff := false
	svc := bypass.NewBypassService(&config.GlobalBypassConfig{
		UserAgent:      "EdgeCometTest/1.0",
		SSRFProtection: &ssrfOff,
	}, zap.NewNop())

	resp, err := svc.FetchContent(origin.URL, nil, "", zap.NewNop())
	require.NoError(t, err)

	// Go's httptest server sends Date but never Last-Modified for a handler-written body.
	_, hasLastModified := resp.Headers[types.LastModifiedHeader]
	require.False(t, hasLastModified)

	processed := orchestrator.ProcessContent(
		context.Background(), resp.Body, resp.StatusCode, resp.Headers, origin.URL,
		false, 1, nil, zap.NewNop(),
	)

	event := BuildRequestEvent(createTestRenderContext(), &orchestrator.RenderResult{
		Source:     orchestrator.ServedFromBypass,
		StatusCode: resp.StatusCode,
		PageSEO:    processed.PageSEO,
	}, time.Second, "eg-1")

	require.NotNil(t, event.PageSEO)
	assert.Empty(t, event.PageSEO.Dates)

	encoded, err := json.Marshal(event.PageSEO)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(encoded), emptyDatesJSON),
		"an inspected page with no date signal must serialize dates as an empty array")
}
