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
	var receivedAuthorization string
	var receivedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedCookie = r.Header.Get("Cookie")
		receivedAuthorization = r.Header.Get("Authorization")
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
	request.Header.Set("Authorization", "Bearer client-key")

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
	if receivedAuthorization != "Bearer client-key" {
		t.Fatalf("client authorization header was not preserved")
	}
	if receivedBody != `{"model":"AIME-Atlas","stream":true}` {
		t.Fatalf("request body changed: %s", receivedBody)
	}
}

func TestForwardResponsesPreservesRequestQueryAndBasePath(t *testing.T) {
	var receivedPath string
	var receivedQuery string
	var receivedAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		receivedAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed"}`))
	}))
	defer upstream.Close()

	openAIProvider := NewOpenAIProvider(&config.OpenAIProviderConfig{
		BaseURL: upstream.URL + "/gateway/openai/v1",
		APIKey:  "configured-key",
	})
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://localhost:3001/v1/responses?trace=true",
		strings.NewReader(`{"model":"gpt-5","input":"hello","unknown":{"keep":true}}`),
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer client-key")

	response, err := openAIProvider.ForwardResponses(context.Background(), request)
	if err != nil {
		t.Fatalf("forward request: %v", err)
	}
	defer response.Body.Close()

	if receivedPath != "/gateway/openai/v1/responses" {
		t.Fatalf("unexpected upstream path: %s", receivedPath)
	}
	if receivedQuery != "trace=true" {
		t.Fatalf("unexpected query: %s", receivedQuery)
	}
	if receivedAuthorization != "Bearer configured-key" {
		t.Fatalf("configured API key did not override client authorization")
	}
}
