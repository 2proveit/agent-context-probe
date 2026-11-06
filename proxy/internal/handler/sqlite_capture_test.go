package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/model"
	"github.com/seifghazi/claude-code-monitor/internal/provider"
	"github.com/seifghazi/claude-code-monitor/internal/service"
)

type sqliteCaptureFixture struct {
	endpoint string
	model    string
	stream   bool
	raw      string
	finish   string
}

func sqliteCaptureFixtures() []sqliteCaptureFixture {
	return []sqliteCaptureFixture{
		{"/v1/messages", "claude-test", false, `{"id":"msg_test","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`, `"stop_reason":"end_turn"`},
		{"/v1/messages", "claude-test", true, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":2}}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"answer\"}}\n\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n", `"stop_reason":"end_turn"`},
		{"/v1/chat/completions", "gpt-test", false, `{"id":"chat_test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}]}`, `"finish_reason":"stop"`},
		{"/v1/chat/completions", "gpt-test", true, "data: {\"id\":\"chat_test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n", `"finish_reason":"stop"`},
		{"/v1/responses", "gpt-test", false, `{"id":"resp_test","object":"response","status":"completed","output":[]}`, `"status":"completed"`},
		{"/v1/responses", "gpt-test", true, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"object\":\"response\",\"status\":\"completed\",\"output\":[]}}\n\n", `"status":"completed"`},
	}
}

func TestSQLiteConcurrentProtocolCaptures(t *testing.T) {
	storage, err := service.NewSQLiteStorageService(&config.StorageConfig{DBPath: filepath.Join(t.TempDir(), "requests.db")})
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	fixtures := sqliteCaptureFixtures()
	forwarder := &sqliteCaptureForwarder{fixtures: fixtures}
	h := newSQLiteCaptureHandler(storage, forwarder)

	const repetitions = 3
	recorders := make([]*httptest.ResponseRecorder, len(fixtures)*repetitions)
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := range recorders {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			fixture := fixtures[i%len(fixtures)]
			req := sqliteCaptureRequest(fixture)
			req.Header.Set("X-Test-Case", fmt.Sprint(i))
			recorder := httptest.NewRecorder()
			recorders[i] = recorder
			serveSQLiteCapture(h, recorder, req)
		}(i)
	}
	close(start)
	workers.Wait()
	for i, recorder := range recorders {
		if recorder.Code != http.StatusOK || recorder.Body.String() != fixtures[i%len(fixtures)].raw {
			t.Fatalf("response %d changed: status=%d body=%q", i, recorder.Code, recorder.Body.String())
		}
	}
	stored, err := storage.GetAllRequests("all", "", "")
	if err != nil {
		t.Fatalf("read captures: %v", err)
	}
	if len(stored) != len(recorders) {
		t.Fatalf("captured %d requests, want %d", len(stored), len(recorders))
	}
	seen := make(map[string]bool)
	for _, request := range stored {
		key := http.Header(request.Headers).Get("X-Test-Case")
		if seen[key] {
			t.Fatalf("duplicate capture for case %s", key)
		}
		seen[key] = true
		var index int
		if _, err := fmt.Sscan(key, &index); err != nil || index < 0 || index >= len(recorders) {
			t.Fatalf("invalid capture case %q", key)
		}
		fixture := fixtures[index%len(fixtures)]
		response := request.Response
		if response == nil || response.StatusCode != http.StatusOK || response.IsStreaming != fixture.stream || response.StreamError != "" || response.Truncated || response.CompletedAt == "" {
			t.Fatalf("case %s has incomplete capture: %+v", key, response)
		}
		if !strings.Contains(string(response.Body), fixture.finish) {
			t.Fatalf("case %s lost finish metadata: %s", key, response.Body)
		}
		if fixture.stream {
			raw := response.BodyText
			if fixture.endpoint == "/v1/messages" {
				raw = strings.Join(response.StreamingChunks, "\n\n") + "\n\n"
			}
			if raw != fixture.raw {
				t.Fatalf("case %s lost raw stream: %q", key, raw)
			}
		} else if string(response.Body) != fixture.raw {
			t.Fatalf("case %s changed captured JSON: %s", key, response.Body)
		}
	}
}

