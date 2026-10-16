package middleware

import (
	"bytes"
	"encoding/json"
	"strings"
)

type object = map[string]interface{}

func obj(v interface{}) object         { m, _ := v.(map[string]interface{}); return m }
func list(v interface{}) []interface{} { a, _ := v.([]interface{}); return a }
func str(v interface{}) string         { s, _ := v.(string); return s }
func clone(v object) object {
	c := object{}
	for k, x := range v {
		c[k] = x
	}
	return c
}
func decodeObject(p []byte) object {
	var v object
	d := json.NewDecoder(bytes.NewReader(p))
	d.UseNumber()
	if d.Decode(&v) != nil {
		return nil
	}
	return v
}
func encodeFault(v interface{}) []byte { b, _ := json.Marshal(v); return b }
func corruptionText(mode string) string {
	if mode == "repetition" {
		return strings.Repeat("果树。", 1400) + "\ufffd"
	}
	if mode == "pseudo_tags" {
		return "</tool returns> ### OpenHands Agent <system_warning> Self-Verify: synthetic model output contamination </system_warning>"
	}
	return ""
}
func isToolCorruption(mode string) bool {
	return mode == "invalid_tool_arguments" || mode == "xml_tool_name"
}
func xmlToolName(name string) string {
	return "<function=" + name + ">\n<parameter=name>\nsynthetic-fault\n</parameter>\n</function>"
}
func faultArguments(mode string) string {
	if mode == "xml_tool_name" {
		return "{}"
	}
	return "{invalid"
}

type responseMutation struct {
	path, mode string
	seen       map[string]bool
	blocks     map[string]string
	queued     [][]byte
}

func newResponseMutation(path, mode string) *responseMutation {
	return &responseMutation{path: path, mode: mode, seen: map[string]bool{}, blocks: map[string]string{}}
}
func (m *responseMutation) tool(v object) bool {
	if v == nil {
		return false
	}
	if m.mode == "xml_tool_name" {
		if name, ok := v["name"].(string); ok {
			v["name"] = xmlToolName(name)
		}
	}
	v["arguments"] = faultArguments(m.mode)
	return true
}
func (m *responseMutation) chatMessage(choice object) bool {
	msg := obj(choice["message"])
	if msg == nil {
		return false
	}
	if isToolCorruption(m.mode) {
		changed := false
		for _, t := range list(msg["tool_calls"]) {
			changed = m.tool(obj(obj(t)["function"])) || changed
		}
		return changed
	}
	msg["content"] = corruptionText(m.mode)
	delete(msg, "tool_calls")
	choice["finish_reason"] = "stop"
	return true
}
func (m *responseMutation) messageBlocks(v object) bool {
	changed := false
	out := []interface{}{}
	textSent := false
	for _, value := range list(v["content"]) {
		b := obj(value)
		kind := str(b["type"])
		if isToolCorruption(m.mode) {
			if kind == "tool_use" {
				if m.mode == "xml_tool_name" {
					b["name"] = xmlToolName(str(b["name"]))
					b["input"] = object{}
				} else {
					b["input"] = "{invalid"
				}
				changed = true
			}
			out = append(out, value)
			continue
		}
		if kind == "text" || kind == "tool_use" {
			changed = true
			if m.mode == "empty_stop" {
				continue
			}
			b["type"] = "text"
			delete(b, "name")
			delete(b, "input")
			b["text"] = ""
			if !textSent {
				b["text"] = corruptionText(m.mode)
				textSent = true
			}
		}
		out = append(out, value)
	}
	if !isToolCorruption(m.mode) {
		if !textSent && m.mode != "empty_stop" {
			out = append(out, object{"type": "text", "text": corruptionText(m.mode)})
		}
		v["stop_reason"] = "end_turn"
		changed = true
	}
	if changed {
		v["content"] = out
	}
	return changed
}
func (m *responseMutation) responseItem(v object) bool {
	kind := str(v["type"])
	if isToolCorruption(m.mode) {
		if kind == "function_call" {
			return m.tool(v)
		}
		return false
	}
	if kind != "message" && kind != "function_call" {
		return false
	}
	if kind == "function_call" {
		v["type"] = "message"
		v["role"] = "assistant"
		delete(v, "name")
		delete(v, "arguments")
		delete(v, "call_id")
	}
	parts := list(v["content"])
	out := []interface{}{}
	sent := false
	for _, p := range parts {
		b := obj(p)
		if str(b["type"]) == "output_text" {
			b["text"] = ""
			if !sent {
				b["text"] = corruptionText(m.mode)
				sent = true
			}
		}
		out = append(out, p)
	}
	if !sent {
		out = append(out, object{"type": "output_text", "text": corruptionText(m.mode), "annotations": []interface{}{}})
	}
	v["content"] = out
	return true
}
func (m *responseMutation) response(v object) bool {
	if v == nil {
		return false
	}
	changed := false
	out := []interface{}{}
	for _, value := range list(v["output"]) {
		item := obj(value)
		kind := str(item["type"])
		if m.mode == "empty_stop" && (kind == "message" || kind == "function_call") {
			changed = true
			continue
		}
		changed = m.responseItem(item) || changed
		out = append(out, value)
	}
	if changed {
		v["output"] = out
	}
	return changed
}
func (m *responseMutation) json(raw []byte) ([]byte, bool) {
	v := decodeObject(raw)
	if v == nil {
		return raw, false
	}
	changed := false
	switch m.path {
	case "/v1/messages":
		changed = m.messageBlocks(v)
	case "/v1/responses":
		changed = m.response(v)
	default:
		for _, c := range list(v["choices"]) {
			changed = m.chatMessage(obj(c)) || changed
		}
	}
	if !changed {
		return raw, false
	}
	return encodeFault(v), true
}

