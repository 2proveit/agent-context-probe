package handler

import "net/http"

// CaptureHeaders clones headers before logging/storage. Header values are
// intentionally retained so the monitor can display the exact exchange.
func CaptureHeaders(headers http.Header) http.Header {
	return headers.Clone()
}
