package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/model"
)

type faultFrameRecorder struct {
	*httptest.ResponseRecorder
	frames  [][]byte
	flushes int
}

func (r *faultFrameRecorder) Write(p []byte) (int, error) {
	r.frames = append(r.frames, append([]byte(nil), p...))
	return r.ResponseRecorder.Write(p)
}
func (r *faultFrameRecorder) Flush() { r.flushes++; r.ResponseRecorder.Flush() }

func TestXMLToolNameSSEFrames(t *testing.T) {
	recorder := &faultFrameRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req = req.WithContext(context.WithValue(req.Context(), model.BodyBytesKey, []byte(`{"model":"client-model","tools":[{"function":{"name":"bash"}}]}`)))
	handler := ResponseCorruption(config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 1, Modes: []string{"xml_tool_name"}}, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: upstream\n\n"))
		_, _ = w.Write([]byte("data: original must be suppressed\n\n"))
	}))
	handler.ServeHTTP(recorder, req)
	if len(recorder.frames) != 8 || recorder.flushes != 8 {
		t.Fatalf("want 8 independent event writes/flushes, got %d/%d", len(recorder.frames), recorder.flushes)
	}
	var events []map[string]interface{}
	for _, frame := range recorder.frames[:7] {
		if !bytes.HasSuffix(frame, []byte("\n\n")) || bytes.Count(frame, []byte("\n\n")) != 1 {
			t.Fatalf("not one event: %s", frame)
		}
		var event map[string]interface{}
		if err := json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(frame, []byte("data:"))), &event); err != nil {
			t.Fatal(err)
		}
		if event["model"] != "client-model" || event["id"] != "chaos_1" {
			t.Fatal("inconsistent event identity")
		}
		events = append(events, event)
	}
	delta := func(i int) map[string]interface{} {
		return events[i]["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})
	}
	if delta(0)["role"] != "assistant" || delta(0)["reasoning_content"] == nil || delta(1)["reasoning_content"] == nil || delta(2)["content"] != "\n\n" {
		t.Fatal("missing observed reasoning/content prefix")
	}
	first := delta(3)["tool_calls"].([]interface{})[0].(map[string]interface{})
	continuation := delta(4)["tool_calls"].([]interface{})[0].(map[string]interface{})
	fn := first["function"].(map[string]interface{})
	if fn["arguments"] != "" || fn["name"] != "<function=bash>\n<parameter=name>\nsynthetic-fault\n</parameter>\n</function>" || first["id"] != "chaos_1_call" {
		t.Fatalf("wrong initial tool frame: %v", first)
	}
	args := continuation["function"].(map[string]interface{})
	if len(args) != 1 || args["arguments"] != "{}" || continuation["id"] != nil || continuation["index"] != first["index"] {
		t.Fatalf("continuation repeats identity or damages arguments: %v", continuation)
	}
	if events[5]["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"] != "tool_calls" || events[5]["usage"] != nil {
		t.Fatal("finish must precede usage")
	}
	if events[6]["usage"] == nil || len(delta(6)) != 0 {
		t.Fatal("missing separate usage with empty delta choice")
	}
	if string(recorder.frames[7]) != "data: [DONE]\n\n" {
		t.Fatal("missing final DONE event")
	}
}
