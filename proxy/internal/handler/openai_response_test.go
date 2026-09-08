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
	enrichOpenAIRequestForDisplay(request)
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
	resp *http.Response
	err  error
}

func (s *stubModelsForwarder) ForwardModels(context.Context, *http.Request) (*http.Response, error) {
	return s.resp, s.err
}

func (s *stubModelsForwarder) ForwardChatCompletions(context.Context, *http.Request) (*http.Response, error) {
	panic("unexpected chat completions request")
}

func (s *stubModelsForwarder) ForwardResponses(context.Context, *http.Request) (*http.Response, error) {
	panic("unexpected responses request")
}

func TestModelsForwardsAndCaptures(t *testing.T) {
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
			h := &Handler{
				storageService: storage,
				openAIProvider: forwarder,
				logger:         log.New(io.Discard, "", 0),
				options:        Options{MaxCaptureBytes: 1024 * 1024},
			}
			request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			request.Header.Set("User-Agent", "models-test")
			request.Header.Set("Authorization", "Bearer client-key")
			recorder := httptest.NewRecorder()
			h.Models(recorder, request)
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
			enrichOpenAIRequestForDisplay(stored)
			if stored.Method != http.MethodGet || stored.Endpoint != "/v1/models" || stored.Protocol != openAIProtocolModels {
				t.Fatalf("unexpected request metadata: %+v", stored)
			}
			if stored.Body != nil || stored.Model != "" || stored.OriginalModel != "" || stored.RoutedModel != "" {
				t.Fatalf("unexpected request body/model: %+v", stored)
			}
			if stored.UserAgent != "models-test" || http.Header(stored.Headers).Get("Authorization") != "[REDACTED]" {
				t.Fatal("request headers were not captured and redacted")
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
}
