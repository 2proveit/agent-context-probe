package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/model"
)

func TestConvertAnthropicToolLoopToOpenAI(t *testing.T) {
	request := &model.AnthropicRequest{
		Model:     "gpt-mock",
		MaxTokens: 256,
		Messages: []model.AnthropicMessage{
			{Role: "user", Content: "Weather in Hangzhou?"},
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{
						"type": "tool_use",
						"id":   "call_weather_1",
						"name": "get_weather",
						"input": map[string]interface{}{
							"city": "Hangzhou",
							"unit": "celsius",
						},
					},
				},
			},
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "call_weather_1",
						"content":     `{"condition":"sunny","temperature":31}`,
					},
				},
			},
		},
	}

	converted := convertAnthropicToOpenAI(request)
	messages, ok := converted["messages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("messages have unexpected type: %T", converted["messages"])
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d: %#v", len(messages), messages)
	}

	assistant := messages[1]
	if assistant["role"] != "assistant" {
		t.Fatalf("tool call role was not preserved: %#v", assistant)
	}
	toolCalls, ok := assistant["tool_calls"].([]map[string]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("assistant tool calls missing: %#v", assistant)
	}
	function, ok := toolCalls[0]["function"].(map[string]interface{})
	if !ok || function["name"] != "get_weather" {
		t.Fatalf("function call was not preserved: %#v", toolCalls[0])
	}
	var arguments map[string]interface{}
	if err := json.Unmarshal([]byte(function["arguments"].(string)), &arguments); err != nil {
		t.Fatalf("arguments are not valid JSON: %v", err)
	}
	if arguments["city"] != "Hangzhou" {
		t.Fatalf("function arguments changed: %#v", arguments)
	}

	result := messages[2]
	if result["role"] != "tool" ||
		result["tool_call_id"] != "call_weather_1" ||
		result["content"] != `{"condition":"sunny","temperature":31}` {
		t.Fatalf("tool result was not preserved: %#v", result)
	}
}

func TestTransformOpenAIToolCallResponseSetsAnthropicStopReason(t *testing.T) {
	response := []byte(`{
		"id": "chatcmpl-mock",
		"model": "gpt-mock",
		"choices": [{
			"finish_reason": "tool_calls",
			"message": {
				"role": "assistant",
				"content": null,
				"tool_calls": [{
					"id": "call_weather_1",
					"type": "function",
					"function": {
						"name": "get_weather",
						"arguments": "{\"city\":\"Hangzhou\"}"
					}
				}]
			}
		}]
	}`)

	var transformed map[string]interface{}
	if err := json.Unmarshal(transformOpenAIResponseToAnthropic(response), &transformed); err != nil {
		t.Fatalf("unmarshal transformed response: %v", err)
	}
	if transformed["stop_reason"] != "tool_use" {
		t.Fatalf("unexpected stop reason: %#v", transformed["stop_reason"])
	}
	content, ok := transformed["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("tool use content missing: %#v", transformed["content"])
	}
	toolUse := content[0].(map[string]interface{})
	if toolUse["type"] != "tool_use" || toolUse["name"] != "get_weather" {
		t.Fatalf("unexpected tool use block: %#v", toolUse)
	}
}

func TestTransformOpenAIToolCallStreamToAnthropic(t *testing.T) {
	openAIStream := strings.Join([]string{
		`data: {"id":"chatcmpl-mock","model":"gpt-mock","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_weather_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-mock","model":"gpt-mock","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Hangzhou\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: {"id":"chatcmpl-mock","model":"gpt-mock","choices":[],"usage":{"prompt_tokens":20,"completion_tokens":10,"total_tokens":30}}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
	var transformed bytes.Buffer

	transformOpenAIStreamToAnthropic(
		io.NopCloser(strings.NewReader(openAIStream)),
		&transformed,
	)

	output := transformed.String()
	for _, expected := range []string{
		`"type":"message_start"`,
		`"type":"tool_use"`,
		`"id":"call_weather_1"`,
		`"name":"get_weather"`,
		`"type":"input_json_delta"`,
		`"partial_json":"{\"city\":"`,
		`"partial_json":"\"Hangzhou\"}"`,
		`"type":"content_block_stop"`,
		`"stop_reason":"tool_use"`,
		`"type":"message_stop"`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %q in transformed stream:\n%s", expected, output)
		}
	}
}
