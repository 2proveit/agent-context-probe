package app

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelMappingsStayOnIncomingProtocol(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		upstreamPath string
	}{
		{"anthropic messages", "/v1/messages", "/anthropic/v1/messages"},
		{"openai chat", "/v1/chat/completions", "/openai/v1/chat/completions"},
		{"openai responses", "/v1/responses", "/openai/v1/responses"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.upstreamPath {
					t.Errorf("upstream path = %q, want %q", r.URL.Path, tc.upstreamPath)
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode upstream body: %v", err)
				}
				var gotModel string
				if err := json.Unmarshal(body["model"], &gotModel); err != nil || gotModel != "target-model" {
					t.Errorf("upstream model = %q, error = %v", gotModel, err)
				}
				if string(body["custom_field"]) != `{"nested":true}` {
					t.Errorf("custom field changed: %s", body["custom_field"])
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"model":"target-model","content":[]}`)
			}))
			defer upstream.Close()

			cfg := testConfig(t)
			cfg.ModelMappings = map[string]string{"source-model": "target-model"}
			cfg.Providers.Anthropic.BaseURL = upstream.URL + "/anthropic"
			cfg.Providers.OpenAI.BaseURL = upstream.URL + "/openai/v1"
			application, err := New(cfg, log.New(io.Discard, "", 0), Options{Assets: testAssets()})
			if err != nil {
				t.Fatalf("create application: %v", err)
			}
			defer application.Close()

			request := httptest.NewRequest(http.MethodPost, tc.endpoint, strings.NewReader(`{"model":"source-model","custom_field":{"nested":true}}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			application.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("response status = %d, body = %s", response.Code, response.Body.String())
			}
			var clientBody map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &clientBody); err != nil {
				t.Fatalf("decode client response: %v", err)
			}
			if string(clientBody["model"]) != `"source-model"` {
				t.Fatalf("client-visible model = %s, want source-model", clientBody["model"])
			}
		})
	}
}

func TestModelMappingsRestoreStreamingResponseModel(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		stream   string
	}{
		{
			"anthropic messages", "/v1/messages",
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"target-model\",\"usage\":{}}}\n\n" +
				"data: {\"type\":\"message_stop\"}\n\n",
		},
		{
			"openai chat", "/v1/chat/completions",
			"data: {\"id\":\"chat_1\",\"object\":\"chat.completion.chunk\",\"model\":\"target-model\",\"choices\":[]}\n\n" +
				"data: [DONE]\n\n",
		},
		{
			"openai responses", "/v1/responses",
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"target-model\",\"status\":\"completed\"}}\n\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model  string `json:"model"`
					Stream bool   `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != "target-model" || !body.Stream {
					t.Errorf("unexpected upstream request: %+v, error = %v", body, err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.stream)
			}))
			defer upstream.Close()
			cfg := testConfig(t)
			cfg.ModelMappings = map[string]string{"source-model": "target-model"}
			cfg.Providers.Anthropic.BaseURL = upstream.URL
			cfg.Providers.OpenAI.BaseURL = upstream.URL + "/v1"
			application, err := New(cfg, log.New(io.Discard, "", 0), Options{Assets: testAssets()})
			if err != nil {
				t.Fatalf("create application: %v", err)
			}
			defer application.Close()
			request := httptest.NewRequest(http.MethodPost, tc.endpoint, strings.NewReader(`{"model":"source-model","stream":true}`))
			response := httptest.NewRecorder()
			application.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("response status = %d, body = %s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"model":"source-model"`) || strings.Contains(response.Body.String(), `"model":"target-model"`) {
				t.Fatalf("client stream model was not restored: %s", response.Body.String())
			}
		})
	}
}
