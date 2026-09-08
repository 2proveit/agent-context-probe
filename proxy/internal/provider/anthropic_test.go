package provider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/config"
)

func TestAnthropicForwardModels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		basePath string
		version  string
		status   int
		body     string
	}{
		{"root and default version", "", "", http.StatusOK, `{"data":[{"id":"claude-test"}],"has_more":false}`},
		{"gateway and client version", "/gateway/anthropic/", "client-version", http.StatusOK, `{"data":[]}`},
		{"unauthorized", "/gateway", "", http.StatusUnauthorized, `{"type":"error","error":{"type":"authentication_error","message":"invalid key"}}`},
		{"not found", "/gateway", "", http.StatusNotFound, "models not found\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantPath := "/v1/models"
				switch tc.basePath {
				case "/gateway/anthropic/":
					wantPath = "/gateway/anthropic/v1/models"
				case "/gateway":
					wantPath = "/gateway/v1/models"
				}
				if r.Method != http.MethodGet || r.URL.Path != wantPath || r.URL.RawQuery != "limit=10&after_id=model-1" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				wantVersion := tc.version
				if wantVersion == "" {
					wantVersion = "2023-06-01"
				}
				if r.Header.Get("X-Api-Key") != "client-key" || r.Header.Get("Anthropic-Version") != wantVersion {
					t.Error("API key or version changed")
				}
				if r.Header.Get("X-Trace-Id") != "trace-1" || r.Header.Get("Connection") != "" {
					t.Error("unexpected forwarded headers")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || len(body) != 0 {
					t.Errorf("unexpected GET body: %q, %v", body, err)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer upstream.Close()
			p := NewAnthropicProvider(&config.AnthropicProviderConfig{
				BaseURL: upstream.URL + tc.basePath,
				Version: "2023-06-01",
			})
			request := httptest.NewRequest(http.MethodGet, "/v1/models?limit=10&after_id=model-1", nil)
			request.Header.Set("X-Api-Key", "client-key")
			request.Header.Set("X-Trace-Id", "trace-1")
			request.Header.Set("Connection", "keep-alive")
			if tc.version != "" {
				request.Header.Set("Anthropic-Version", tc.version)
			}
			response, err := p.ForwardModels(request.Context(), request)
			if err != nil {
				t.Fatalf("forward models: %v", err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != tc.status || string(body) != tc.body {
				t.Fatalf("unexpected response: status=%d body=%q err=%v", response.StatusCode, body, err)
			}
			if request.Header.Get("Anthropic-Version") != tc.version || request.URL.Path != "/v1/models" {
				t.Fatal("original request was modified")
			}
		})
	}
}

func TestAnthropicForwardModelsConnectionFailure(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	upstream.Close()
	p := NewAnthropicProvider(&config.AnthropicProviderConfig{BaseURL: upstream.URL})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response, err := p.ForwardModels(request.Context(), request)
	if response != nil {
		response.Body.Close()
		t.Fatal("expected no response on connection failure")
	}
	if err == nil {
		t.Fatal("expected connection error")
	}
}
