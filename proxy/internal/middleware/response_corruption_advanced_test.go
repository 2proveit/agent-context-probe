package middleware

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArgumentPrefixAcrossEveryRuneBoundary(t *testing.T) {
	for _, original := range []string{`{}`, `{"中文":"转义\\\"以及{}[]", "nested":[1,{"a":2}]}`, `["x",{}]`, ` {"content":"long HTML", "filePath":"report.html"} `} {
		want := truncatedArguments(original)
		if !strings.HasPrefix(original, want) || json.Valid([]byte(want)) {
			t.Fatalf("not an incomplete prefix: %q", want)
		}
		runes := []rune(original)
		for split := 0; split <= len(runes); split++ {
			p := &argumentPrefix{}
			got := p.take(string(runes[:split])) + p.take(string(runes[split:])) + p.take(" ignored after close")
			if got != want {
				t.Fatalf("split %d: got %q want %q", split, got, want)
			}
		}
	}
}
func TestAdvancedJSONPreservesNonTargetFields(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
		for _, mode := range []string{"reasoning_repetition", "truncated_tool_arguments"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				raw := `{"id":"remote","created":9007199254740993,"vendor":{"keep":true},"choices":[{"index":0,"finish_reason":"tool_calls","message":{"content":"visible","reasoning_content":"thinking","tool_calls":[{"id":"call_remote","function":{"name":"write","arguments":"{\"x\":1}"}}]}}]}`
				if path == "/v1/messages" {
					raw = `{"id":"remote","created":9007199254740993,"vendor":{"keep":true},"stop_reason":"tool_use","content":[{"type":"thinking","thinking":"think","signature":"sig"},{"type":"text","text":"visible"},{"type":"tool_use","id":"call_remote","name":"write","input":{"x":1}}]}`
				}
				if path == "/v1/responses" {
					raw = `{"id":"remote","created":9007199254740993,"vendor":{"keep":true},"status":"completed","output":[{"type":"reasoning","id":"rs_remote","summary":[{"type":"summary_text","text":"think"}],"encrypted_content":"opaque"},{"type":"message","content":[{"type":"output_text","text":"visible"}]},{"type":"function_call","id":"fc_remote","call_id":"call_remote","name":"write","arguments":"{\"x\":1}"}]}`
				}
				out, changed := newResponseMutation(path, mode).json([]byte(raw))
				if !changed {
					t.Fatal("not applied")
				}
				v := decodeObject(out)
				if v["id"] != "remote" || string(v["created"].(json.Number)) != "9007199254740993" || obj(v["vendor"])["keep"] != true {
					t.Fatalf("metadata changed: %s", out)
				}
				if _, ok := v["usage"]; ok {
					t.Fatal("invented usage")
				}
				for _, keep := range []string{"visible", "call_remote", "write"} {
					if !strings.Contains(string(out), keep) {
						t.Fatalf("lost %s", keep)
					}
				}
				switch path {
				case "/v1/chat/completions":
					c := obj(list(v["choices"])[0])
					msg := obj(c["message"])
					f := obj(obj(list(msg["tool_calls"])[0])["function"])
					if mode == "truncated_tool_arguments" {
						if json.Valid([]byte(str(f["arguments"]))) || c["finish_reason"] != "length" || msg["reasoning_content"] != "thinking" {
							t.Fatal("wrong tool fault")
						}
					} else if c["finish_reason"] != "tool_calls" || !strings.Contains(str(msg["reasoning_content"]), reasoningRepeatUnit) || f["arguments"] != `{"x":1}` {
						t.Fatal("wrong reasoning fault")
					}
				case "/v1/messages":
					if obj(list(v["content"])[0])["signature"] != "sig" {
						t.Fatal("lost signature")
					}
					if mode == "truncated_tool_arguments" && v["stop_reason"] != "max_tokens" {
						t.Fatal("wrong Messages stop")
					}
				case "/v1/responses":
					if obj(list(v["output"])[0])["encrypted_content"] != "opaque" {
						t.Fatal("lost opaque metadata")
					}
					if mode == "truncated_tool_arguments" && (v["status"] != "incomplete" || obj(v["incomplete_details"])["reason"] != "max_output_tokens") {
						t.Fatal("wrong Responses status")
					}
				}
			})
		}
	}
}
func emitAdvanced(t *testing.T, m *responseMutation, v object) object {
	t.Helper()
	frames, changed := m.sse(append(append([]byte("data: "), encodeFault(v)...), []byte("\n\n")...))
	if len(frames) != 1 {
		t.Fatalf("expected live unbuffered frame: %q", frames)
	}
	if !changed {
		return frameObject(frames[0])
	}
	return frameObject(frames[0])
}
func TestReasoningRepetitionStreamsAndPreservesOtherChannels(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			m := newResponseMutation(path, "reasoning_repetition")
			if path == "/v1/messages" {
				emitAdvanced(t, m, object{"type": "content_block_start", "index": 2, "content_block": object{"type": "thinking", "thinking": ""}})
			}
			for i := 0; i < 2; i++ {
				v := object{"id": "remote", "choices": []interface{}{object{"index": 0, "delta": object{"reasoning_content": "original", "content": "keep"}}}}
				if path == "/v1/messages" {
					v = object{"type": "content_block_delta", "index": 2, "delta": object{"type": "thinking_delta", "thinking": "original"}}
				}
				if path == "/v1/responses" {
					v = object{"type": "response.reasoning_summary_text.delta", "sequence_number": i, "item_id": "rs_remote", "summary_index": 0, "delta": "original"}
				}
				out := emitAdvanced(t, m, v)
				if !strings.Contains(string(encodeFault(out)), reasoningRepeatUnit) {
					t.Fatal("missing repetition")
				}
				if _, ok := out["usage"]; ok {
					t.Fatal("invented usage")
				}
				if path == "/v1/chat/completions" && obj(obj(list(out["choices"])[0])["delta"])["content"] != "keep" {
					t.Fatal("changed visible content")
				}
			}
			if path == "/v1/responses" {
				out := emitAdvanced(t, m, object{"type": "response.reasoning_summary_text.done", "item_id": "rs_remote", "summary_index": 0, "text": "original aggregate"})
				if out["text"] != strings.Repeat(reasoningRepeatUnit, 16) {
					t.Fatal("delta/done mismatch")
				}
				out = emitAdvanced(t, m, object{"type": "response.completed", "response": object{"id": "remote", "status": "completed", "output": []interface{}{object{"id": "rs_remote", "type": "reasoning", "summary": []interface{}{object{"type": "summary_text", "text": "original aggregate"}}}}}})
				item := obj(list(obj(out["response"])["output"])[0])
				if obj(list(item["summary"])[0])["text"] != strings.Repeat(reasoningRepeatUnit, 16) {
					t.Fatal("completed snapshot mismatch")
				}
			}
		})
	}
}
func TestTruncatedToolChatLiveInterleavedCallsAndRealUsage(t *testing.T) {
	m := newResponseMutation("/v1/chat/completions", "truncated_tool_arguments")
	args := map[int]string{}
	for _, part := range []struct {
		index    int
		fragment string
	}{{0, `{"中文":"`}, {1, `{"b":[`}, {0, `value"}`}, {1, `1,2]}`}} {
		v := object{"id": "remote", "created": 123, "choices": []interface{}{object{"index": 0, "delta": object{"reasoning_content": "keep", "tool_calls": []interface{}{object{"id": "real-call", "index": part.index, "function": object{"name": "write", "arguments": part.fragment}}}}, "finish_reason": nil}}}
		out := emitAdvanced(t, m, v)
		c := obj(list(out["choices"])[0])
		delta := obj(c["delta"])
		tc := obj(list(delta["tool_calls"])[0])
		args[part.index] += str(obj(tc["function"])["arguments"])
		if tc["id"] != "real-call" || delta["reasoning_content"] != "keep" {
			t.Fatal("changed unrelated fields")
		}
	}
	for _, a := range args {
		if json.Valid([]byte(a)) {
			t.Fatalf("valid arguments: %s", a)
		}
	}
	end := emitAdvanced(t, m, object{"id": "remote", "choices": []interface{}{object{"index": 0, "delta": object{}, "finish_reason": "tool_calls"}}, "usage": object{"completion_tokens": 0, "details": object{"reasoning_tokens": 23405}}})
	if obj(list(end["choices"])[0])["finish_reason"] != "length" || obj(end["usage"])["completion_tokens"] != json.Number("0") {
		t.Fatal("wrong terminal/usage")
	}
	frames, changed := m.sse([]byte("data: [DONE]\n\n"))
	if changed || string(frames[0]) != "data: [DONE]\n\n" {
		t.Fatal("changed DONE")
	}
}
func TestTruncatedArgumentsOtherProtocols(t *testing.T) {
	m := newResponseMutation("/v1/messages", "truncated_tool_arguments")
	emitAdvanced(t, m, object{"type": "content_block_start", "index": 3, "content_block": object{"type": "tool_use", "id": "call_remote", "name": "write", "input": object{}}})
	out := emitAdvanced(t, m, object{"type": "content_block_delta", "index": 3, "delta": object{"type": "input_json_delta", "partial_json": `{"x":1}`}})
	if obj(out["delta"])["partial_json"] != `{"x":1` {
		t.Fatal("wrong Messages prefix")
	}
	out = emitAdvanced(t, m, object{"type": "message_delta", "delta": object{"stop_reason": "tool_use"}, "usage": object{"output_tokens": 7}})
	if obj(out["delta"])["stop_reason"] != "max_tokens" {
		t.Fatal("wrong stop")
	}
	m = newResponseMutation("/v1/responses", "truncated_tool_arguments")
	emitAdvanced(t, m, object{"type": "response.output_item.added", "item": object{"type": "function_call", "id": "fc_remote", "call_id": "call_remote", "name": "write", "arguments": ""}})
	out = emitAdvanced(t, m, object{"type": "response.function_call_arguments.delta", "item_id": "fc_remote", "delta": `{"x":1}`})
	if out["delta"] != `{"x":1` || out["item_id"] != "fc_remote" {
		t.Fatal("wrong Responses prefix/reference")
	}
	raw := []byte("event: response.completed\nid: real-event\ndata: {\"type\":\"response.completed\",\"sequence_number\":9,\"response\":{\"id\":\"remote\",\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"id\":\"fc_remote\",\"call_id\":\"call_remote\",\"name\":\"write\",\"arguments\":\"{\\\"x\\\":1}\"}]}}\n\n")
	frames, _ := m.sse(raw)
	v := frameObject(frames[0])
	if v["type"] != "response.incomplete" || !strings.Contains(string(frames[0]), "event: response.incomplete\n") || !strings.Contains(string(frames[0]), "id: real-event\n") {
		t.Fatalf("inconsistent terminal: %s", frames[0])
	}
}
func TestAdvancedFaultNoTargetAndIncompleteStreamPassThrough(t *testing.T) {
	for _, mode := range []string{"reasoning_repetition", "truncated_tool_arguments"} {
		m := newResponseMutation("/v1/chat/completions", mode)
		for _, raw := range []string{"data: {\"id\":\"remote\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"keep\"},\"finish_reason\":\"stop\"}]}\n\n", "data: {\"id\":\"incomplete"} {
			frames, changed := m.sse([]byte(raw))
			if changed || len(frames) != 1 || string(frames[0]) != raw {
				t.Fatal("changed absent target/incomplete frame")
			}
		}
		if frames, changed := m.drain(); changed || len(frames) > 0 {
			t.Fatal("invented terminal")
		}
	}
}

func TestTruncatedEmptyInputMessageKeepsBlockReferences(t *testing.T) {
	m := newResponseMutation("/v1/messages", "truncated_tool_arguments")
	emitAdvanced(t, m, object{"type": "content_block_start", "index": 3, "content_block": object{"type": "tool_use", "id": "call_remote", "name": "read", "input": object{}}})
	raw := []byte("event: content_block_stop\nid: stop-3\ndata: {\"type\":\"content_block_stop\",\"index\":3}\n\n")
	frames, changed := m.sse(raw)
	if !changed || len(frames) != 2 || strings.Contains(string(frames[0]), "id: stop-3") || string(frames[1]) != string(raw) {
		t.Fatalf("invalid inserted delta: %q", frames)
	}
	v := frameObject(frames[0])
	if v["index"] != json.Number("3") || obj(v["delta"])["partial_json"] != "{" || !strings.Contains(string(frames[0]), "event: content_block_delta") {
		t.Fatal("block reference mismatch")
	}
}