// Keep SSE event/id/retry/comment lines. Only the data payload is re-encoded.
// Unknown JSON fields and numbers are retained, including large numeric IDs.
func frameObject(raw []byte) object {
	var parts []string
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "data:") {
			parts = append(parts, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return decodeObject([]byte(strings.Join(parts, "\n")))
}
func frameWith(raw []byte, v object) []byte {
	newline := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	out := []string{}
	written := false
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			if !written {
				out = append(out, "data: "+string(encodeFault(v)))
				written = true
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			original := frameObject(raw)
			if oldType, newType := str(original["type"]), str(v["type"]); oldType != "" && newType != "" && strings.TrimSpace(strings.TrimPrefix(line, "event:")) == oldType {
				line = "event: " + newType
			}
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, newline))
}
func choiceKey(c object) string  { return string(encodeFault(c["index"])) }
func toolKey(c, t object) string { return choiceKey(c) + ":" + string(encodeFault(t["index"])) }
func (m *responseMutation) sse(raw []byte) ([][]byte, bool) {
	v := frameObject(raw)
	if m.path == "/v1/chat/completions" && isToolCorruption(m.mode) {
		tool, finish := false, false
		for _, x := range list(v["choices"]) {
			c := obj(x)
			tool = tool || len(list(obj(c["delta"])["tool_calls"])) > 0
			finish = finish || c["finish_reason"] != nil
		}
		if tool || len(m.queued) > 0 {
			m.queued = append(m.queued, raw)
			if finish || bytes.Contains(raw, []byte("[DONE]")) {
				return m.drain()
			}
			return nil, false
		}
		return [][]byte{raw}, false
	}
	if v == nil {
		return [][]byte{raw}, false
	}
	var changed, drop bool
	switch m.path {
	case "/v1/messages":
		changed, drop = m.messageEvent(v)
	case "/v1/responses":
		changed, drop = m.responseEvent(v)
	default:
		changed = m.chatEvent(v)
	}
	if drop {
		return nil, true
	}
	if changed {
		return [][]byte{frameWith(raw, v)}, true
	}
	return [][]byte{raw}, false
}
func (m *responseMutation) chatEvent(v object) bool {
	changed := false
	for _, x := range list(v["choices"]) {
		c := obj(x)
		d := obj(c["delta"])
		if d == nil {
			continue
		}
		key := choiceKey(c)
		_, hasText := d["content"]
		hasTool := len(list(d["tool_calls"])) > 0
		terminal := c["finish_reason"] != nil
		if hasText || hasTool || terminal {
			if hasText || hasTool || (!m.seen[key] && m.mode != "empty_stop") {
				d["content"] = ""
				if !m.seen[key] {
					d["content"] = corruptionText(m.mode)
					m.seen[key] = true
				}
			}
			delete(d, "tool_calls")
			if terminal {
				c["finish_reason"] = "stop"
			}
			changed = true
		}
	}
	return changed
}
func (m *responseMutation) drain() ([][]byte, bool) {
	if len(m.queued) == 0 {
		return nil, false
	}
	queue := m.queued
	m.queued = nil
	names := map[string]string{}
	count := map[string]int{}
	for _, raw := range queue {
		v := frameObject(raw)
		for _, x := range list(v["choices"]) {
			c := obj(x)
			for _, t := range list(obj(c["delta"])["tool_calls"]) {
				tc := obj(t)
				k := toolKey(c, tc)
				names[k] += str(obj(tc["function"])["name"])
				count[k]++
			}
		}
	}
	seen := map[string]bool{}
	arguments := map[string]bool{}
	out := [][]byte{}
	changed := false
	for _, raw := range queue {
		v := frameObject(raw)
		if v == nil {
			out = append(out, raw)
			continue
		}
		modified := false
		var extras []object
		for _, x := range list(v["choices"]) {
			c := obj(x)
			d := obj(c["delta"])
			var continuations []interface{}
			for _, value := range list(d["tool_calls"]) {
				tc := obj(value)
				fn := obj(tc["function"])
				if fn == nil {
					continue
				}
				k := toolKey(c, tc)
				first := !seen[k]
				seen[k] = true
				if m.mode == "xml_tool_name" {
					if first {
						fn["name"] = xmlToolName(names[k])
					} else {
						delete(fn, "name")
					}
				}
				if first {
					fn["arguments"] = ""
					if count[k] == 1 {
						continuation := clone(tc)
						delete(continuation, "id")
						continuation["function"] = object{"arguments": faultArguments(m.mode)}
						continuations = append(continuations, continuation)
						arguments[k] = true
					}
				} else if !arguments[k] {
					fn["arguments"] = faultArguments(m.mode)
					arguments[k] = true
				} else {
					fn["arguments"] = ""
				}
				modified = true
			}
			if len(continuations) > 0 {
				cc := clone(c)
				cc["delta"] = object{"tool_calls": continuations}
				e := clone(v)
				delete(e, "usage")
				e["choices"] = []interface{}{cc}
				extras = append(extras, e)
				if c["finish_reason"] != nil {
					c["finish_reason"] = nil
				}
			}
		}
		if modified {
			out = append(out, frameWith(raw, v))
			changed = true
		} else {
			out = append(out, raw)
		}
		for _, extra := range extras {
			// A continuation has no invented SSE event ID. Response and call references
			// come from the original event; name/id are not repeated inside tool_calls.
			extraRaw := frameWith(raw, extra)
			lines := strings.Split(string(extraRaw), "\n")
			clean := []string{}
			for _, line := range lines {
				if !strings.HasPrefix(line, "id:") {
					clean = append(clean, line)
				}
			}
			out = append(out, []byte(strings.Join(clean, "\n")))
		}
	}
	return out, changed
}
func (m *responseMutation) messageEvent(v object) (bool, bool) {
	kind := str(v["type"])
	key := string(encodeFault(v["index"]))
	if kind == "message_start" {
		msg := obj(v["message"])
		if len(list(msg["content"])) > 0 {
			return m.messageBlocks(msg), false
		}
		return false, false
	}
	if kind == "content_block_start" {
		b := obj(v["content_block"])
		t := str(b["type"])
		m.blocks[key] = t
		if isToolCorruption(m.mode) {
			if t != "tool_use" {
				return false, false
			}
			if m.mode == "xml_tool_name" {
				b["name"] = xmlToolName(str(b["name"]))
			}
			b["input"] = object{}
			return true, false
		}
		if t == "text" || t == "tool_use" {
			if m.mode == "empty_stop" {
				return true, true
			}
			b["type"] = "text"
			delete(b, "name")
			delete(b, "input")
			b["text"] = corruptionText(m.mode)
			m.seen[key] = true
			return true, false
		}
	}
	if kind == "content_block_delta" {
		d := obj(v["delta"])
		if isToolCorruption(m.mode) {
			if str(d["type"]) == "input_json_delta" {
				d["partial_json"] = ""
				if !m.seen[key] {
					d["partial_json"] = faultArguments(m.mode)
					m.seen[key] = true
				}
				return true, false
			}
			return false, false
		}
		if m.blocks[key] == "text" || m.blocks[key] == "tool_use" {
			if m.mode == "empty_stop" {
				return true, true
			}
			d["type"] = "text_delta"
			delete(d, "partial_json")
			d["text"] = ""
			return true, false
		}
	}
	if kind == "content_block_stop" && m.mode == "empty_stop" && (m.blocks[key] == "text" || m.blocks[key] == "tool_use") {
		return true, true
	}
	if kind == "message_delta" && !isToolCorruption(m.mode) {
		if d := obj(v["delta"]); d != nil {
			d["stop_reason"] = "end_turn"
			return true, false
		}
	}
	return false, false
}
func (m *responseMutation) responseEvent(v object) (bool, bool) {
	kind := str(v["type"])
	if r := obj(v["response"]); r != nil {
		return m.response(r), false
	}
	if item := obj(v["item"]); item != nil {
		t := str(item["type"])
		id := str(item["id"])
		m.blocks[id] = t
		if m.mode == "empty_stop" && (t == "message" || t == "function_call") {
			return true, true
		}
		changed := m.responseItem(item)
		if changed && strings.HasSuffix(kind, ".added") {
			if isToolCorruption(m.mode) {
				item["arguments"] = ""
			} else {
				for _, p := range list(item["content"]) {
					if part := obj(p); str(part["type"]) == "output_text" {
						part["text"] = ""
					}
				}
			}
		}
		return changed, false
	}
	id := str(v["item_id"])
	if isToolCorruption(m.mode) {
		if kind == "response.function_call_arguments.delta" {
			v["delta"] = ""
			if !m.seen[id] {
				v["delta"] = faultArguments(m.mode)
				m.seen[id] = true
			}
			return true, false
		}
		if kind == "response.function_call_arguments.done" {
			v["arguments"] = faultArguments(m.mode)
			if m.mode == "xml_tool_name" {
				if name, ok := v["name"].(string); ok {
					v["name"] = xmlToolName(name)
				}
			}
			return true, false
		}
		return false, false
	}
	output := m.blocks[id] == "message" || m.blocks[id] == "function_call"
	if m.mode == "empty_stop" && output {
		return true, true
	}
	if kind == "response.output_text.delta" || kind == "response.function_call_arguments.delta" {
		v["type"] = "response.output_text.delta"
		v["content_index"] = json.Number("0")
		v["delta"] = ""
		if !m.seen[id] {
			v["delta"] = corruptionText(m.mode)
			m.seen[id] = true
		}
		return true, false
	}
	if kind == "response.output_text.done" || kind == "response.function_call_arguments.done" {
		v["type"] = "response.output_text.done"
		v["content_index"] = json.Number("0")
		delete(v, "arguments")
		delete(v, "name")
		v["text"] = corruptionText(m.mode)
		return true, false
	}
	if part := obj(v["part"]); part != nil && str(part["type"]) == "output_text" {
		part["text"] = ""
		if strings.HasSuffix(kind, ".done") {
			part["text"] = corruptionText(m.mode)
		}
		return true, false
	}
	return false, false
}
