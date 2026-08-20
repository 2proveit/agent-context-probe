package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (r *flushRecorder) Flush() {
	r.flushed = true
	r.ResponseRecorder.Flush()
}

func TestLoggingPreservesStreamingFlush(t *testing.T) {
	wrappedHandler := Logging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("logging response writer does not implement http.Flusher")
		}
		_, _ = w.Write([]byte("data: first\n\n"))
		flusher.Flush()
	}))

	recorder := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	wrappedHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil))

	if !recorder.flushed {
		t.Fatal("flush was not forwarded to the underlying response writer")
	}
}
