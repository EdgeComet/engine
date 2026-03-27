package ajax

import "strings"

// ajaxAcceptTypes are Accept header values indicating non-HTML API requests
var ajaxAcceptTypes = []string{
	"application/json",
	"application/xml",
	"text/xml",
	"application/rss+xml",
	"application/atom+xml",
}

// htmlAcceptType indicates a browser page navigation request
const htmlAcceptType = "text/html"

// xhrHeaderValue is the standard X-Requested-With value for XMLHttpRequest
const xhrHeaderValue = "xmlhttprequest"

// IsAjaxRequest checks whether the request is an AJAX call based on
// the Accept and X-Requested-With headers.
// Returns true if the request should be bypassed instead of rendered.
func IsAjaxRequest(acceptHeader, xRequestedWith string) bool {
	if strings.EqualFold(xRequestedWith, xhrHeaderValue) {
		return true
	}

	if acceptHeader == "" {
		return false
	}

	acceptLower := strings.ToLower(acceptHeader)

	// Browser page requests always include text/html in Accept
	if strings.Contains(acceptLower, htmlAcceptType) {
		return false
	}

	for _, ajaxType := range ajaxAcceptTypes {
		if strings.Contains(acceptLower, ajaxType) {
			return true
		}
	}

	return false
}
