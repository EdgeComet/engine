package clientip

import (
	"net"
	"strings"

	"github.com/valyala/fasthttp"
)

// Extract returns the client IP from the first non-empty configured header,
// falling back to RemoteAddr if all headers are empty or not configured.
func Extract(ctx *fasthttp.RequestCtx, headers []string) string {
	for _, header := range headers {
		value := strings.TrimSpace(string(ctx.Request.Header.Peek(header)))
		if value == "" {
			continue
		}
		if ip := parseHeaderValue(value); ip != "" {
			return ip
		}
	}
	return parseRemoteAddr(ctx.RemoteAddr().String())
}

func parseHeaderValue(value string) string {
	if idx := strings.IndexByte(value, ','); idx >= 0 {
		value = value[:idx]
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return normalizeIP(value)
}

func parseRemoteAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return normalizeIP(addr)
	}
	return normalizeIP(host)
}

func normalizeIP(raw string) string {
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	if idx := strings.IndexByte(raw, '%'); idx >= 0 {
		raw = raw[:idx]
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return raw
	}
	return ip.String()
}
