package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/model"
	"github.com/seifghazi/claude-code-monitor/internal/service"
)

func TestCopyOpenAIResponseStoresRawAndStructuredStream(t *testing.T) {
	storage, err := service.NewSQLiteStorageService(&config.StorageConfig{
		DBPath: t.TempDir() + "/requests.db",
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	handler := &Handler{
		storageService: storage,
		logger:         log.New(io.Discard, "", 0),
		options:        Options{MaxCaptureBytes: 1024 * 1024},
	}
	requestLog := savedOpenAIRequest(t, storage)
	raw := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\"}}\n\n"
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(raw)),
	}
	recorder := httptest.NewRecorder()

	handler.copyOpenAIResponse(
		recorder,
		upstream,
		requestLog,
		time.Now(),
		true,
		openAIProtocolResponses,
	)

	if recorder.Body.String() != raw {
		t.Fatalf("client response changed: %q", recorder.Body.String())
	}
	if requestLog.Response.BodyText != raw {
		t.Fatal("raw SSE was not retained")
	}
	if !strings.Contains(string(requestLog.Response.Body), `"status":"completed"`) {
		t.Fatalf("structured response missing: %s", requestLog.Response.Body)
	}
	if requestLog.Response.StreamError != "" {
		t.Fatalf("unexpected stream error: %s", requestLog.Response.StreamError)
	}
}

func TestCopyOpenAIResponsePersistsFirstTokenTiming(t *testing.T) {
	storage, err := service.NewSQLiteStorageService(&config.StorageConfig{DBPath: t.TempDir() + "/requests.db"})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	defer storage.Close()
	handler := &Handler{storageService: storage, logger: log.New(io.Discard, "", 0)}
	requestLog := savedOpenAIRequest(t, storage)
	raw := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	upstream := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(raw))}
	handler.copyOpenAIResponse(httptest.NewRecorder(), upstream, requestLog, time.Now().Add(-time.Second), true, openAIProtocolChat)
	stored, _, err := storage.GetRequestByShortID(requestLog.RequestID)
	if err != nil {
		t.Fatalf("read persisted request: %v", err)
	}
	if stored.Response == nil || stored.Response.TTFTMs == nil || *stored.Response.TTFTMs < 900 || stored.Response.FirstTokenAt == "" {
		t.Fatalf("missing persisted first token timing: %+v", stored.Response)
	}
}

