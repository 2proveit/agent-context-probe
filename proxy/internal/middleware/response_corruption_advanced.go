package middleware

import "strings"

const reasoningRepeatUnit = "我需要重新检查上一轮的执行结果。"

// argumentPrefix forwards an actual JSON object/array prefix, stopping before
// its outer closing delimiter. It works across deltas without buffering the
// call or breaking UTF-8, and cannot accidentally return a complete JSON value.
type argumentPrefix struct {
	started, stopped, quoted, escaped bool
	depth                             int
}

func (p *argumentPrefix) take(s string) string {
	if p.stopped {
		return ""
	}
	end := len(s)
	for i, c := range s {
		if !p.started {
			if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
				continue
			}
			if c != '{' && c != '[' {
				p.stopped = true
				return s[:i]
			}
			p.started = true
			p.depth = 1
			continue
		}
		if p.quoted {
			if p.escaped {
				p.escaped = false
			} else if c == '\\' {
				p.escaped = true
			} else if c == '"' {
				p.quoted = false
			}
			continue
		}
		switch c {
		case '"':
			p.quoted = true
		case '{', '[':
			p.depth++
		case '}', ']':
			p.depth--
			if p.depth == 0 {
				end = i
				p.stopped = true
			}
		}
		if p.stopped {
			break
		}
	}
	return s[:end]
}
func truncatedArguments(s string) string { return (&argumentPrefix{}).take(s) }
func (m *responseMutation) argumentDelta(key, s string) string {
	p := m.prefixes[key]
	if p == nil {
		p = &argumentPrefix{}
		m.prefixes[key] = p
	}
	return p.take(s)
}
func (m *responseMutation) advanced() bool {
	return m.mode == "reasoning_repetition" || m.mode == "truncated_tool_arguments"
}
func (m *responseMutation) reason(key, s string, delta bool) string {
	if s == "" {
		return s
	}
	if delta {
		m.reasonCounts[key]++
		return strings.Repeat(reasoningRepeatUnit, 8)
	}
	n := m.reasonCounts[key] * 8
	if n == 0 {
		n = 64
	}
	return strings.Repeat(reasoningRepeatUnit, n)
}
func (m *responseMutation) reasonField(v object, field, key string, delta bool) bool {
	s, ok := v[field].(string)
	if !ok || s == "" {
		return false
	}
	v[field] = m.reason(key, s, delta)
	return true
}
func (m *responseMutation) advancedChat(v object, stream bool) bool {
	changed := false
	for _, x := range list(v["choices"]) {
		c := obj(x)
		msg := obj(c["message"])
		if stream {
			msg = obj(c["delta"])
		}
		if msg == nil {
			continue
		}
		key := choiceKey(c)
		if m.mode == "reasoning_repetition" {
			changed = m.reasonField(msg, "reasoning_content", key, stream) || changed
			continue
		}
		for _, x := range list(msg["tool_calls"]) {
			t := obj(x)
			f := obj(t["function"])
			m.toolChoices[key] = true
			if s, ok := f["arguments"].(string); ok {
				if stream {
					f["arguments"] = m.argumentDelta(toolKey(c, t), s)
				} else {
					f["arguments"] = truncatedArguments(s)
				}
				changed = true
			}
		}
		if m.toolChoices[key] && c["finish_reason"] != nil {
			c["finish_reason"] = "length"
			changed = true
		}
	}
	return changed
}
func (m *responseMutation) advancedMessages(v object) bool {
	changed, tool := false, false
	for _, x := range list(v["content"]) {
		b := obj(x)
		if m.mode == "reasoning_repetition" {
			if str(b["type"]) == "thinking" {
				changed = m.reasonField(b, "thinking", "", false) || changed
			}
		} else if str(b["type"]) == "tool_use" {
			// Messages JSON requires an object here; a truncated serialized input
			// intentionally violates that field's type while keeping the envelope valid.
			b["input"] = truncatedArguments(string(encodeFault(b["input"])))
			tool = true
			changed = true
		}
	}
	if tool {
		v["stop_reason"] = "max_tokens"
	}
	return changed
}
func reasonKey(id, channel string, index interface{}) string {
	return id + ":" + channel + ":" + string(encodeFault(index))
}
func (m *responseMutation) advancedItem(item object) bool {
	if m.mode == "truncated_tool_arguments" {
		if str(item["type"]) != "function_call" {
			return false
		}
		m.toolChoices["responses"] = true
		if s, ok := item["arguments"].(string); ok {
			item["arguments"] = truncatedArguments(s)
			return true
		}
		return false
	}
	if str(item["type"]) != "reasoning" {
		return false
	}
	changed := false
	for _, channel := range []string{"summary", "content"} {
		for i, x := range list(item[channel]) {
			b := obj(x)
			if str(b["type"]) == "summary_text" || str(b["type"]) == "reasoning_text" {
				changed = m.reasonField(b, "text", reasonKey(str(item["id"]), channel, i), false) || changed
			}
		}
	}
	return changed
}
func (m *responseMutation) advancedResponse(v object) bool {
	changed := false
	for _, x := range list(v["output"]) {
		changed = m.advancedItem(obj(x)) || changed
	}
	// A length-limited Responses response ends in incomplete, never completed.
	if m.mode == "truncated_tool_arguments" && m.toolChoices["responses"] && (v["status"] == "completed" || v["status"] == "incomplete") {
		v["status"] = "incomplete"
		details := obj(v["incomplete_details"])
		if details == nil {
			details = object{}
			v["incomplete_details"] = details
		}
		details["reason"] = "max_output_tokens"
		changed = true
	}
	return changed
}
func (m *responseMutation) advancedJSON(raw []byte) ([]byte, bool) {
	v := decodeObject(raw)
	if v == nil {
		return raw, false
	}
	changed := false
	switch m.path {
	case "/v1/messages":
		changed = m.advancedMessages(v)
	case "/v1/responses":
		changed = m.advancedResponse(v)
	default:
		changed = m.advancedChat(v, false)
	}
	if !changed {
		return raw, false
	}
	return encodeFault(v), true
}
func (m *responseMutation) advancedMessageEvent(v object) (bool, bool) {
	kind := str(v["type"])
	key := string(encodeFault(v["index"]))
	if kind == "message_start" {
		return m.advancedMessages(obj(v["message"])), false
	}
	if kind == "content_block_start" {
		b := obj(v["content_block"])
		m.blocks[key] = str(b["type"])
		if m.mode == "reasoning_repetition" && str(b["type"]) == "thinking" {
			return m.reasonField(b, "thinking", key, true), false
		}
		if m.mode == "truncated_tool_arguments" && str(b["type"]) == "tool_use" {
			m.toolChoices["messages"] = true
			// Normally input is {} and actual arguments follow in deltas. A provider
			// may also supply a populated input in the start event.
			if input := obj(b["input"]); len(input) > 0 {
				b["input"] = truncatedArguments(string(encodeFault(input)))
				m.seen[key] = true
				return true, false
			}
		}
	}
	if kind == "content_block_delta" {
		d := obj(v["delta"])
		if m.mode == "reasoning_repetition" && str(d["type"]) == "thinking_delta" {
			return m.reasonField(d, "thinking", key, true), false
		}
		if m.mode == "truncated_tool_arguments" && m.blocks[key] == "tool_use" && str(d["type"]) == "input_json_delta" {
			d["partial_json"] = m.argumentDelta(key, str(d["partial_json"]))
			m.seen[key] = true
			return true, false
		}
	}
	if kind == "message_delta" && m.mode == "truncated_tool_arguments" && m.toolChoices["messages"] {
		if d := obj(v["delta"]); d != nil {
			d["stop_reason"] = "max_tokens"
			return true, false
		}
	}
	return false, false
}
func (m *responseMutation) advancedResponseEvent(v object) bool {
	kind := str(v["type"])
	if r := obj(v["response"]); r != nil {
		changed := m.advancedResponse(r)
		if m.mode == "truncated_tool_arguments" && m.toolChoices["responses"] && kind == "response.completed" {
			v["type"] = "response.incomplete"
			changed = true
		}
		return changed
	}
	if item := obj(v["item"]); item != nil {
		if m.mode == "truncated_tool_arguments" && strings.HasSuffix(kind, ".added") && str(item["type"]) == "function_call" {
			m.toolChoices["responses"] = true
			if s, ok := item["arguments"].(string); ok && s != "" {
				item["arguments"] = m.argumentDelta(str(item["id"]), s)
				return true
			}
			return false
		}
		return m.advancedItem(item)
	}
	id := str(v["item_id"])
	if m.mode == "truncated_tool_arguments" {
		if kind == "response.function_call_arguments.delta" {
			m.toolChoices["responses"] = true
			v["delta"] = m.argumentDelta(id, str(v["delta"]))
			return true
		}
		if kind == "response.function_call_arguments.done" {
			v["arguments"] = truncatedArguments(str(v["arguments"]))
			return true
		}
		return false
	}
	channel, index := "summary", v["summary_index"]
	if index == nil {
		index = 0
	}
	if strings.HasPrefix(kind, "response.reasoning_text.") {
		channel = "content"
		index = v["content_index"]
		if index == nil {
			index = 0
		}
	}
	key := reasonKey(id, channel, index)
	if kind == "response.reasoning_summary_text.delta" || kind == "response.reasoning_text.delta" {
		return m.reasonField(v, "delta", key, true)
	}
	if kind == "response.reasoning_summary_text.done" || kind == "response.reasoning_text.done" {
		return m.reasonField(v, "text", key, false)
	}
	if part := obj(v["part"]); str(part["type"]) == "summary_text" && strings.HasPrefix(kind, "response.reasoning_summary_part.") {
		return m.reasonField(part, "text", key, false)
	}
	return false
}
func (m *responseMutation) advancedSSE(raw []byte) ([][]byte, bool) {
	v := frameObject(raw)
	if v == nil {
		return [][]byte{raw}, false
	}
	changed := false
	switch m.path {
	case "/v1/messages":
		// Empty-input tools sometimes have no argument deltas. Supply a deliberately
		// incomplete prefix before their real block_stop, using the same block index.
		key := string(encodeFault(v["index"]))
		if m.mode == "truncated_tool_arguments" && str(v["type"]) == "content_block_stop" && m.blocks[key] == "tool_use" && !m.seen[key] {
			m.seen[key] = true
			d := clone(v)
			d["type"] = "content_block_delta"
			d["delta"] = object{"type": "input_json_delta", "partial_json": "{"}
			extra := frameWith(raw, d)
			lines := strings.Split(string(extra), "\n")
			kept := lines[:0]
			for _, line := range lines {
				if !strings.HasPrefix(line, "id:") {
					kept = append(kept, line)
				}
			}
			return [][]byte{[]byte(strings.Join(kept, "\n")), raw}, true
		}
		changed, _ = m.advancedMessageEvent(v)
	case "/v1/responses":
		changed = m.advancedResponseEvent(v)
	default:
		changed = m.advancedChat(v, true)
	}
	if !changed {
		return [][]byte{raw}, false
	}
	return [][]byte{frameWith(raw, v)}, true
}
