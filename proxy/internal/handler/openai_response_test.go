package handler

import (
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