func TestSQLiteCapturesCanceledUpstreamStreams(t *testing.T) {
	for _, fixture := range sqliteCaptureFixtures() {
		if !fixture.stream {
			continue
		}
		t.Run(fixture.endpoint, func(t *testing.T) {
			storage, err := service.NewSQLiteStorageService(&config.StorageConfig{DBPath: filepath.Join(t.TempDir(), "requests.db")})
			if err != nil {
				t.Fatalf("create storage: %v", err)
			}
			t.Cleanup(func() { _ = storage.Close() })
			// Cancel after a nonterminal event has been delivered, before the
			// upstream body supplies a protocol completion event.
			switch fixture.endpoint {
			case "/v1/messages":
				fixture.raw = "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n"
			case "/v1/chat/completions":
				fixture.raw = "data: {\"id\":\"chat_test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"
			case "/v1/responses":
				fixture.raw = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
			}
			forwarder := &sqliteCaptureForwarder{fixtures: []sqliteCaptureFixture{fixture}, cancelStream: true}
			h := newSQLiteCaptureHandler(storage, forwarder)
			recorder := httptest.NewRecorder()
			serveSQLiteCapture(h, recorder, sqliteCaptureRequest(fixture))
			if recorder.Body.String() != fixture.raw {
				t.Fatalf("partial stream changed: %q", recorder.Body.String())
			}
			stored, err := storage.GetAllRequests("all", "", "")
			if err != nil || len(stored) != 1 {
				t.Fatalf("read interrupted capture: count=%d err=%v", len(stored), err)
			}
			response := stored[0].Response
			if response == nil || response.StreamError != "upstream response stream was interrupted" || len(response.Body) != 0 || !response.IsStreaming || response.CompletedAt == "" {
				t.Fatalf("cancellation metadata missing: %+v", response)
			}
			raw := response.BodyText
			if fixture.endpoint == "/v1/messages" {
				raw = strings.Join(response.StreamingChunks, "\n\n") + "\n\n"
			}
			if raw != fixture.raw {
				t.Fatalf("interrupted capture lost raw stream: %q", raw)
			}
		})
	}
}

func newSQLiteCaptureHandler(storage service.StorageService, forwarder *sqliteCaptureForwarder) *Handler {
	logger := log.New(io.Discard, "", 0)
	router := service.NewModelRouter(&config.Config{}, map[string]provider.Provider{"anthropic": forwarder}, logger)
	return New(storage, logger, router, forwarder, nil, Options{})
}

func sqliteCaptureRequest(fixture sqliteCaptureFixture) *http.Request {
	body, _ := json.Marshal(map[string]interface{}{"model": fixture.model, "stream": fixture.stream, "max_tokens": 10, "messages": []interface{}{}})
	req := httptest.NewRequest(http.MethodPost, fixture.endpoint, strings.NewReader(string(body)))
	return req.WithContext(context.WithValue(req.Context(), model.BodyBytesKey, body))
}

func serveSQLiteCapture(h *Handler, recorder *httptest.ResponseRecorder, req *http.Request) {
	switch req.URL.Path {
	case "/v1/messages":
		h.Messages(recorder, req)
	case "/v1/chat/completions":
		h.ChatCompletions(recorder, req)
	case "/v1/responses":
		h.Responses(recorder, req)
	}
}

type sqliteCaptureForwarder struct {
	fixtures     []sqliteCaptureFixture
	cancelStream bool
}

func (f *sqliteCaptureForwarder) Name() string { return "anthropic" }
func (f *sqliteCaptureForwarder) ForwardRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
	return f.forward(ctx, req)
}
func (f *sqliteCaptureForwarder) ForwardChatCompletions(ctx context.Context, req *http.Request) (*http.Response, error) {
	return f.forward(ctx, req)
}
func (f *sqliteCaptureForwarder) ForwardResponses(ctx context.Context, req *http.Request) (*http.Response, error) {
	return f.forward(ctx, req)
}
func (f *sqliteCaptureForwarder) ForwardModels(context.Context, *http.Request) (*http.Response, error) {
	panic("unexpected models request")
}
func (f *sqliteCaptureForwarder) forward(ctx context.Context, req *http.Request) (*http.Response, error) {
	var body struct{ Stream bool }
	if err := json.Unmarshal(getBodyBytes(req), &body); err != nil {
		return nil, err
	}
	for _, fixture := range f.fixtures {
		if fixture.endpoint != req.URL.Path || fixture.stream != body.Stream {
			continue
		}
		contentType := "application/json"
		if fixture.stream {
			contentType = "text/event-stream"
		}
		var reader io.ReadCloser = io.NopCloser(strings.NewReader(fixture.raw))
		if f.cancelStream {
			streamCtx, cancel := context.WithCancel(ctx)
			reader = &sqliteCanceledStream{ctx: streamCtx, cancel: cancel, raw: fixture.raw}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: reader}, nil
	}
	return nil, fmt.Errorf("unexpected request %s", req.URL.Path)
}

type sqliteCanceledStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	raw    string
}

func (s *sqliteCanceledStream) Read(buffer []byte) (int, error) {
	if s.raw != "" {
		count := copy(buffer, s.raw)
		s.raw = s.raw[count:]
		if s.raw == "" {
			s.cancel()
		}
		return count, nil
	}
	return 0, s.ctx.Err()
}
func (s *sqliteCanceledStream) Close() error {
	s.cancel()
	return nil
}
