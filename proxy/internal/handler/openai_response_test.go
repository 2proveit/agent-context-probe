package handler

import (
	"encoding/json"
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

func TestCaptureHeadersRetainsAuthorizationAndCookies(t *testing.T) {
	headers := http.Header{
		"Authorization": []string{"Bearer visible-token"},
		"Cookie":        []string{"session=visible"},
	}
	captured := CaptureHeaders(headers)
	if captured.Get("Authorization") != "Bearer visible-token" {
		t.Fatal("authorization header was changed")
	}
	if captured.Get("Cookie") != "session=visible" {
		t.Fatal("cookie header was changed")
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
