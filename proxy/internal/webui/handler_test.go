package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testHandler(t *testing.T) *Handler {
	t.Helper()
	handler, err := NewHandler(fstest.MapFS{
		"index.html":        &fstest.MapFile{Data: []byte("<html>dashboard</html>")},
		"assets/app-123.js": &fstest.MapFile{Data: []byte("console.log('ok')")},
		"favicon.ico":       &fstest.MapFile{Data: []byte("icon")},
	})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}
	return handler
}

func TestHandlerServesIndexAssetsAndSPAFallback(t *testing.T) {
	handler := testHandler(t)
	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
		wantCache  string
	}{
		{path: "/", wantStatus: http.StatusOK, wantBody: "dashboard", wantCache: "no-cache"},
		{path: "/ui", wantStatus: http.StatusOK, wantBody: "dashboard", wantCache: "no-cache"},
		{path: "/sessions/example", wantStatus: http.StatusOK, wantBody: "dashboard", wantCache: "no-cache"},
		{path: "/assets/app-123.js", wantStatus: http.StatusOK, wantBody: "console.log", wantCache: "immutable"},
		{path: "/missing.js", wantStatus: http.StatusNotFound, wantBody: "not found"},
		{path: "/api/missing", wantStatus: http.StatusNotFound, wantBody: "not found"},
		{path: "/v1/missing", wantStatus: http.StatusNotFound, wantBody: "not found"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if !strings.Contains(recorder.Body.String(), test.wantBody) {
				t.Fatalf("body = %q, want %q", recorder.Body.String(), test.wantBody)
			}
			if test.wantCache != "" && !strings.Contains(recorder.Header().Get("Cache-Control"), test.wantCache) {
				t.Fatalf("cache header = %q", recorder.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestHandlerSupportsHeadAndRejectsMutation(t *testing.T) {
	handler := testHandler(t)
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/assets/app-123.js", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD response = %d, %q", head.Code, head.Body.String())
	}

	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", post.Code)
	}
}
