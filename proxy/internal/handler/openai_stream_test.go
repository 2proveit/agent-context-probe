package handler

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestAggregateChatCompletionsStreamRequiresDoneAndPreservesExtensions(t *testing.T) {
	body := []byte("data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"AIME-Atlas\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"think \"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"AIME-Atlas\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\",\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"q\\\":\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"AIME-Atlas\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"banks\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n" +
		"data: [DONE]\n")

	aggregated, err := aggregateOpenAIStream(openAIProtocolChat, body)
	if err != nil {
		t.Fatalf("aggregate stream: %v", err)
	}
	var completion chatCompletion
	if err := json.Unmarshal(aggregated, &completion); err != nil {
		t.Fatalf("parse completion: %v", err)
	}
	message := completion.Choices[0].Message
	if message.Content != "done" || message.ReasoningContent != "think " {
		t.Fatalf("unexpected assembled message: %+v", message)
	}
	if got := message.ToolCalls[0].Function.Arguments; got != `{"q":"banks"}` {
		t.Fatalf("unexpected tool arguments: %s", got)
	}

	_, err = aggregateOpenAIStream(openAIProtocolChat, bytesWithoutDone(body))
	if !errors.Is(err, errOpenAIStreamInterrupted) {
		t.Fatalf("expected interrupted error, got %v", err)
	}
}

func TestAggregateResponsesStreamUsesTerminalResponse(t *testing.T) {
	body := []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"complete\"}]}],\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3}}}\n\n")

	aggregated, err := aggregateOpenAIStream(openAIProtocolResponses, body)
	if err != nil {
		t.Fatalf("aggregate Responses stream: %v", err)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(aggregated, &response); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if response["status"] != "completed" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestAggregateResponsesStreamDoesNotReturnPartialOutput(t *testing.T) {
	body := []byte(`data: {"type":"response.output_text.delta","delta":"partial"}`)
	aggregated, err := aggregateOpenAIStream(openAIProtocolResponses, body)
	if aggregated != nil || !errors.Is(err, errOpenAIStreamInterrupted) {
		t.Fatalf("expected interruption without partial output, got %s, %v", aggregated, err)
	}
}

func bytesWithoutDone(body []byte) []byte {
	const done = "data: [DONE]\n"
	for index := len(body) - len(done); index >= 0; index-- {
		if string(body[index:index+len(done)]) == done {
			return append(append([]byte(nil), body[:index]...), body[index+len(done):]...)
		}
	}
	return body
}