func TestCopyOpenAIResponseMarksTruncatedCaptureWithoutPartialResult(t *testing.T) {
	storage, err := service.NewSQLiteStorageService(&config.StorageConfig{
		DBPath: t.TempDir() + "/requests.db",
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	handler := &Handler{
		storageService: storage,
		logger:         log.New(io.Discard, "", 0),
		options:        Options{MaxCaptureBytes: 24},
	}
	requestLog := savedOpenAIRequest(t, storage)
	raw := "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(raw)),
	}

	handler.copyOpenAIResponse(
		httptest.NewRecorder(),
		upstream,
		requestLog,
		time.Now(),
		true,
		openAIProtocolResponses,
	)

	if !requestLog.Response.Truncated || requestLog.Response.CapturedBytes != 24 {
		t.Fatalf("capture was not marked truncated: %+v", requestLog.Response)
	}
	if len(requestLog.Response.Body) != 0 {
		t.Fatalf("truncated stream must not publish partial structured output: %s", requestLog.Response.Body)
	}
	if requestLog.Response.StreamError == "" {
		t.Fatal("expected a visible truncation error")
	}
}

func TestCaptureHeadersRedactsAuthorizationAndCookies(t *testing.T) {
	headers := http.Header{
		"Authorization": []string{"Bearer visible-token"},
		"Cookie":        []string{"session=visible"},
		"X-Trace-Id":    []string{"trace-123"},
	}
	captured := CaptureHeaders(headers)
	if captured.Get("Authorization") != "[REDACTED]" {
		t.Fatal("authorization header was not redacted")
	}
	if captured.Get("Cookie") != "[REDACTED]" {
		t.Fatal("cookie header was not redacted")
	}
	if captured.Get("X-Trace-Id") != "trace-123" {
		t.Fatal("non-sensitive header was changed")
	}
	captured.Set("Authorization", "changed")
	if headers.Get("Authorization") != "Bearer visible-token" {
		t.Fatal("captured headers must not alias the original map")
	}
}

func TestEnrichOpenAIRequestParsesHistoricalStreamWithoutWriting(t *testing.T) {
	request := &model.RequestLog{
		Endpoint: "/v1/chat/completions",
		Response: &model.ResponseLog{
			IsStreaming: true,
			BodyText: "data: {\"id\":\"chat-old\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"old\"},\"finish_reason\":\"stop\"}]}\n\n" +
				"data: [DONE]\n\n",
		},
	}
	(&Handler{}).enrichOpenAIRequestForDisplay(request)
	if request.Protocol != openAIProtocolChat {
		t.Fatalf("protocol was not derived: %s", request.Protocol)
	}
	if !strings.Contains(string(request.Response.Body), `"content":"old"`) {
		t.Fatalf("historical stream was not parsed: %s", request.Response.Body)
	}
}

func TestHandleAnthropicStreamingResponseStoresToolUse(t *testing.T) {
	storage, err := service.NewSQLiteStorageService(&config.StorageConfig{
		DBPath: t.TempDir() + "/requests.db",
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	handler := &Handler{
		storageService: storage,
		logger:         log.New(io.Discard, "", 0),
	}
	requestLog := savedOpenAIRequest(t, storage)
	raw := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_mock","type":"message","role":"assistant","model":"gpt-mock","content":[],"stop_reason":null,"stop_sequence":null,"usage":{}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_weather_1","name":"get_weather","input":{}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"Hangzhou\"}"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":20,"output_tokens":10}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n\n")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(raw)),
	}

	handler.handleStreamingResponse(
		httptest.NewRecorder(),
		upstream,
		requestLog,
		time.Now(),
	)

	if requestLog.Response.StreamError != "" {
		t.Fatalf("unexpected stream error: %s", requestLog.Response.StreamError)
	}
	if requestLog.Response.TTFTMs == nil || requestLog.Response.FirstTokenAt == "" {
		t.Fatal("tool use did not record first token timing")
	}
	var body map[string]interface{}
	if err := json.Unmarshal(requestLog.Response.Body, &body); err != nil {
		t.Fatalf("unmarshal stored response: %v", err)
	}
	if body["stop_reason"] != "tool_use" {
		t.Fatalf("stop reason was not retained: %#v", body["stop_reason"])
	}
	content, ok := body["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("tool content missing: %#v", body["content"])
	}
	toolUse := content[0].(map[string]interface{})
	input := toolUse["input"].(map[string]interface{})
	if toolUse["type"] != "tool_use" ||
		toolUse["id"] != "call_weather_1" ||
		toolUse["name"] != "get_weather" ||
		input["city"] != "Hangzhou" {
		t.Fatalf("tool use was not aggregated: %#v", toolUse)
	}
}

