package app

import (
	"bufio"
	"bytes"
	"context"
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
			for _, mode := range []string{"empty_stop", "repetition", "pseudo_tags", "invalid_tool_arguments", "xml_tool_name", "truncate_stream"} {
				t.Run(fmt.Sprintf("%s/%t/%s", path, stream, mode), func(t *testing.T) {
					original := responseFaultRequest(path, stream, true)
					healthy := healthyFaultResponse(path, stream)
					if mode == "xml_tool_name" || mode == "invalid_tool_arguments" {
						healthy = healthyToolFaultResponse(path, stream)
					}
					application, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 1, Seed: 42, Modes: []string{mode}, TruncateAfterBytes: 32}, func(w http.ResponseWriter, r *http.Request) {
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
						if bytes.Contains(raw, []byte("healthy-upstream")) || bytes.Contains(raw, []byte("test_tool")) {
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
					case "xml_tool_name":
						assertXMLToolFault(t, raw, stream)
					case "invalid_tool_arguments":
						if !bytes.Contains(raw, []byte("{invalid")) || !bytes.Contains(raw, []byte("test_tool")) {
							t.Fatal("missing malformed tool arguments")
						}
					}
					if mode == "xml_tool_name" || mode == "invalid_tool_arguments" {
						for _, value := range []string{"up_1", "call_remote", "remote-model", "9007199254740993"} {
							if !bytes.Contains(raw, []byte(value)) {
								t.Fatalf("lost remote identity/metadata %s: %s", value, raw)
							}
						}
						if bytes.Contains(raw, []byte("chaos_")) {
							t.Fatal("fault ID leaked into model response body")
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

func TestResponseCorruptionDisabled(t *testing.T) {
	for _, setting := range []config.ResponseCorruptionConfig{{}, {Enabled: false, EveryNRequests: 1, Modes: []string{"repetition"}}} {
		_, server := faultApplication(t, setting, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, healthyFaultResponse("/v1/chat/completions", true))
		})
		headers, body, status := sendFaultHTTP(t, server, "/v1/chat/completions", responseFaultRequest("/v1/chat/completions", true, true))
		if status != 200 || headers.Get(middleware.CorruptionHeader) != "" || string(body) != healthyFaultResponse("/v1/chat/completions", true) {
			t.Fatal("default-off forwarding changed")
		}
	}
}

func TestResponseCorruptionPreservesErrorsAndUnrelatedEndpoints(t *testing.T) {
	_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 1, Modes: []string{"repetition"}}, func(w http.ResponseWriter, r *http.Request) {
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

func TestInjectedStreamPreservesLateUsage(t *testing.T) {
	_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 1, Modes: []string{"empty_stop"}}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"remote-id\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		w.(http.Flusher).Flush()
		if r.Context().Err() != nil {
			t.Error("metadata fault cancelled provider before late usage")
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"remote-id\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"healthy-upstream\"},\"finish_reason\":\"stop\"}],\"usage\":{\"total_tokens\":56}}\n\ndata: [DONE]\n\n")
	})
	_, body, status := sendFaultHTTP(t, server, "/v1/chat/completions", responseFaultRequest("/v1/chat/completions", true, true))
	if status != 200 || !bytes.Contains(body, []byte(`"total_tokens":56`)) || !bytes.Contains(body, []byte(`"id":"remote-id"`)) || bytes.Contains(body, []byte("healthy-upstream")) {
		t.Fatalf("late metadata not preserved: %s", body)
	}
}

func TestDownstreamConsumerRetriesAfterInjectedEmptyStop(t *testing.T) {
	var calls atomic.Int32
	_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 2, Seed: 42, Modes: []string{"empty_stop"}}, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, healthyFaultResponse("/v1/chat/completions", false))
	})
	// Warm up one eligible request so this consumer starts at the second,
	// guaranteed faulty response; its retry is the third, healthy response.
	headers, _, _ := sendFaultHTTP(t, server, "/v1/chat/completions", responseFaultRequest("/v1/chat/completions", false, true))
	if headers.Get(middleware.CorruptionHeader) != "" {
		t.Fatal("warm-up must be healthy")
	}
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
	if calls.Load() != 3 {
		t.Fatalf("unexpected retry count: %d", calls.Load())
	}
}

