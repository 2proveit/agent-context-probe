package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/config"
)

func TestForwardChatCompletionsPreservesRequestAndBasePath(t *testing.T) {
	var receivedPath string
	var receivedCookie string
	var receivedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedCookie = r.Header.Get("Cookie")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		receivedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"completion-1"}`))
	}))
	defer upstream.Close()

	openAIProvider := NewOpenAIProvider(&config.OpenAIProviderConfig{
		BaseURL: upstream.URL + "/standardgwapi/lumi_control/lumi/v1",
	})
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://localhost:3001/v1/chat/completions",
		strings.NewReader(`{"model":"AIME-Atlas","stream":true}`),
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Cookie", "session=test")

	response, err := openAIProvider.ForwardChatCompletions(context.Background(), request)
	if err != nil {
		t.Fatalf("forward request: %v", err)
	}
	defer response.Body.Close()

	if receivedPath != "/standardgwapi/lumi_control/lumi/v1/chat/completions" {
		t.Fatalf("unexpected upstream path: %s", receivedPath)
	}
	if receivedCookie != "session=test" {
		t.Fatalf("cookie header was not preserved")
	}
	if receivedBody != `{"model":"AIME-Atlas","stream":true}` {
		t.Fatalf("request body changed: %s", receivedBody)
	}
}
