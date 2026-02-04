package clientip

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
)

func newRequestCtx(remoteAddr string, headers map[string]string) *fasthttp.RequestCtx {
	ctx := &fasthttp.RequestCtx{}
	addr, _ := net.ResolveTCPAddr("tcp", remoteAddr)
	ctx.SetRemoteAddr(addr)
	for key, value := range headers {
		ctx.Request.Header.Set(key, value)
	}
	return ctx
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name       string
		headers    []string
		reqHeaders map[string]string
		remoteAddr string
		expected   string
	}{
		{
			name:       "single IPv4 in header",
			headers:    []string{"X-Real-IP"},
			reqHeaders: map[string]string{"X-Real-IP": "203.0.113.50"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "203.0.113.50",
		},
		{
			name:       "XFF comma-separated, leftmost extracted",
			headers:    []string{"X-Forwarded-For"},
			reqHeaders: map[string]string{"X-Forwarded-For": "203.0.113.50, 70.41.3.18, 150.172.238.178"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "203.0.113.50",
		},
		{
			name:       "whitespace trimming",
			headers:    []string{"X-Real-IP"},
			reqHeaders: map[string]string{"X-Real-IP": " 10.0.0.1 "},
			remoteAddr: "1.1.1.1:1234",
			expected:   "10.0.0.1",
		},
		{
			name:       "multiple headers, first populated wins",
			headers:    []string{"X-Real-IP", "X-Forwarded-For"},
			reqHeaders: map[string]string{"X-Forwarded-For": "10.0.0.2"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "10.0.0.2",
		},
		{
			name:       "multiple headers, first has value",
			headers:    []string{"X-Real-IP", "X-Forwarded-For"},
			reqHeaders: map[string]string{"X-Real-IP": "10.0.0.1", "X-Forwarded-For": "10.0.0.2"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "10.0.0.1",
		},
		{
			name:       "all headers empty, RemoteAddr fallback",
			headers:    []string{"X-Real-IP"},
			reqHeaders: nil,
			remoteAddr: "192.168.1.100:54321",
			expected:   "192.168.1.100",
		},
		{
			name:       "nil headers, immediate RemoteAddr fallback",
			headers:    nil,
			reqHeaders: map[string]string{"X-Real-IP": "10.0.0.1"},
			remoteAddr: "192.168.1.100:54321",
			expected:   "192.168.1.100",
		},
		{
			name:       "empty headers slice",
			headers:    []string{},
			reqHeaders: map[string]string{"X-Real-IP": "10.0.0.1"},
			remoteAddr: "192.168.1.100:54321",
			expected:   "192.168.1.100",
		},
		{
			name:       "IPv6 address",
			headers:    []string{"X-Real-IP"},
			reqHeaders: map[string]string{"X-Real-IP": "::1"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "::1",
		},
		{
			name:       "IPv6 with brackets",
			headers:    []string{"X-Real-IP"},
			reqHeaders: map[string]string{"X-Real-IP": "[::1]"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "::1",
		},
		{
			name:       "IPv6 with zone ID",
			headers:    []string{"X-Real-IP"},
			reqHeaders: map[string]string{"X-Real-IP": "fe80::1%eth0"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "fe80::1",
		},
		{
			name:       "IPv4-mapped IPv6",
			headers:    []string{"X-Real-IP"},
			reqHeaders: map[string]string{"X-Real-IP": "::ffff:192.168.1.1"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "192.168.1.1",
		},
		{
			name:       "RemoteAddr IPv6 with port",
			headers:    []string{},
			reqHeaders: nil,
			remoteAddr: "[::1]:8080",
			expected:   "::1",
		},
		{
			name:       "unparseable IP returns raw",
			headers:    []string{"X-Real-IP"},
			reqHeaders: map[string]string{"X-Real-IP": "not-an-ip"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "not-an-ip",
		},
		{
			name:       "header value unknown",
			headers:    []string{"X-Forwarded-For"},
			reqHeaders: map[string]string{"X-Forwarded-For": "unknown"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "unknown",
		},
		{
			name:       "header with only whitespace skips to next",
			headers:    []string{"X-Real-IP", "X-Forwarded-For"},
			reqHeaders: map[string]string{"X-Real-IP": "   ", "X-Forwarded-For": "10.0.0.1"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "10.0.0.1",
		},
		{
			name:       "comma-separated with empty first segment",
			headers:    []string{"X-Forwarded-For"},
			reqHeaders: map[string]string{"X-Forwarded-For": " , 10.0.0.2"},
			remoteAddr: "1.1.1.1:1234",
			expected:   "1.1.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newRequestCtx(tt.remoteAddr, tt.reqHeaders)
			result := Extract(ctx, tt.headers)
			assert.Equal(t, tt.expected, result)
		})
	}
}