func TestResponseCorruptionSeedReplayAndConcurrency(t *testing.T) {
	replay := func() []string {
		_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 2, Seed: 73}, func(w http.ResponseWriter, r *http.Request) {
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
	first := replay()
	for i, mode := range first {
		if (mode != "") != ((i+1)%2 == 0) {
			t.Fatalf("request %d: unexpected trigger mode %q", i+1, mode)
		}
	}
	if fmt.Sprint(first) != fmt.Sprint(replay()) {
		t.Fatal("same response order and seed did not replay")
	}
	_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 2, Seed: 42}, func(w http.ResponseWriter, r *http.Request) {
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
	if healthy.Load() != 16 || faults.Load() != 16 {
		t.Fatal("expected exactly 16 healthy and 16 faulty concurrent responses")
	}
}

// Check decoded fields rather than raw text: JSON encoding escapes XML tags,
// and the regression is specifically an unknown name with valid arguments.
func assertXMLToolFault(t *testing.T, raw []byte, stream bool) {
	t.Helper()
	expected := "<function=test_tool>\n<parameter=name>\nsynthetic-fault\n</parameter>\n</function>"
	names, arguments := 0, 0
	var walk func(interface{})
	walk = func(value interface{}) {
		switch v := value.(type) {
		case map[string]interface{}:
			if name, ok := v["name"]; ok {
				if name != expected {
					t.Fatalf("tool name was repaired or altered: %v", name)
				}
				names++
			}
			for _, key := range []string{"arguments", "partial_json"} {
				if argument, ok := v[key]; ok {
					if argument != "{}" && argument != "" {
						t.Fatalf("name fault damaged %s: %v", key, argument)
					}
					if argument == "{}" {
						arguments++
					}
				}
			}
			if input, ok := v["input"]; ok {
				object, ok := input.(map[string]interface{})
				if !ok || len(object) != 0 {
					t.Fatalf("name fault damaged Messages input: %v", input)
				}
				arguments++
			}
			if kind, _ := v["type"].(string); kind == "response.function_call_arguments.delta" && v["delta"] != "{}" {
				t.Fatalf("name fault damaged argument delta: %v", v["delta"])
			}
			for _, key := range []string{"finish_reason", "stop_reason"} {
				if reason := v[key]; reason != nil && reason != "tool_calls" && reason != "tool_use" {
					t.Fatalf("name fault lost tool-call finish: %v", reason)
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []interface{}:
			for _, child := range v {
				walk(child)
			}
		}
	}
	decode := func(data []byte) {
		var value interface{}
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		walk(value)
	}
	if stream {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data != "[DONE]" {
					decode([]byte(data))
				}
			}
		}
	} else {
		decode(raw)
	}
	if names == 0 || arguments == 0 {
		t.Fatalf("missing XML tool name or valid arguments: names=%d arguments=%d", names, arguments)
	}
}

func TestResponseCorruptionToolFaultRequiresTool(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			for _, mode := range []string{"invalid_tool_arguments", "xml_tool_name"} {
				t.Run(fmt.Sprintf("%s/%t/%s", path, stream, mode), func(t *testing.T) {
					healthy := healthyFaultResponse(path, stream)
					_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 1, Modes: []string{mode}}, func(w http.ResponseWriter, r *http.Request) {
						kind := "application/json"
						if stream {
							kind = "text/event-stream"
						}
						w.Header().Set("Content-Type", kind)
						_, _ = io.WriteString(w, healthy)
					})
					headers, body, status := sendFaultHTTP(t, server, path, responseFaultRequest(path, stream, false))
					// The existing Messages handler forwards data frames and removes
					// event-name lines even when corruption is disabled.
					if path == "/v1/messages" && stream {
						var lines []string
						for _, line := range strings.Split(healthy, "\n") {
							if !strings.HasPrefix(line, "event:") {
								lines = append(lines, line)
							}
						}
						healthy = strings.Join(lines, "\n")
					}
					if status != 200 || string(body) != healthy || headers.Get(middleware.CorruptionHeader) != "" {
						t.Fatalf("tool fault injected without declared tools: %s", body)
					}
				})
			}
		}
	}
}

