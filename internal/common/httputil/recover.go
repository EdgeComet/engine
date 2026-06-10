package httputil

import (
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

// RecoverHandler recovers panics from next, logs them through the logger,
// and responds 500. fasthttp does not recover handler panics itself:
// without this wrapper a panic kills the whole process.
func RecoverHandler(next fasthttp.RequestHandler, logger *zap.Logger) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Handler panic",
					zap.Any("panic", r),
					zap.ByteString("path", ctx.Path()),
					zap.Stack("stack"))
				ctx.Response.Reset()
				ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			}
		}()
		next(ctx)
	}
}
