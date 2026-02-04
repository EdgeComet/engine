package httputil

import (
	"encoding/json"

	"github.com/valyala/fasthttp"
)

// APIResponse is the unified response format for all APIs
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// JSONResponse sends a JSON response with the unified format
func JSONResponse(ctx *fasthttp.RequestCtx, success bool, message string, data interface{}, statusCode int) {
	resp := APIResponse{
		Success: success,
		Message: message,
		Data:    data,
	}
	body, _ := json.Marshal(resp)
	ctx.SetStatusCode(statusCode)
	ctx.SetContentType("application/json")
	ctx.SetBody(body)
}

// JSONError is a convenience wrapper for error responses
func JSONError(ctx *fasthttp.RequestCtx, message string, statusCode int) {
	JSONResponse(ctx, false, message, nil, statusCode)
}

// JSONSuccess is a convenience wrapper for success responses with no data
func JSONSuccess(ctx *fasthttp.RequestCtx, message string, statusCode int) {
	JSONResponse(ctx, true, message, nil, statusCode)
}

// JSONData is a convenience wrapper for success responses with data
func JSONData(ctx *fasthttp.RequestCtx, data interface{}, statusCode int) {
	JSONResponse(ctx, true, "", data, statusCode)
}