func TestResponseCorruptionIntervalAcrossEndpoints(t *testing.T) {
	_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 3, Modes: []string{"xml_tool_name"}}, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model  string        `json:"model"`
			Stream bool          `json:"stream"`
			Tools  []interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Model == "upstream-error" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
			return
		}
		kind := "application/json"
		if request.Stream {
			kind = "text/event-stream"
		}
		w.Header().Set("Content-Type", kind)
		body := healthyFaultResponse(r.URL.Path, request.Stream)
		if len(request.Tools) > 0 {
			body = healthyToolFaultResponse(r.URL.Path, request.Stream)
		}
		_, _ = io.WriteString(w, body)
	})
	send := func(path string, stream, tools bool, want string) {
		t.Helper()
		headers, body, status := sendFaultHTTP(t, server, path, responseFaultRequest(path, stream, tools))
		if status != 200 || headers.Get(middleware.CorruptionHeader) != want {
			t.Fatalf("%s: want mode %q got %q status %d", path, want, headers.Get(middleware.CorruptionHeader), status)
		}
		if want == "xml_tool_name" {
			assertXMLToolFault(t, body, stream)
		}
	}
	send("/v1/chat/completions", false, true, "") // eligible 1
	response, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	bad := bytes.Replace(responseFaultRequest("/v1/chat/completions", false, true), []byte("client-model"), []byte("upstream-error"), 1)
	headers, _, status := sendFaultHTTP(t, server, "/v1/chat/completions", bad)
	if status != 429 || headers.Get(middleware.CorruptionHeader) != "" {
		t.Fatal("upstream error should pass through")
	}
	send("/v1/chat/completions", true, false, "")             // no applicable mode, does not count
	send("/v1/messages", true, true, "")                      // eligible 2
	send("/v1/responses", true, true, "xml_tool_name")        // eligible 3
	send("/v1/messages", false, true, "")                     // eligible 4
	send("/v1/responses", false, true, "")                    // eligible 5
	send("/v1/chat/completions", true, true, "xml_tool_name") // eligible 6
}

// Real tool calls rather than a text-only response: field faults may no longer
// invent a tool invocation, IDs or usage when the provider did not return them.
func healthyToolFaultResponse(path string, stream bool) string {
	var message map[string]interface{}
	switch path {
	case "/v1/messages":
		message = map[string]interface{}{"id": "up_1", "type": "message", "role": "assistant", "model": "remote-model", "content": []interface{}{map[string]interface{}{"type": "tool_use", "id": "call_remote", "name": "test_tool", "input": map[string]interface{}{"value": "original"}}}, "stop_reason": "tool_use", "usage": map[string]int{"input_tokens": 11, "output_tokens": 7}}
	case "/v1/responses":
		message = map[string]interface{}{"id": "up_1", "object": "response", "created_at": 123, "model": "remote-model", "status": "completed", "output": []interface{}{map[string]interface{}{"id": "fc_remote", "type": "function_call", "call_id": "call_remote", "name": "test_tool", "arguments": "{\"value\":\"original\"}", "status": "completed"}}, "usage": map[string]int{"input_tokens": 11, "output_tokens": 7}}
	default:
		message = map[string]interface{}{"id": "up_1", "object": "chat.completion", "created": 123, "system_fingerprint": "fp_remote", "model": "remote-model", "choices": []interface{}{map[string]interface{}{"index": 0, "finish_reason": "tool_calls", "message": map[string]interface{}{"role": "assistant", "tool_calls": []interface{}{map[string]interface{}{"id": "call_remote", "type": "function", "function": map[string]string{"name": "test_tool", "arguments": "{\"value\":\"original\"}"}}}}}}, "usage": map[string]int{"prompt_tokens": 11, "completion_tokens": 7}}
	}
	message["vendor_meta"] = map[string]interface{}{"opaque": "keep", "large_integer": json.Number("9007199254740993")}
	raw, _ := json.Marshal(message)
	if !stream {
		return string(raw)
	}
	var out strings.Builder
	emit := func(v interface{}) { raw, _ := json.Marshal(v); out.WriteString("data: " + string(raw) + "\n\n") }
	switch path {
	case "/v1/messages":
		emit(map[string]interface{}{"type": "message_start", "message": message})
		// Put the tool into start/delta/stop events, not the initial snapshot.
		out.Reset()
		start := map[string]interface{}{}
		for k, v := range message {
			start[k] = v
		}
		start["content"] = []interface{}{}
		start["stop_reason"] = nil
		emit(map[string]interface{}{"type": "message_start", "message": start})
		emit(map[string]interface{}{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{"type": "tool_use", "id": "call_remote", "name": "test_tool", "input": map[string]interface{}{}}})
		emit(map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": "{\"value\":\"original\"}"}})
		emit(map[string]interface{}{"type": "content_block_stop", "index": 0})
		emit(map[string]interface{}{"type": "message_delta", "delta": map[string]string{"stop_reason": "tool_use"}, "usage": map[string]int{"output_tokens": 7}})
		emit(map[string]interface{}{"type": "message_stop"})
	case "/v1/responses":
		item := message["output"].([]interface{})[0]
		emit(map[string]interface{}{"type": "response.output_item.added", "sequence_number": 0, "output_index": 0, "item": item})
		emit(map[string]interface{}{"type": "response.function_call_arguments.delta", "sequence_number": 1, "output_index": 0, "item_id": "fc_remote", "delta": "{\"value\":\"original\"}"})
		emit(map[string]interface{}{"type": "response.function_call_arguments.done", "sequence_number": 2, "output_index": 0, "item_id": "fc_remote", "arguments": "{\"value\":\"original\"}"})
		emit(map[string]interface{}{"type": "response.output_item.done", "sequence_number": 3, "output_index": 0, "item": item})
		emit(map[string]interface{}{"type": "response.completed", "sequence_number": 4, "response": message})
	default:
		chunk := func(delta interface{}, finish interface{}) {
			v := map[string]interface{}{}
			for k, val := range message {
				v[k] = val
			}
			delete(v, "usage")
			v["object"] = "chat.completion.chunk"
			v["choices"] = []interface{}{map[string]interface{}{"index": 0, "delta": delta, "finish_reason": finish}}
			emit(v)
		}
		chunk(map[string]string{"role": "assistant"}, nil)
		chunk(map[string]interface{}{"tool_calls": []interface{}{map[string]interface{}{"index": 0, "id": "call_remote", "type": "function", "function": map[string]string{"name": "test_tool", "arguments": ""}}}}, nil)
		chunk(map[string]interface{}{"tool_calls": []interface{}{map[string]interface{}{"index": 0, "type": "function", "function": map[string]string{"arguments": "{\"value\":\"original\"}"}}}}, nil)
		chunk(map[string]interface{}{}, "tool_calls")
		emit(map[string]interface{}{"id": "up_1", "choices": []interface{}{}, "usage": message["usage"]})
		out.WriteString("data: [DONE]\n\n")
	}
	return out.String()
}

