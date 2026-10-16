package handler

import (
	"net/http"

	"github.com/seifghazi/claude-code-monitor/internal/model"
)

func recordResponseCorruption(response *model.ResponseLog, w http.ResponseWriter) {
	recorder, ok := w.(interface{ CorruptionHeaders() http.Header })
	if !ok {
		return
	}
	headers := recorder.CorruptionHeaders()
	if len(headers) == 0 {
		return
	}
	if response.Headers == nil {
		response.Headers = make(http.Header)
	}
	for key, values := range headers {
		response.Headers[key] = append([]string(nil), values...)
	}
}
