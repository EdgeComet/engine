package httputil

import "iter"

// CopyHeaders copies header key/value pairs out of a pooled fasthttp request or response.
// Both key and value are copied into new strings: the underlying buffers are returned to the
// fasthttp pool as soon as the request completes. Returns nil when there are no headers.
func CopyHeaders(headers iter.Seq2[[]byte, []byte]) map[string][]string {
	var result map[string][]string
	for key, value := range headers {
		if result == nil {
			result = make(map[string][]string)
		}
		name := string(key)
		result[name] = append(result[name], string(value))
	}
	return result
}
