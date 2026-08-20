package capture

import (
	"net/http"
	"strings"
)

const RedactedValue = "[REDACTED]"

var sensitiveHeaders = map[string]struct{}{
	"api-key":                     {},
	"authorization":               {},
	"cookie":                      {},
	"proxy-authorization":         {},
	"set-cookie":                  {},
	"x-access-token":              {},
	"x-agent-context-probe-token": {},
	"x-anthropic-api-key":         {},
	"x-api-key":                   {},
	"x-auth-token":                {},
	"x-openai-api-key":            {},
}

// Headers returns a detached copy suitable for persistence and display. The
// original header map remains untouched so providers can forward credentials.
func Headers(headers http.Header) http.Header {
	redacted := headers.Clone()
	for name, values := range redacted {
		if !IsSensitiveHeader(name) {
			continue
		}

		if len(values) == 0 {
			redacted[name] = []string{RedactedValue}
			continue
		}
		for index := range values {
			values[index] = RedactedValue
		}
	}
	return redacted
}

func IsSensitiveHeader(name string) bool {
	normalized := strings.ToLower(name)
	if _, ok := sensitiveHeaders[normalized]; ok {
		return true
	}
	return strings.HasSuffix(normalized, "-api-key") ||
		strings.HasSuffix(normalized, "-secret") ||
		strings.HasSuffix(normalized, "-token")
}
