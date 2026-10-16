package middleware

import (
	"encoding/json"
	"strings"
)

const syntheticReasoning = "Synthetic reasoning-only response for harness fault testing."
const syntheticPseudoTags = "</tool returns> ### OpenHands Agent <system_warning> Self-Verify: synthetic model output contamination </system_warning>"

func corruptionText(mode string) string {
	switch mode {
	case "repetition":
		return strings.Repeat("果树。", 1400) + "\ufffd"
	case "pseudo_tags":
		return syntheticPseudoTags
	}
	return ""
}
func encodeFault(v interface{}) []byte { b, _ := json.Marshal(v); return b }
func syntheticResponse(path, model, tool, id, mode string, stream bool) []byte {
	switch path {
	case "/v1/messages":
		return anthropicFault(model, tool, id, mode, stream)
	case "/v1/responses":
		return responsesFault(model, tool, id, mode, stream)
	default:
		return chatFault(model, tool, id, mode, stream)
	}
}

// xml_tool_name mirrors the observed failure: the serialized invocation lands
// in the name field while arguments remain valid JSON. Never use real paths or
// commands in this synthetic payload.
func corruptedTool(mode, tool string) (string, string) {
	if mode == "xml_tool_name" {
		return "<function=" + tool + ">\n<parameter=name>\nsynthetic-fault\n</parameter>\n</function>", "{}"
	}
	return tool, "{invalid"
}

func isToolCorruption(mode string) bool {
	return mode == "invalid_tool_arguments" || mode == "xml_tool_name"
}