func TestHandleAnthropicStreamingResponseStoresThinkingAndText(t *testing.T) {
	storage, err := service.NewSQLiteStorageService(&config.StorageConfig{
		DBPath: t.TempDir() + "/requests.db",
	})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	handler := &Handler{
		storageService: storage,
		logger:         log.New(io.Discard, "", 0),
	}
	requestLog := savedOpenAIRequest(t, storage)
	raw := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_thinking","type":"message","role":"assistant","model":"claude-test","content":[],"stop_reason":null,"usage":{"input_tokens":12}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reasoning text"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"answer text"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":10}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n\n")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(raw)),
	}

	handler.handleStreamingResponse(
		httptest.NewRecorder(),
		upstream,
		requestLog,
		time.Now(),
	)

	if requestLog.Response.StreamError != "" {
		t.Fatalf("unexpected stream error: %s", requestLog.Response.StreamError)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(requestLog.Response.Body, &body); err != nil {
		t.Fatalf("unmarshal stored response: %v", err)
	}
	content, ok := body["content"].([]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("thinking and text content missing: %#v", body["content"])
	}
	thinking := content[0].(map[string]interface{})
	if thinking["type"] != "thinking" || thinking["thinking"] != "reasoning text" || thinking["signature"] != "signed" {
		t.Fatalf("unexpected thinking block: %#v", thinking)
	}
	text := content[1].(map[string]interface{})
	if text["type"] != "text" || text["text"] != "answer text" {
		t.Fatalf("unexpected text block: %#v", text)
	}
	usage := body["usage"].(map[string]interface{})
	if usage["input_tokens"] != float64(12) || usage["output_tokens"] != float64(10) {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}

func savedOpenAIRequest(t *testing.T, storage service.StorageService) *model.RequestLog {
	t.Helper()
	request := &model.RequestLog{
		RequestID: "request-1",
		Timestamp: time.Now().Format(time.RFC3339),
		Method:    http.MethodPost,
		Endpoint:  "/v1/responses",
		Headers:   http.Header{},
		Body:      map[string]interface{}{"model": "gpt-5"},
	}
	if _, err := storage.SaveRequest(request); err != nil {
		t.Fatalf("save request: %v", err)
	}
	return request
}

type stubModelsForwarder struct {
	calls int
	resp  *http.Response
	err   error
}

func (s *stubModelsForwarder) ForwardModels(context.Context, *http.Request) (*http.Response, error) {
	s.calls++
	return s.resp, s.err
}

func (s *stubModelsForwarder) ForwardChatCompletions(context.Context, *http.Request) (*http.Response, error) {
	panic("unexpected chat completions request")
}

func (s *stubModelsForwarder) ForwardResponses(context.Context, *http.Request) (*http.Response, error) {
	panic("unexpected responses request")
}

func TestModelsForwardsAndCaptures(t *testing.T) {
	for _, route := range []struct {
		name     string
		headers  http.Header
		protocol string
	}{
		{"no headers", http.Header{}, openAIProtocolModels},
		{"bearer", http.Header{"Authorization": {"Bearer client-key"}}, openAIProtocolModels},
		{"version", http.Header{"Anthropic-Version": {"2023-06-01"}}, anthropicProtocolModels},
		{"api key", http.Header{"X-Api-Key": {"client-key"}}, anthropicProtocolModels},
		{"both with bearer", http.Header{"Anthropic-Version": {"2023-06-01"}, "X-Api-Key": {"client-key"}, "Authorization": {"Bearer client-key"}}, anthropicProtocolModels},
		{"empty api key", http.Header{"X-Api-Key": {""}}, anthropicProtocolModels},
		{"empty version", http.Header{"Anthropic-Version": {""}}, anthropicProtocolModels},
	} {
		t.Run(route.name, func(t *testing.T) {
			for _, tc := range []struct {
				name        string
				status      int
				body        string
				contentType string
				err         error
			}{
				{
					name: "success", status: http.StatusOK, contentType: "application/json",
					body: "{\n  \"object\": \"list\", \"data\": [{\"id\":\"model-1\",\"object\":\"model\"}]\n}\n",
				},
				{
					name: "not found", status: http.StatusNotFound, contentType: "application/json",
					body: `{"error":{"message":"models unavailable","type":"not_found"}}`,
				},
				{
					name: "unauthorized", status: http.StatusUnauthorized, contentType: "application/json",
					body: `{"error":{"message":"invalid API key","type":"authentication_error"}}`,
				},
				{
					name: "non-JSON error", status: http.StatusServiceUnavailable, contentType: "text/plain",
					body: "upstream unavailable\n",
				},
				{
					name: "connection failure", status: http.StatusBadGateway,
					err: errors.New("connection refused"),
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					storage, err := service.NewSQLiteStorageService(&config.StorageConfig{
						DBPath: t.TempDir() + "/requests.db",
					})
					if err != nil {
						t.Fatalf("create storage: %v", err)
					}
					t.Cleanup(func() { _ = storage.Close() })
					forwarder := &stubModelsForwarder{err: tc.err}
					if tc.err == nil {
						forwarder.resp = &http.Response{
							StatusCode: tc.status,
							Header: http.Header{
								"Content-Type": []string{tc.contentType},
								"X-Request-Id": []string{"upstream-1"},
							},
							Body: io.NopCloser(strings.NewReader(tc.body)),
						}
					}
					unused := &stubModelsForwarder{err: errors.New("wrong provider selected")}
					openAI, anthropic := unused, forwarder
					if route.protocol == openAIProtocolModels {
						openAI, anthropic = forwarder, unused
					}
					h := New(storage, log.New(io.Discard, "", 0), nil, openAI, anthropic, Options{MaxCaptureBytes: 1024 * 1024, OpenAIConfigured: true, AnthropicConfigured: true})
					request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
					request.Header = route.headers.Clone()
					request.Header.Set("User-Agent", "models-test")
					recorder := httptest.NewRecorder()
					h.Models(recorder, request)
					if forwarder.calls != 1 || unused.calls != 0 {
						t.Fatalf("unexpected provider calls: selected=%d unused=%d", forwarder.calls, unused.calls)
					}
					if recorder.Code != tc.status {
						t.Fatalf("status = %d, want %d", recorder.Code, tc.status)
					}
					if tc.err == nil {
						if recorder.Body.String() != tc.body {
							t.Fatalf("client response changed: %q", recorder.Body.String())
						}
						if recorder.Header().Get("Content-Type") != tc.contentType || recorder.Header().Get("X-Request-Id") != "upstream-1" {
							t.Fatal("upstream response headers were not preserved")
						}
					}
					requests, err := storage.GetAllRequests("all", "", "")
					if err != nil {
						t.Fatalf("read captured requests: %v", err)
					}
					if len(requests) != 1 {
						t.Fatalf("captured %d requests, want 1", len(requests))
					}
					stored := requests[0]
					// Protocol is derived for display rather than persisted in SQLite.
					h.enrichOpenAIRequestForDisplay(stored)
					if stored.Method != http.MethodGet || stored.Endpoint != "/v1/models" || stored.Protocol != route.protocol {
						t.Fatalf("unexpected request metadata: %+v", stored)
					}
					if stored.Body != nil || stored.Model != "" || stored.OriginalModel != "" || stored.RoutedModel != "" {
						t.Fatalf("unexpected request body/model: %+v", stored)
					}
					if stored.UserAgent != "models-test" {
						t.Fatal("request headers were not captured and redacted")
					}
					for _, name := range []string{"Authorization", "X-Api-Key"} {
						if _, present := request.Header[name]; present && http.Header(stored.Headers).Get(name) != "[REDACTED]" {
							t.Fatalf("%s was not redacted", name)
						}
					}
					if stored.RequestID == "" || stored.Timestamp == "" {
						t.Fatal("missing request ID or timestamp")
					}
					response := stored.Response
					if response == nil || response.StatusCode != tc.status || response.IsStreaming || response.CompletedAt == "" {
						t.Fatalf("unexpected stored response: %+v", response)
					}
					if tc.err != nil {
						for _, body := range [][]byte{recorder.Body.Bytes(), response.Body} {
							var payload struct {
								Error struct {
									Message string `json:"message"`
									Type    string `json:"type"`
									Code    string `json:"code"`
								} `json:"error"`
							}
							if err := json.Unmarshal(body, &payload); err != nil {
								t.Fatalf("decode connection error: %v", err)
							}
							if payload.Error.Message != "Failed to connect to upstream" || payload.Error.Type != "proxy_error" || payload.Error.Code != "upstream_connection_failed" {
								t.Fatalf("unexpected connection error: %s", body)
							}
						}
					} else if json.Valid([]byte(tc.body)) {
						// SQLite serializes RawMessage, so compare JSON without whitespace.
						var got, want interface{}
						if err := json.Unmarshal(response.Body, &got); err != nil {
							t.Fatalf("decode stored JSON: %v", err)
						}
						if err := json.Unmarshal([]byte(tc.body), &want); err != nil {
							t.Fatal(err)
						}
						gotJSON, _ := json.Marshal(got)
						wantJSON, _ := json.Marshal(want)
						if string(gotJSON) != string(wantJSON) {
							t.Fatalf("stored body changed: %s", response.Body)
						}
					} else if response.BodyText != tc.body {
						t.Fatalf("stored text changed: %q", response.BodyText)
					}
					if tc.status == http.StatusOK {
						var models model.ModelsResponse
						if err := json.Unmarshal(recorder.Body.Bytes(), &models); err != nil {
							t.Fatalf("decode models: %v", err)
						}
						if models.Object != "list" || len(models.Data) != 1 || models.Data[0].ID != "model-1" {
							t.Fatalf("unexpected models: %+v", models)
						}
					}
				})
			}
		})
	}
}

