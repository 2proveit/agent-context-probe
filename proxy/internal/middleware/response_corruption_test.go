package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	rawFrames := []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"remote reasoning"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"","tool_calls":[{"index":0,"id":"call_remote","type":"function","function":{"name":"bash","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"","tool_calls":[{"index":0,"type":"function","function":{"arguments":"{\"command\":\"pwd\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":71,"completion_tokens":19,"completion_tokens_details":{"reasoning_tokens":12}}}`,
	}
	var source bytes.Buffer
	for _, raw := range rawFrames {
		v := decodeObject([]byte(raw))
		v["id"] = "remote_response"
		v["model"] = "remote-model"
		v["created"] = json.Number("123456789")
		v["vendor_field"] = json.Number("9007199254740993")
		source.WriteString("id: remote-event\r\ndata: " + string(encodeFault(v)) + "\r\n\r\n")
	}
	source.WriteString("data: [DONE]\r\n\r\n")
	handler := ResponseCorruption(config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 1, Modes: []string{"xml_tool_name"}}, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Arbitrary write boundaries, including inside UTF-8/CRLF and JSON, must
		// not be mistaken for SSE event boundaries.
		for _, b := range source.Bytes() {
			_, _ = w.Write([]byte{b})
		}
	}))
	handler.ServeHTTP(recorder, req)
	if len(recorder.frames) != 6 || recorder.flushes != 6 {
		t.Fatalf("want 6 event writes/flushes, got %d/%d", len(recorder.frames), recorder.flushes)
	}
	for i, raw := range recorder.frames[:5] {
		v := frameObject(raw)
		if v["id"] != "remote_response" || v["model"] != "remote-model" || v["created"] != json.Number("123456789") || v["vendor_field"] != json.Number("9007199254740993") {
			t.Fatalf("lost remote metadata: %s", raw)
		}
		if !bytes.HasPrefix(raw, []byte("id: remote-event\r\n")) {
			t.Fatal("lost SSE metadata")
		}
		if i == 1 {
			tc := obj(list(obj(obj(list(v["choices"])[0])["delta"])["tool_calls"])[0])
			fn := obj(tc["function"])
			if tc["id"] != "call_remote" || fn["name"] != xmlToolName("bash") || fn["arguments"] != "" {
				t.Fatalf("changed reference: %v", tc)
			}
		}
		if i == 2 {
			tc := obj(list(obj(obj(list(v["choices"])[0])["delta"])["tool_calls"])[0])
			fn := obj(tc["function"])
			if tc["id"] != nil || fn["name"] != nil || fn["arguments"] != "{}" {
				t.Fatalf("bad continuation: %v", tc)
			}
		}
		if i == 4 && obj(v["usage"])["prompt_tokens"] != json.Number("71") {
			t.Fatal("lost real usage")
		}
	}
	if string(recorder.frames[5]) != "data: [DONE]\r\n\r\n" {
		t.Fatal("terminal frame changed")
	}
}

func TestFaultPreservesAbsentUsageAndIncompleteSSE(t *testing.T) {
	for _, mode := range []string{"empty_stop", "repetition", "pseudo_tags", "xml_tool_name", "invalid_tool_arguments"} {
		m := newResponseMutation("/v1/chat/completions", mode)
		raw := []byte(`{"id":"remote","created":14,"model":"provider-model","extra":{"value":12},"choices":[{"index":0,"message":{"role":"assistant","content":"text","tool_calls":[{"id":"tc_remote","function":{"name":"bash","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		out, _ := m.json(raw)
		v := decodeObject(out)
		if _, ok := v["usage"]; ok {
			t.Fatal("invented usage")
		}
		if v["id"] != "remote" || v["model"] != "provider-model" || v["created"] != json.Number("14") || obj(v["extra"])["value"] != json.Number("12") {
			t.Fatal("lost metadata")
		}
		if isToolCorruption(mode) {
			tc := obj(list(obj(obj(list(v["choices"])[0])["message"])["tool_calls"])[0])
			if tc["id"] != "tc_remote" {
				t.Fatal("lost call ID")
			}
		}
	}
}

func TestXMLToolNameMultipleFragmentedCalls(t *testing.T) {
	m := newResponseMutation("/v1/chat/completions", "xml_tool_name")
	frames := []string{
		`{"id":"response","created":81,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_A","type":"function","function":{"name":"ba","arguments":""}},{"index":1,"id":"call_B","type":"function","function":{"name":"re","arguments":""}}]}}]}`,
		`{"id":"response","created":81,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"sh","arguments":""}},{"index":1,"function":{"name":"ad","arguments":""}}]}}]}`,
		`{"id":"response","created":81,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"x\":"}},{"index":1,"function":{"arguments":"{\"y\":"}}]}}]}`,
		`{"id":"response","created":81,"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}},{"index":1,"function":{"arguments":"2}"}}]}}]}`,
		`{"id":"response","created":81,"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	var output [][]byte
	for _, frame := range frames {
		out, _ := m.sse([]byte("data: " + frame + "\n\n"))
		output = append(output, out...)
	}
	names, args, ids := map[string]string{}, map[string]string{}, map[string]string{}
	for _, raw := range output {
		event := frameObject(raw)
		if event["id"] != "response" || event["created"] != json.Number("81") {
			t.Fatal("lost response metadata")
		}
		if _, ok := event["usage"]; ok {
			t.Fatal("invented missing usage")
		}
		for _, x := range list(event["choices"]) {
			c := obj(x)
			for _, value := range list(obj(c["delta"])["tool_calls"]) {
				tc := obj(value)
				k := string(encodeFault(tc["index"]))
				fn := obj(tc["function"])
				names[k] += str(fn["name"])
				args[k] += str(fn["arguments"])
				ids[k] += str(tc["id"])
			}
		}
	}
	if names["0"] != xmlToolName("bash") || names["1"] != xmlToolName("read") || args["0"] != "{}" || args["1"] != "{}" || ids["0"] != "call_A" || ids["1"] != "call_B" {
		t.Fatalf("inconsistent references/names: %v %v %v", names, args, ids)
	}
}

func TestFaultDoesNotCompleteIncompleteRemoteStream(t *testing.T) {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	raw := []byte("data: {\"id\":\"remote-unfinished\"")
	ResponseCorruption(config.ResponseCorruptionConfig{Enabled: true, EveryNRequests: 1, Modes: []string{"empty_stop"}}, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(raw)
	})).ServeHTTP(recorder, req)
	if !bytes.Equal(recorder.Body.Bytes(), raw) {
		t.Fatalf("invented completion/metadata: %s", recorder.Body.Bytes())
	}
}

func TestToolFaultPreservesNonToolSSEFrames(t *testing.T) {
	for _, mode := range []string{"xml_tool_name", "invalid_tool_arguments"} {
		t.Run(mode, func(t *testing.T) {
			mutation := newResponseMutation("/v1/chat/completions", mode)
			for _, raw := range []string{
				"data: {\"id\":\"remote\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":null,\"reasoning_content\":\"\"},\"finish_reason\":null}],\"usage\":null}\n\n",
				"data: {\"id\":\"remote\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"healthy\"},\"finish_reason\":\"stop\"}]}\n\n",
				"data: [DONE]\n\n",
			} {
				frames, changed := mutation.sse([]byte(raw))
				if changed || len(frames) != 1 || string(frames[0]) != raw {
					t.Fatalf("non-tool frame changed: %q", frames)
				}
			}
		})
	}
}

func TestToolFaultSingleFrameMultipleCalls(t *testing.T) {
	m := newResponseMutation("/v1/chat/completions", "xml_tool_name")
	raw := []byte("data: {\"id\":\"remote\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"reason once\",\"tool_calls\":[{\"index\":0,\"id\":\"call_a\",\"function\":{\"name\":\"bash\",\"arguments\":\"{}\"}},{\"index\":1,\"id\":\"call_b\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
	frames, changed := m.sse(raw)
	if !changed || len(frames) != 2 {
		t.Fatalf("expected name frame and one arguments frame, got %q", frames)
	}
	first := obj(list(frameObject(frames[0])["choices"])[0])
	last := obj(list(frameObject(frames[1])["choices"])[0])
	if first["finish_reason"] != nil || last["finish_reason"] != "tool_calls" {
		t.Fatal("finish must follow all tool continuations")
	}
	delta := obj(last["delta"])
	if _, ok := delta["reasoning_content"]; ok {
		t.Fatal("continuation duplicated reasoning")
	}
	if len(list(delta["tool_calls"])) != 2 {
		t.Fatal("continuation lost a tool")
	}
}

func TestResponsesFaultKeepsEventNameAndItemReferencesConsistent(t *testing.T) {
	m := newResponseMutation("/v1/responses", "repetition")
	frames, _ := m.sse([]byte("event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"sequence_number\":7,\"output_index\":0,\"item\":{\"id\":\"fc_remote\",\"call_id\":\"call_remote\",\"type\":\"function_call\",\"name\":\"bash\",\"arguments\":\"\"}}\n\n"))
	item := obj(frameObject(frames[0])["item"])
	if item["id"] != "fc_remote" || obj(list(item["content"])[0])["text"] != "" {
		t.Fatal("added item must keep its ID and wait for text deltas")
	}
	frames, _ = m.sse([]byte("event: response.function_call_arguments.delta\nid: event-8\ndata: {\"type\":\"response.function_call_arguments.delta\",\"sequence_number\":8,\"item_id\":\"fc_remote\",\"output_index\":0,\"delta\":\"{}\"}\n\n"))
	v := frameObject(frames[0])
	if !strings.Contains(string(frames[0]), "event: response.output_text.delta\n") || !strings.Contains(string(frames[0]), "id: event-8\n") || v["item_id"] != "fc_remote" || str(v["delta"]) != corruptionText("repetition") {
		t.Fatalf("inconsistent event or reference: %s", frames[0])
	}
}