func TestTruncatedToolArgumentsAtDownstreamBoundary(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", path, stream), func(t *testing.T) {
				original := responseFaultRequest(path, stream, true)
				_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 1, Modes: []string{"truncated_tool_arguments"}}, func(w http.ResponseWriter, r *http.Request) {
					got, _ := io.ReadAll(r.Body)
					if !bytes.Equal(original, got) {
						t.Error("changed outgoing request")
					}
					kind := "application/json"
					if stream {
						kind = "text/event-stream"
					}
					w.Header().Set("Content-Type", kind)
					io.WriteString(w, healthyToolFaultResponse(path, stream))
				})
				h, b, status := sendFaultHTTP(t, server, path, original)
				if status != 200 || h.Get(middleware.CorruptionHeader) != "truncated_tool_arguments" {
					t.Fatalf("not delivered: %d %s", status, b)
				}
				for _, keep := range []string{"up_1", "call_remote", "remote-model", "9007199254740993", "original"} {
					if !bytes.Contains(b, []byte(keep)) {
						t.Fatalf("lost %s: %s", keep, b)
					}
				}
				terminal := "length"
				if path == "/v1/messages" {
					terminal = "max_tokens"
				}
				if path == "/v1/responses" {
					terminal = "max_output_tokens"
				}
				if !bytes.Contains(b, []byte(terminal)) {
					t.Fatalf("missing length terminal: %s", b)
				}
				if stream && path == "/v1/chat/completions" && !bytes.Contains(b, []byte("[DONE]")) {
					t.Fatal("truncated SSE instead of arguments")
				}
			})
		}
	}
}

func TestReasoningFaultDeliveredBeforeUpstreamFinishesAndClientCanCancel(t *testing.T) {
	cancelled := make(chan struct{})
	_, server := faultApplication(t, config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 1, Modes: []string{"reasoning_repetition"}}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"remote_live\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"healthy reasoning\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			close(cancelled)
		case <-time.After(3 * time.Second):
			t.Error("downstream cancellation did not reach provider")
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat/completions", bytes.NewReader(responseFaultRequest("/v1/chat/completions", true, false)))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "我需要重新检查上一轮的执行结果。") || !strings.Contains(line, "remote_live") {
		t.Fatalf("not a live reasoning fault: %s", line)
	}
	cancel()
	resp.Body.Close()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("provider not cancelled")
	}
}
