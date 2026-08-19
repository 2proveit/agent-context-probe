package handler

import (
	"net/http"

	"github.com/seifghazi/claude-code-monitor/internal/capture"
)

// CaptureHeaders clones headers and removes credentials before persistence.
// Providers continue to receive the original request headers.
func CaptureHeaders(headers http.Header) http.Header {
	return capture.Headers(headers)
}
