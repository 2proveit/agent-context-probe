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

func TestForwardModelsPreservesRequestAndResponse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			const basePath = "/standardgwapi/lumi_control/lumi/v1"
			body := `{"object":"list","data":[{"id":"model-1","object":"model"}]}`
			if status != http.StatusOK {
				body = `{"error":{"message":"upstream error"}}`
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != basePath+"/models" {
					t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
				}
				if r.URL.RawQuery != "trace=true" || r.Header.Get("X-Trace-Id") != "trace-1" {
					t.Error("query or custom header was not preserved")
				}
				if r.Header.Get("Authorization") != "Bearer configured-key" {
					t.Error("configured API key did not override client authorization")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, body)
			}))
			defer upstream.Close()
			p := NewOpenAIProvider(&config.OpenAIProviderConfig{
				BaseURL: upstream.URL + basePath + "/",
				APIKey:  "configured-key",
			})
			request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost:3001/v1/models?trace=true", nil)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			request.Header.Set("Authorization", "Bearer client-key")
			request.Header.Set("X-Trace-Id", "trace-1")
			response, err := p.ForwardModels(request.Context(), request)
			if err != nil {
				t.Fatalf("forward models: %v", err)
			}
			defer response.Body.Close()
			got, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			if response.StatusCode != status || string(got) != body {
				t.Fatalf("response changed: status=%d body=%s", response.StatusCode, got)
			}
		})
	}
}

func TestForwardModelsConnectionFailure(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	upstream.Close()
	p := NewOpenAIProvider(&config.OpenAIProviderConfig{BaseURL: upstream.URL + "/v1"})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response, err := p.ForwardModels(request.Context(), request)
	if response != nil {
		response.Body.Close()
		t.Fatal("expected no response when connection fails")
	}
	if err == nil {
		t.Fatal("expected an upstream connection error")
	}
}