func chatFault(model, tool, id, mode string, stream bool) []byte {
	tool, arguments := corruptedTool(mode, tool)
	text := corruptionText(mode)
	message := map[string]interface{}{"role": "assistant", "content": text}
	finish := "stop"
	if mode == "empty_stop" {
		message["reasoning_content"] = syntheticReasoning
	}
	if isToolCorruption(mode) {
		message["content"] = nil
		message["tool_calls"] = []interface{}{map[string]interface{}{"id": id + "_call", "type": "function", "function": map[string]interface{}{"name": tool, "arguments": arguments}}}
		finish = "tool_calls"
	}
	usage := map[string]int{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
	if !stream {
		return encodeFault(map[string]interface{}{"id": id, "object": "chat.completion", "created": 0, "model": model, "choices": []interface{}{map[string]interface{}{"index": 0, "message": message, "finish_reason": finish}}, "usage": usage})
	}
	if mode == "xml_tool_name" {
		return chatXMLToolStream(model, tool, id, usage)
	}
	var out strings.Builder
	emit := func(delta interface{}, stop interface{}) {
		out.WriteString("data: ")
		out.Write(encodeFault(map[string]interface{}{"id": id, "object": "chat.completion.chunk", "created": 0, "model": model, "choices": []interface{}{map[string]interface{}{"index": 0, "delta": delta, "finish_reason": stop}}}))
		out.WriteString("\n\n")
	}
	emit(map[string]string{"role": "assistant"}, nil)
	switch mode {
	case "empty_stop":
		emit(map[string]string{"reasoning_content": syntheticReasoning}, nil)
	case "invalid_tool_arguments", "xml_tool_name":
		emit(map[string]interface{}{"tool_calls": []interface{}{map[string]interface{}{"index": 0, "id": id + "_call", "type": "function", "function": map[string]string{"name": tool, "arguments": arguments}}}}, nil)
	default:
		emit(map[string]string{"content": text}, nil)
	}
	emit(map[string]interface{}{}, finish)
	out.WriteString("data: ")
	out.Write(encodeFault(map[string]interface{}{"id": id, "object": "chat.completion.chunk", "created": 0, "model": model, "choices": []interface{}{}, "usage": usage}))
	out.WriteString("\n\ndata: [DONE]\n\n")
	return []byte(out.String())
}

func anthropicFault(model, tool, id, mode string, stream bool) []byte {
	tool, arguments := corruptedTool(mode, tool)
	stop := "end_turn"
	block := map[string]interface{}{"type": "text", "text": corruptionText(mode)}
	if mode == "empty_stop" {
		block = map[string]interface{}{"type": "thinking", "thinking": syntheticReasoning}
	}
	if isToolCorruption(mode) {
		var input interface{} = arguments
		if mode == "xml_tool_name" {
			input = map[string]interface{}{}
		}
		block = map[string]interface{}{"type": "tool_use", "id": id + "_call", "name": tool, "input": input}
		stop = "tool_use"
	}
	usage := map[string]int{"input_tokens": 0, "output_tokens": 0}
	message := map[string]interface{}{"id": id, "type": "message", "role": "assistant", "model": model, "content": []interface{}{block}, "stop_reason": stop, "stop_sequence": nil, "usage": usage}
	if !stream {
		return encodeFault(message)
	}
	var out strings.Builder
	emit := func(kind string, v map[string]interface{}) {
		v["type"] = kind
		out.WriteString("event: " + kind + "\ndata: ")
		out.Write(encodeFault(v))
		out.WriteString("\n\n")
	}
	emit("message_start", map[string]interface{}{"message": map[string]interface{}{"id": id, "type": "message", "role": "assistant", "model": model, "content": []interface{}{}, "stop_reason": nil, "stop_sequence": nil, "usage": usage}})
	var start, delta map[string]interface{}
	switch mode {
	case "empty_stop":
		start = map[string]interface{}{"type": "thinking", "thinking": ""}
		delta = map[string]interface{}{"type": "thinking_delta", "thinking": syntheticReasoning}
	case "invalid_tool_arguments", "xml_tool_name":
		start = map[string]interface{}{"type": "tool_use", "id": id + "_call", "name": tool, "input": map[string]interface{}{}}
		delta = map[string]interface{}{"type": "input_json_delta", "partial_json": arguments}
	default:
		start = map[string]interface{}{"type": "text", "text": ""}
		delta = map[string]interface{}{"type": "text_delta", "text": corruptionText(mode)}
	}
	emit("content_block_start", map[string]interface{}{"index": 0, "content_block": start})
	emit("content_block_delta", map[string]interface{}{"index": 0, "delta": delta})
	emit("content_block_stop", map[string]interface{}{"index": 0})
	emit("message_delta", map[string]interface{}{"delta": map[string]interface{}{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 0}})
	emit("message_stop", map[string]interface{}{})
	return []byte(out.String())
}

func responsesFault(model, tool, id, mode string, stream bool) []byte {
	tool, arguments := corruptedTool(mode, tool)
	text := corruptionText(mode)
	itemID := id + "_item"
	part := map[string]interface{}{"type": "output_text", "text": text, "annotations": []interface{}{}}
	item := map[string]interface{}{"id": itemID, "type": "message", "status": "completed", "role": "assistant", "content": []interface{}{part}}
	if mode == "empty_stop" {
		part = map[string]interface{}{"type": "summary_text", "text": syntheticReasoning}
		item = map[string]interface{}{"id": itemID, "type": "reasoning", "summary": []interface{}{part}}
	}
	if isToolCorruption(mode) {
		item = map[string]interface{}{"id": itemID, "type": "function_call", "status": "completed", "call_id": id + "_call", "name": tool, "arguments": arguments}
	}
	response := map[string]interface{}{"id": id, "object": "response", "created_at": 0, "model": model, "status": "completed", "error": nil, "incomplete_details": nil, "output": []interface{}{item}, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}}
	if !stream {
		return encodeFault(response)
	}
	var out strings.Builder
	seq := 0
	emit := func(kind string, v map[string]interface{}) {
		v["type"] = kind
		v["sequence_number"] = seq
		seq++
		out.WriteString("event: " + kind + "\ndata: ")
		out.Write(encodeFault(v))
		out.WriteString("\n\n")
	}
	emit("response.created", map[string]interface{}{"response": map[string]interface{}{"id": id, "object": "response", "created_at": 0, "model": model, "status": "in_progress", "output": []interface{}{}}})
	pending := map[string]interface{}{}
	for k, v := range item {
		pending[k] = v
	}
	if isToolCorruption(mode) {
		pending["arguments"] = ""
	} else if mode == "empty_stop" {
		pending["summary"] = []interface{}{}
	} else {
		pending["content"] = []interface{}{}
	}
	if mode != "empty_stop" {
		pending["status"] = "in_progress"
	}
	emit("response.output_item.added", map[string]interface{}{"output_index": 0, "item": pending})
	if isToolCorruption(mode) {
		emit("response.function_call_arguments.delta", map[string]interface{}{"output_index": 0, "item_id": itemID, "delta": arguments})
		emit("response.function_call_arguments.done", map[string]interface{}{"output_index": 0, "item_id": itemID, "arguments": arguments})
	} else if mode == "empty_stop" {
		emit("response.reasoning_summary_part.added", map[string]interface{}{"output_index": 0, "item_id": itemID, "summary_index": 0, "part": map[string]interface{}{"type": "summary_text", "text": ""}})
		emit("response.reasoning_summary_text.delta", map[string]interface{}{"output_index": 0, "item_id": itemID, "summary_index": 0, "delta": syntheticReasoning})
		emit("response.reasoning_summary_text.done", map[string]interface{}{"output_index": 0, "item_id": itemID, "summary_index": 0, "text": syntheticReasoning})
		emit("response.reasoning_summary_part.done", map[string]interface{}{"output_index": 0, "item_id": itemID, "summary_index": 0, "part": part})
	} else {
		emit("response.content_part.added", map[string]interface{}{"output_index": 0, "item_id": itemID, "content_index": 0, "part": map[string]interface{}{"type": "output_text", "text": "", "annotations": []interface{}{}}})
		emit("response.output_text.delta", map[string]interface{}{"output_index": 0, "item_id": itemID, "content_index": 0, "delta": text})
		emit("response.output_text.done", map[string]interface{}{"output_index": 0, "item_id": itemID, "content_index": 0, "text": text})
		emit("response.content_part.done", map[string]interface{}{"output_index": 0, "item_id": itemID, "content_index": 0, "part": part})
	}
	emit("response.output_item.done", map[string]interface{}{"output_index": 0, "item": item})
	emit("response.completed", map[string]interface{}{"response": response})
	return []byte(out.String())
}

