package handler

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/seifghazi/claude-code-monitor/internal/model"
)

// firstTokenObserver timestamps the first completed SSE data line containing
// generated content. Metadata, usage, finish markers and empty deltas do not
// establish TTFT. The buffer is bounded independently of response capture.
type firstTokenObserver struct {
	protocol string
	start    time.Time
	line     []byte
	skipping bool
	at       time.Time
}

const maxObservedSSELine = 1024 * 1024

func (o *firstTokenObserver) observe(chunk []byte, receivedAt time.Time) {
	if !o.at.IsZero() {
		return
	}
	for len(chunk) > 0 {
		end := bytes.IndexByte(chunk, '\n')
		if end < 0 {
			if !o.skipping {
				if len(o.line)+len(chunk) > maxObservedSSELine {
					o.line = nil
					o.skipping = true
				} else {
					o.line = append(o.line, chunk...)
				}
			}
			return
		}
		if !o.skipping && len(o.line)+end <= maxObservedSSELine {
			o.line = append(o.line, chunk[:end]...)
			if hasGeneratedContent(o.protocol, bytes.TrimSpace(o.line)) {
				o.at = receivedAt
				return
			}
		}
		o.line = nil
		o.skipping = false
		chunk = chunk[end+1:]
	}
}

func (o firstTokenObserver) record(response *model.ResponseLog) {
	if o.at.IsZero() {
		return
	}
	response.FirstTokenAt = o.at.Format(time.RFC3339Nano)
	ms := o.at.Sub(o.start).Milliseconds()
	if ms < 0 {
		ms = 0
	}
	response.TTFTMs = &ms
}

func hasGeneratedContent(protocol string, line []byte) bool {
	if !bytes.HasPrefix(line, []byte("data:")) {
		return false
	}
	data := bytes.TrimSpace(line[len("data:"):])
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return false
	}
	switch protocol {
	case openAIProtocolChat:
		var event struct {
			Choices []struct {
				Delta struct {
					Content          string              `json:"content"`
					ReasoningContent string              `json:"reasoning_content"`
					Refusal          string              `json:"refusal"`
					ToolCalls        []chatToolCallDelta `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal(data, &event) != nil {
			return false
		}
		for _, choice := range event.Choices {
			delta := choice.Delta
			if delta.Content != "" || delta.ReasoningContent != "" || delta.Refusal != "" {
				return true
			}
			for _, call := range delta.ToolCalls {
				if call.Function.Name != "" || call.Function.Arguments != "" {
					return true
				}
			}
		}
	case openAIProtocolResponses:
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		}
		if json.Unmarshal(data, &event) != nil || event.Delta == "" {
			return false
		}
		switch event.Type {
		case "response.output_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta", "response.refusal.delta":
			return true
		}
	}
	return false
}

func hasAnthropicGeneratedContent(event model.StreamingEvent) bool {
	switch event.Type {
	case "content_block_start":
		if event.ContentBlock == nil {
			return false
		}
		block := event.ContentBlock
		return block.Text != "" || block.Thinking != "" || (block.Type == "tool_use" && block.Name != "")
	case "content_block_delta":
		if event.Delta == nil {
			return false
		}
		delta := event.Delta
		return delta.Text != "" || delta.Thinking != "" || delta.PartialJSON != ""
	}
	return false
}
