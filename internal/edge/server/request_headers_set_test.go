package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"

	"github.com/edgecomet/engine/internal/common/config"
)

// The two halves of the origin request header set are written in different packages and only ever
// meet on the request path: ExtractClientHeaders picks headers off the incoming bot request, and
// ApplyRequestHeaders overlays the values configuration sets. These cases pin the seam, including
// the precache shape where there is no incoming request at all.
func TestExtractAndApplyRequestHeaders(t *testing.T) {
	const (
		apiKeyHeader     = "X-Api-Key"
		apiKeyLowerSpell = "x-api-key"
		tenantHeader     = "X-Tenant-Id"
		authHeader       = "Authorization"

		configuredKey  = "configured-key"
		forwardedKey   = "forwarded-key"
		forwardedToken = "Bearer token"
	)

	newRequestCtx := func(headers map[string]string) *fasthttp.RequestCtx {
		ctx := &fasthttp.RequestCtx{}
		for name, value := range headers {
			ctx.Request.Header.Set(name, value)
		}
		return ctx
	}

	t.Run("forwarded only", func(t *testing.T) {
		ctx := newRequestCtx(map[string]string{authHeader: forwardedToken})
		resolved := &config.ResolvedConfig{}

		headers := resolved.ApplyRequestHeaders(ExtractClientHeaders(ctx, []string{authHeader}))

		assert.Equal(t, map[string][]string{authHeader: {forwardedToken}}, headers)
	})

	t.Run("set only", func(t *testing.T) {
		ctx := newRequestCtx(nil)
		resolved := &config.ResolvedConfig{
			RequestHeadersSet: map[string]string{apiKeyHeader: configuredKey},
		}

		headers := resolved.ApplyRequestHeaders(ExtractClientHeaders(ctx, []string{authHeader}))

		assert.Equal(t, map[string][]string{apiKeyHeader: {configuredKey}}, headers)
	})

	t.Run("forwarded and set", func(t *testing.T) {
		ctx := newRequestCtx(map[string]string{authHeader: forwardedToken})
		resolved := &config.ResolvedConfig{
			RequestHeadersSet: map[string]string{apiKeyHeader: configuredKey, tenantHeader: "acme"},
		}

		headers := resolved.ApplyRequestHeaders(ExtractClientHeaders(ctx, []string{authHeader}))

		assert.Equal(t, map[string][]string{
			authHeader:   {forwardedToken},
			apiKeyHeader: {configuredKey},
			tenantHeader: {"acme"},
		}, headers)
	})

	t.Run("set header wins over a forwarded header spelled differently", func(t *testing.T) {
		// The case divergence has to come from the configuration side: fasthttp canonicalises
		// incoming header names, so whatever spelling the bot puts on the wire arrives as
		// X-Api-Key and could never differ from a canonically spelled configured name.
		ctx := newRequestCtx(map[string]string{apiKeyHeader: forwardedKey})
		resolved := &config.ResolvedConfig{
			RequestHeadersSet: map[string]string{apiKeyLowerSpell: configuredKey},
		}

		headers := resolved.ApplyRequestHeaders(ExtractClientHeaders(ctx, []string{apiKeyHeader}))

		assert.Equal(t, map[string][]string{apiKeyLowerSpell: {configuredKey}}, headers,
			"the origin must receive one entry, spelled the way the configuration spelled it")
	})

	t.Run("precache has no incoming request", func(t *testing.T) {
		resolved := &config.ResolvedConfig{
			RequestHeadersSet: map[string]string{apiKeyHeader: configuredKey},
		}

		headers := resolved.ApplyRequestHeaders(nil)

		assert.Equal(t, map[string][]string{apiKeyHeader: {configuredKey}}, headers)
	})

	t.Run("nothing configured and nothing forwarded", func(t *testing.T) {
		ctx := newRequestCtx(nil)
		resolved := &config.ResolvedConfig{}

		assert.Nil(t, resolved.ApplyRequestHeaders(ExtractClientHeaders(ctx, nil)))
	})
}
