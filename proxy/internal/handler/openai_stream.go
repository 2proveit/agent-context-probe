package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	openAIProtocolChat      = "openai_chat_completions"
	openAIProtocolResponses = "openai_responses"
)

var errOpenAIStreamInterrupted = errors.New("stream ended before a terminal event")

type chatStreamChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []chatStreamChoice `json:"choices"`
	Usage   json.RawMessage    `json:"usage"`
}

type chatStreamChoice struct {
	Index        int             `json:"index"`
	Delta        chatStreamDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
}

type chatStreamDelta struct {
	Role             string              `json:"role"`
	Content          string              `json:"content"`
	ReasoningContent string              `json:"reasoning_content"`
	Refusal          string              `json:"refusal"`
	ToolCalls        []chatToolCallDelta `json:"tool_calls"`
}

type chatToolCallDelta struct {
	Index    int              `json:"index"`
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatChoiceAccumulator struct {
	Role             string
	Content          string
	ReasoningContent string
	Refusal          string
	ToolCalls        map[int]*chatToolCallDelta
	FinishReason     *string
}

type chatCompletion struct {
	ID      string                 `json:"id,omitempty"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created,omitempty"`
	Model   string                 `json:"model,omitempty"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   json.RawMessage        `json:"usage,omitempty"`
}

type chatCompletionChoice struct {
	Index        int                   `json:"index"`
	Message      chatCompletionMessage `json:"message"`
	FinishReason *string               `json:"finish_reason"`
}

type chatCompletionMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	Refusal          string         `json:"refusal,omitempty"`
	ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function chatFunctionCall `json:"function"`
}

func aggregateOpenAIStream(protocol string, body []byte) (json.RawMessage, error) {
	switch protocol {
	case openAIProtocolChat:
		return aggregateChatCompletionsStream(body)
	case openAIProtocolResponses:
		return aggregateResponsesStream(body)
	default:
		return nil, fmt.Errorf("unsupported OpenAI protocol %q", protocol)
	}
}

func aggregateChatCompletionsStream(body []byte) (json.RawMessage, error) {
	choices := make(map[int]*chatChoiceAccumulator)
	completion := chatCompletion{Object: "chat.completion"}
	done := false

	for _, data := range openAISSEData(body) {
		if bytes.Equal(data, []byte("[DONE]")) {
			done = true
			continue
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			continue
		}
		if completion.ID == "" {
			completion.ID = chunk.ID
			completion.Created = chunk.Created
			completion.Model = chunk.Model
		}
		if len(chunk.Usage) > 0 && string(chunk.Usage) != "null" {
			completion.Usage = append(json.RawMessage(nil), chunk.Usage...)
		}

		for _, choice := range chunk.Choices {
			accumulator := choices[choice.Index]
			if accumulator == nil {
				accumulator = &chatChoiceAccumulator{
					Role:      "assistant",
					ToolCalls: make(map[int]*chatToolCallDelta),
				}
				choices[choice.Index] = accumulator
			}
			if choice.Delta.Role != "" {
				accumulator.Role = choice.Delta.Role
			}
			accumulator.Content += choice.Delta.Content
			accumulator.ReasoningContent += choice.Delta.ReasoningContent
			accumulator.Refusal += choice.Delta.Refusal
			if choice.FinishReason != nil {
				reason := *choice.FinishReason
				accumulator.FinishReason = &reason
			}
			for _, delta := range choice.Delta.ToolCalls {
				toolCall := accumulator.ToolCalls[delta.Index]
				if toolCall == nil {
					toolCall = &chatToolCallDelta{Index: delta.Index}
					accumulator.ToolCalls[delta.Index] = toolCall
				}
				if delta.ID != "" {
					toolCall.ID = delta.ID
				}
				if delta.Type != "" {
					toolCall.Type = delta.Type
				}
				toolCall.Function.Name += delta.Function.Name
				toolCall.Function.Arguments += delta.Function.Arguments
			}
		}
	}

	if !done {
		return nil, errOpenAIStreamInterrupted
	}

	indexes := sortedMapKeys(choices)
	for _, index := range indexes {
		accumulator := choices[index]
		toolCalls := make([]chatToolCall, 0, len(accumulator.ToolCalls))
		for _, toolIndex := range sortedMapKeys(accumulator.ToolCalls) {
			delta := accumulator.ToolCalls[toolIndex]
			toolCalls = append(toolCalls, chatToolCall{
				ID:       delta.ID,
				Type:     delta.Type,
				Function: delta.Function,
			})
		}
		completion.Choices = append(completion.Choices, chatCompletionChoice{
			Index: index,
			Message: chatCompletionMessage{
				Role:             accumulator.Role,
				Content:          accumulator.Content,
				ReasoningContent: accumulator.ReasoningContent,
				Refusal:          accumulator.Refusal,
				ToolCalls:        toolCalls,
			},
			FinishReason: accumulator.FinishReason,
		})
	}

	return json.Marshal(completion)
}

func aggregateResponsesStream(body []byte) (json.RawMessage, error) {
	for _, data := range openAISSEData(body) {
		var event struct {
			Type     string          `json:"type"`
			Response json.RawMessage `json:"response"`
			Message  string          `json:"message"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}
		switch event.Type {
		case "response.completed":
			if len(event.Response) == 0 || string(event.Response) == "null" {
				return nil, errors.New("response.completed did not include a response object")
			}
			return append(json.RawMessage(nil), event.Response...), nil
		case "response.failed", "response.incomplete":
			if len(event.Response) > 0 && string(event.Response) != "null" {
				return append(json.RawMessage(nil), event.Response...), fmt.Errorf("%s", event.Type)
			}
			return nil, fmt.Errorf("%s", event.Type)
		case "error":
			if event.Message != "" {
				return nil, errors.New(event.Message)
			}
			return nil, errors.New("Responses stream returned an error event")
		}
	}
	return nil, errOpenAIStreamInterrupted
}

func openAISSEData(body []byte) [][]byte {
	var events [][]byte
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) > 0 {
			events = append(events, data)
		}
	}
	return events
}

func sortedMapKeys[T any](values map[int]T) []int {
	keys := make([]int, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}