// chatXMLToolStream follows the observed call_06/call_09 data-frame order.
// The initial tool frame carries an empty argument string; only the following
// frame completes it. IDs and names must not be repeated in the continuation.
func chatXMLToolStream(model, tool, id string, usage map[string]int) []byte {
	var out strings.Builder
	emit := func(delta map[string]interface{}, finish string, includeUsage bool) {
		choice := map[string]interface{}{"index": 0, "delta": delta}
		if finish != "" {
			choice["finish_reason"] = finish
		}
		chunk := map[string]interface{}{"id": id, "object": "chat.completion.chunk", "created": 0, "model": model, "choices": []interface{}{choice}}
		if includeUsage {
			chunk["usage"] = usage
		}
		out.WriteString("data: ")
		out.Write(encodeFault(chunk))
		out.WriteString("\n\n")
	}
	emit(map[string]interface{}{"role": "assistant", "reasoning_content": "Synthetic reasoning "}, "", false)
	emit(map[string]interface{}{"reasoning_content": "for XML tool-name fault testing."}, "", false)
	emit(map[string]interface{}{"content": "\n\n"}, "", false)
	emit(map[string]interface{}{"content": "", "tool_calls": []interface{}{map[string]interface{}{"index": 0, "id": id + "_call", "type": "function", "function": map[string]string{"name": tool, "arguments": ""}}}}, "", false)
	emit(map[string]interface{}{"content": "", "tool_calls": []interface{}{map[string]interface{}{"index": 0, "type": "function", "function": map[string]string{"arguments": "{}"}}}}, "", false)
	emit(map[string]interface{}{}, "tool_calls", false)
	emit(map[string]interface{}{}, "", true)
	out.WriteString("data: [DONE]\n\n")
	return []byte(out.String())
}
