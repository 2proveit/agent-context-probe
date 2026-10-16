package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/middleware"
)

func responseFaultRequest(path string, stream bool, tools bool) []byte {
	body := map[string]interface{}{"model": "client-model", "max_tokens": 32, "stream": stream, "custom_field": map[string]interface{}{"untouched": true}}
	if path == "/v1/responses" {
		body["input"] = "ping"
	} else {
		body["messages"] = []interface{}{map[string]string{"role": "user", "content": "ping"}}
	}
	if tools {
		schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		switch path {
		case "/v1/messages":
			body["tools"] = []interface{}{map[string]interface{}{"name": "test_tool", "description": "Synthetic test only", "input_schema": schema}}
		case "/v1/responses":
			body["tools"] = []interface{}{map[string]interface{}{"type": "function", "name": "test_tool", "parameters": schema}}
		default:
			body["tools"] = []interface{}{map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "test_tool", "parameters": schema}}}
		}
	}
	raw, _ := json.Marshal(body)
	return raw
}

func healthyFaultResponse(path string, stream bool) string {
	var body string
	switch path {
	case "/v1/messages":
		body = `{"id":"up_1","type":"message","model":"client-model","role":"assistant","content":[{"type":"text","text":"healthy-upstream"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	case "/v1/responses":
		body = `{"id":"up_1","object":"response","model":"client-model","status":"completed","output":[{"id":"m_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"healthy-upstream","annotations":[]}]}]}`
	default:
		body = `{"id":"up_1","object":"chat.completion","model":"client-model","choices":[{"index":0,"message":{"role":"assistant","content":"healthy-upstream"},"finish_reason":"stop"}]}`
	}
	if !stream {
		return body
	}
	switch path {
	case "/v1/messages":
		return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"up_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"client-model\",\"content\":[],\"usage\":{}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"healthy-upstream\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	case "/v1/responses":
		return "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + body + "}\n\n"
	default:
		return "data: {\"id\":\"up_1\",\"model\":\"client-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"healthy-upstream\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	}
}

func faultApplication(t *testing.T, settings config.ResponseCorruptionConfig, upstream http.HandlerFunc) (*App, *httptest.Server) {
	t.Helper()
	mock := httptest.NewServer(upstream)
	t.Cleanup(mock.Close)
	cfg := testConfig(t)
	cfg.ResponseCorruption = settings
	cfg.Providers.OpenAI.BaseURL = mock.URL + "/v1"
	cfg.Providers.Anthropic.BaseURL = mock.URL
	application, err := New(cfg, log.New(io.Discard, "", 0), Options{Assets: testAssets()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	server := httptest.NewServer(application.Handler())
	t.Cleanup(server.Close)
	return application, server
}
func sendFaultHTTP(t *testing.T, server *httptest.Server, path string, body []byte) (http.Header, []byte, int) {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Post(server.URL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.Header, raw, response.StatusCode
}

func TestResponseCorruptionAtDownstreamBoundary(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			for _, mode := range []string{"empty_stop", "repetition", "pseudo_tags", "invalid_tool_arguments", "truncate_stream"} {
				t.Run(fmt.Sprintf("%s/%t/%s", path, stream, mode), func(t *testing.T) {
					original := responseFaultRequest(path, stream, true)
					healthy := healthyFaultResponse(path, stream)
					application, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, Probability: 1, Seed: 42, Modes: []string{mode}, TruncateAfterBytes: 32}, func(w http.ResponseWriter, r *http.Request) {
						got, _ := io.ReadAll(r.Body)
						if !bytes.Equal(got, original) {
							t.Errorf("upstream request changed: %s", got)
						}
						kind := "application/json"
						if stream {
							kind = "text/event-stream"
						}
						w.Header().Set("Content-Type", kind)
						if !stream {
							w.Header().Set("Content-Length", strconv.Itoa(len(healthy)))
						}
						_, _ = io.WriteString(w, healthy)
					})
					headers, raw, status := sendFaultHTTP(t, server, path, original)
					if status != 200 {
						t.Fatalf("status %d: %s", status, raw)
					}
					if mode == "truncate_stream" && !stream {
						if string(raw) != healthy || headers.Get(middleware.CorruptionHeader) != "" {
							t.Fatal("stream-only mode changed JSON")
						}
						return
					}
					if headers.Get(middleware.CorruptionHeader) != mode || headers.Get(middleware.CorruptionIDHeader) == "" {
						t.Fatalf("missing marker: %v", headers)
					}
					if bytes.Contains(raw, []byte("healthy-upstream")) {
						t.Fatal("original output leaked after injected response")
					}
					if mode == "truncate_stream" {
						if len(raw) > 32 || len(raw) == 0 || bytes.Contains(raw, []byte("[DONE]")) || bytes.Contains(raw, []byte("message_stop")) || bytes.Contains(raw, []byte("\n\n")) {
							t.Fatalf("stream not truncated: %s", raw)
						}
						return
					}
					if stream {
						terminal := "[DONE]"
						if path == "/v1/messages" {
							terminal = "message_stop"
						}
						if path == "/v1/responses" {
							terminal = "response.completed"
						}
						if !bytes.Contains(raw, []byte(terminal)) {
							t.Fatalf("semantic fault has invalid stream termination: %s", raw)
						}
						// Each envelope stays parseable; only tool argument JSON is deliberately bad.
						for _, line := range strings.Split(string(raw), "\n") {
							if strings.HasPrefix(line, "data:") {
								data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
								if data != "[DONE]" && !json.Valid([]byte(data)) {
									t.Fatalf("invalid SSE envelope: %s", line)
								}
							}
						}
					} else if !json.Valid(raw) {
						t.Fatalf("invalid response envelope: %s", raw)
					}
					switch mode {
					case "empty_stop":
						if !bytes.Contains(raw, []byte("Synthetic reasoning-only")) || bytes.Contains(raw, []byte("test_tool")) {
							t.Fatalf("not reasoning-only stop: %s", raw)
						}
					case "repetition":
						if strings.Count(string(raw), "果树。") < 1400 {
							t.Fatal("missing repeated output")
						}
					case "pseudo_tags":
						if !bytes.Contains(raw, []byte("system_warning")) || !bytes.Contains(raw, []byte("OpenHands Agent")) {
							t.Fatal("missing pseudo-tags")
						}
					case "invalid_tool_arguments":
						if !bytes.Contains(raw, []byte("{invalid")) || !bytes.Contains(raw, []byte("test_tool")) {
							t.Fatal("missing malformed tool arguments")
						}
					}
					// Captured provider bytes are labelled rather than presented as injected output.
					captured := httptest.NewRecorder()
					application.Handler().ServeHTTP(captured, httptest.NewRequest("GET", "/api/requests", nil))
					if !strings.Contains(captured.Body.String(), "upstream-before-injection") || !strings.Contains(captured.Body.String(), headers.Get(middleware.CorruptionIDHeader)) {
						t.Fatalf("missing persisted fault metadata: %s", captured.Body.String())
					}
				})
			}
		}
	}
}

func TestResponseCorruptionDisabledAndZeroRate(t *testing.T) {
	for _, setting := range []config.ResponseCorruptionConfig{{}, {Enabled: false, Probability: 1, Modes: []string{"repetition"}}, {Enabled: true, Probability: 0, Modes: []string{"repetition"}}} {
		_, server := faultApplication(t, setting, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, healthyFaultResponse("/v1/chat/completions", true))
		})
		headers, body, status := sendFaultHTTP(t, server, "/v1/chat/completions", responseFaultRequest("/v1/chat/completions", true, true))
		if status != 200 || headers.Get(middleware.CorruptionHeader) != "" || string(body) != healthyFaultResponse("/v1/chat/completions", true) {
			t.Fatal("default-off/zero-rate forwarding changed")
		}
	}
}

