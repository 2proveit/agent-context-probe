package handler

import (
	"testing"
	"time"

	"github.com/seifghazi/claude-code-monitor/internal/model"
)

func TestFirstTokenObserverIgnoresMetadataAndHandlesSplitSSE(t *testing.T) {
	start := time.Now()
	observer := firstTokenObserver{protocol: openAIProtocolChat, start: start}
	observer.observe([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\r\n"), start.Add(10*time.Millisecond))
	observer.observe([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"\"}}]}\n"), start.Add(20*time.Millisecond))
	observer.observe([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hel"), start.Add(30*time.Millisecond))
	observer.observe([]byte("lo\"}}]}\n"), start.Add(40*time.Millisecond))
	observer.observe([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"later\"}}]}\n"), start.Add(50*time.Millisecond))
	response := &model.ResponseLog{}
	observer.record(response)
	if response.TTFTMs == nil || *response.TTFTMs != 40 {
		t.Fatalf("expected 40ms TTFT, got %+v", response.TTFTMs)
	}
	if response.FirstTokenAt != start.Add(40*time.Millisecond).Format(time.RFC3339Nano) {
		t.Fatalf("wrong first token timestamp: %s", response.FirstTokenAt)
	}
}

func TestFirstTokenObserverProtocolsAndEmptyStream(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		line     string
		want     bool
	}{
		{"chat tool name", openAIProtocolChat, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"search"}}]}}]}`, true},
		{"chat tool id only", openAIProtocolChat, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1"}]}}]}`, false},
		{"responses text", openAIProtocolResponses, `data: {"type":"response.output_text.delta","delta":"hello"}`, true},
		{"responses metadata", openAIProtocolResponses, `data: {"type":"response.created","delta":"hello"}`, false},
		{"responses tool args", openAIProtocolResponses, `data: {"type":"response.function_call_arguments.delta","delta":"{"}`, true},
		{"done", openAIProtocolChat, "data: [DONE]", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			observer := firstTokenObserver{protocol: tc.protocol, start: time.Now()}
			observer.observe([]byte(tc.line+"\n"), time.Now())
			response := &model.ResponseLog{}
			observer.record(response)
			if (response.TTFTMs != nil) != tc.want {
				t.Fatalf("unexpected TTFT for %s: %+v", tc.name, response)
			}
		})
	}
	if !hasAnthropicGeneratedContent(model.StreamingEvent{Type: "content_block_delta", Delta: &model.Delta{Type: "thinking_delta", Thinking: "reason"}}) {
		t.Fatal("Anthropic thinking delta should establish first token")
	}
	if hasAnthropicGeneratedContent(model.StreamingEvent{Type: "content_block_start", ContentBlock: &model.ContentBlock{Type: "text"}}) {
		t.Fatal("empty Anthropic content block should not establish first token")
	}
}
