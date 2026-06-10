package httputil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRecoverHandlerPanic(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)

	handler := RecoverHandler(func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusOK)
		panic("handler exploded")
	}, logger)

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/render/test")
	require.NotPanics(t, func() { handler(ctx) })

	assert.Equal(t, fasthttp.StatusInternalServerError, ctx.Response.StatusCode())

	entries := logs.All()
	require.Len(t, entries, 1)
	assert.Equal(t, "Handler panic", entries[0].Message)
	fields := entries[0].ContextMap()
	assert.Equal(t, "handler exploded", fields["panic"])
	assert.Equal(t, "/render/test", fields["path"])
}

func TestRecoverHandlerPassthrough(t *testing.T) {
	handler := RecoverHandler(func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusAccepted)
	}, zap.NewNop())

	ctx := &fasthttp.RequestCtx{}
	handler(ctx)
	assert.Equal(t, fasthttp.StatusAccepted, ctx.Response.StatusCode())
}