type modelsCaptureStorage struct {
	service.StorageService
	protocol string
}

func (s *modelsCaptureStorage) SaveRequest(request *model.RequestLog) (string, error) {
	s.protocol = request.Protocol
	return s.StorageService.SaveRequest(request)
}

func TestModelsConfigurationFallback(t *testing.T) {
	for _, tc := range []struct {
		name                string
		anthropicHeader     bool
		openAIConfigured    bool
		anthropicConfigured bool
		want                string
		fallback            bool
	}{
		{"openai falls back", false, false, true, anthropicProtocolModels, true},
		{"anthropic falls back", true, true, false, openAIProtocolModels, true},
		{"both configured openai", false, true, true, openAIProtocolModels, false},
		{"both configured anthropic", true, true, true, anthropicProtocolModels, false},
		{"neither configured openai", false, false, false, openAIProtocolModels, false},
		{"neither configured anthropic", true, false, false, anthropicProtocolModels, false},
		{"only openai preferred", false, true, false, openAIProtocolModels, false},
		{"only anthropic preferred", true, false, true, anthropicProtocolModels, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := service.NewSQLiteStorageService(&config.StorageConfig{DBPath: t.TempDir() + "/requests.db"})
			if err != nil {
				t.Fatalf("create storage: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			storage := &modelsCaptureStorage{StorageService: db}
			const body = `{"data":[{"id":"model-1"}]}`
			selected := &stubModelsForwarder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}}
			unused := &stubModelsForwarder{err: errors.New("unexpected provider")}
			openAI, anthropic := selected, unused
			if tc.want == anthropicProtocolModels {
				openAI, anthropic = unused, selected
			}
			var logs strings.Builder
			h := New(storage, log.New(&logs, "", 0), nil, openAI, anthropic, Options{
				OpenAIConfigured: tc.openAIConfigured, AnthropicConfigured: tc.anthropicConfigured,
			})
			request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			request.Header.Set("Authorization", "Bearer client-key")
			if tc.anthropicHeader {
				request.Header.Set("X-Api-Key", "client-key")
			}
			recorder := httptest.NewRecorder()
			h.Models(recorder, request)
			if selected.calls != 1 || unused.calls != 0 || recorder.Code != http.StatusOK || recorder.Body.String() != body {
				t.Fatalf("unexpected forwarding: selected=%d unused=%d status=%d body=%s", selected.calls, unused.calls, recorder.Code, recorder.Body.String())
			}
			if storage.protocol != tc.want {
				t.Fatalf("captured protocol = %q, want %q", storage.protocol, tc.want)
			}
			if strings.Contains(logs.String(), "Models provider fallback:") != tc.fallback {
				t.Fatalf("unexpected fallback log: %s", logs.String())
			}
			requests, err := db.GetAllRequests("all", "", "")
			if err != nil || len(requests) != 1 {
				t.Fatalf("read captured request: count=%d err=%v", len(requests), err)
			}
			stored := requests[0]
			h.enrichOpenAIRequestForDisplay(stored)
			if stored.Protocol != storage.protocol || stored.Response == nil || string(stored.Response.Body) != body {
				t.Fatalf("capture/display mismatch: %+v", stored)
			}
			// Check the actual history endpoint also uses this handler's options.
			history := httptest.NewRecorder()
			h.GetRequests(history, httptest.NewRequest(http.MethodGet, "/api/requests", nil))
			var page struct {
				Requests []model.RequestLog `json:"requests"`
			}
			if err := json.Unmarshal(history.Body.Bytes(), &page); err != nil {
				t.Fatalf("decode history: %v", err)
			}
			if len(page.Requests) != 1 || page.Requests[0].Protocol != tc.want {
				t.Fatalf("unexpected history protocol: %s", history.Body.String())
			}
		})
	}
}