func TestResponseCorruptionPreservesErrorsAndUnrelatedEndpoints(t *testing.T) {
	_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, Probability: 1, Modes: []string{"repetition"}}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"message":"real upstream rate limit"}}`)
	})
	headers, body, status := sendFaultHTTP(t, server, "/v1/chat/completions", responseFaultRequest("/v1/chat/completions", false, true))
	if status != 429 || headers.Get(middleware.CorruptionHeader) != "" || !bytes.Contains(body, []byte("real upstream rate limit")) {
		t.Fatal("real upstream error overwritten")
	}
	response, err := server.Client().Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get(middleware.CorruptionHeader) != "" {
		t.Fatal("health polluted")
	}
}

func TestInjectedStreamCancelsUpstreamPromptly(t *testing.T) {
	cancelled := make(chan struct{})
	_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, Probability: 1, Modes: []string{"empty_stop"}}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	})
	_, body, status := sendFaultHTTP(t, server, "/v1/chat/completions", responseFaultRequest("/v1/chat/completions", true, true))
	if status != 200 || !bytes.Contains(body, []byte("[DONE]")) {
		t.Fatalf("injected stop did not arrive: %s", body)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream was left running after injection")
	}
}

func TestDownstreamConsumerRetriesAfterInjectedEmptyStop(t *testing.T) {
	var calls atomic.Int32
	_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, Probability: .5, Seed: 42, Modes: []string{"empty_stop"}}, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, healthyFaultResponse("/v1/chat/completions", false))
	})
	// A small HTTP consumer demonstrates a bounded recovery path. It is not Lumi
	// or OpenCode acceptance: reject a completed empty message and retry once.
	for attempt := 0; attempt < 2; attempt++ {
		headers, raw, _ := sendFaultHTTP(t, server, "/v1/chat/completions", responseFaultRequest("/v1/chat/completions", false, true))
		var body struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				Finish string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Choices) != 1 {
			t.Fatal("missing choice")
		}
		if attempt == 0 {
			if body.Choices[0].Message.Content != "" || headers.Get(middleware.CorruptionHeader) != "empty_stop" {
				t.Fatal("first attempt must be empty-stop fault")
			}
			continue
		}
		if body.Choices[0].Message.Content != "healthy-upstream" || headers.Get(middleware.CorruptionHeader) != "" {
			t.Fatal("retry did not recover")
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected retry count: %d", calls.Load())
	}
}

func TestResponseCorruptionSeedReplayAndConcurrency(t *testing.T) {
	replay := func() []string {
		_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, Probability: .5, Seed: 73}, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, healthyFaultResponse("/v1/chat/completions", false))
		})
		var modes []string
		for i := 0; i < 16; i++ {
			h, _, _ := sendFaultHTTP(t, server, "/v1/chat/completions", responseFaultRequest("/v1/chat/completions", false, true))
			modes = append(modes, h.Get(middleware.CorruptionHeader))
		}
		return modes
	}
	if fmt.Sprint(replay()) != fmt.Sprint(replay()) {
		t.Fatal("same response order and seed did not replay")
	}
	_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, Probability: .5, Seed: 42}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, healthyFaultResponse("/v1/chat/completions", false))
	})
	var wg sync.WaitGroup
	var healthy, faults atomic.Int32
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, _, _ := sendFaultHTTP(t, server, "/v1/chat/completions", responseFaultRequest("/v1/chat/completions", false, true))
			if h.Get(middleware.CorruptionHeader) == "" {
				healthy.Add(1)
			} else {
				faults.Add(1)
			}
		}()
	}
	wg.Wait()
	if healthy.Load() == 0 || faults.Load() == 0 || healthy.Load()+faults.Load() != 32 {
		t.Fatal("randomized concurrent responses not mixed")
	}
}
