package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/config"
	"github.com/seifghazi/claude-code-monitor/internal/model"
	"github.com/seifghazi/claude-code-monitor/internal/service"
)

// These are proxy integration contracts using each client's documented custom
// header facility, not live executions of the third-party agent runtimes.
func TestFrameworkSessionCaptureContracts(t *testing.T) {
	clients := []struct{ name, endpoint, identity, parent string }{
		{"codex", "/v1/responses", "Thread-Id", "X-Codex-Parent-Thread-Id"},
		{"opencode", "/v1/chat/completions", "X-OpenCode-Session", "X-Parent-Session-Id"},
		{"claude-code", "/v1/messages", "X-Claude-Code-Session-Id", "X-Parent-Session-Id"},
		{"langchain-langgraph", "/v1/chat/completions", "X-Thread-Id", "X-Parent-Thread-Id"},
		{"openai-agents", "/v1/responses", "X-Session-Affinity", "X-Parent-Session-Id"},
		{"autogen", "/v1/chat/completions", "X-Session-Id", "X-Parent-Session-Id"},
		{"crewai", "/v1/chat/completions", "X-Conversation-Id", "X-Parent-Session-Id"},
	}
	for _, client := range clients {
		for _, fixture := range sqliteCaptureFixtures() {
			if fixture.endpoint != client.endpoint {
				continue
			}
			mode := "json"
			if fixture.stream {
				mode = "sse"
			}
			t.Run(client.name+"/"+mode, func(t *testing.T) {
				storage, err := service.NewSQLiteStorageService(&config.StorageConfig{DBPath: filepath.Join(t.TempDir(), "requests.db")})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = storage.Close() })
				h := newSQLiteCaptureHandler(storage, &sqliteCaptureForwarder{fixtures: []sqliteCaptureFixture{fixture}})
				for _, id := range []string{"root", "child", "root", "child"} {
					req := sqliteCaptureRequest(fixture)
					req.Header.Set(client.identity, id)
					if id == "child" {
						req.Header.Set(client.parent, "root")
					}
					if client.name == "codex" {
						req.Header.Set("Session-Id", "root")
					}
					recorder := httptest.NewRecorder()
					serveSQLiteCapture(h, recorder, req)
					if recorder.Code != 200 || recorder.Body.String() != fixture.raw {
						t.Fatalf("response changed: status=%d body=%q", recorder.Code, recorder.Body.String())
					}
				}
				recorder := httptest.NewRecorder()
				h.GetSessions(recorder, httptest.NewRequest("GET", "/api/sessions?session=root", nil))
				var result struct {
					Session sessionDetail `json:"session"`
				}
				if recorder.Code != 200 {
					t.Fatalf("session lookup failed: %d %s", recorder.Code, recorder.Body.String())
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Session.Requests) != 2 || len(result.Session.Children) != 1 || len(result.Session.Children[0].Requests) != 2 || result.Session.Children[0].Summary.SessionID != "child" {
					t.Fatalf("incorrect agent tree: %+v", result.Session)
				}
			})
		}
	}
}

func TestFrameworkWithoutTransportIdentityIsNotGuessed(t *testing.T) {
	requests := []model.RequestLog{
		{RequestID: "one", UserAgent: "langchain", Headers: map[string][]string{"User-Agent": {"langchain"}}, Body: map[string]interface{}{"model": "shared-model", "metadata": map[string]interface{}{"user_id": "shared-user", "thread_id": "internal-only"}}},
		{RequestID: "two", UserAgent: "langchain", Headers: map[string][]string{"User-Agent": {"langchain"}}, Body: map[string]interface{}{"model": "shared-model", "metadata": map[string]interface{}{"user_id": "shared-user", "thread_id": "internal-only"}}},
	}
	roots, _ := buildSessionGroups(requests)
	if len(roots) != 2 {
		t.Fatal("unidentified requests must remain separate")
	}
}

func TestDeepSeekHarnessSessionCapture(t *testing.T) {
	for _, fixture := range sqliteCaptureFixtures() {
		if fixture.endpoint != "/v1/messages" || !fixture.stream {
			continue
		}
		storage, err := service.NewSQLiteStorageService(&config.StorageConfig{DBPath: filepath.Join(t.TempDir(), "requests.db")})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = storage.Close() })
		handler := newSQLiteCaptureHandler(storage, &sqliteCaptureForwarder{fixtures: []sqliteCaptureFixture{fixture}})
		for _, sessionID := range []string{"dsh-first", "dsh-second", "dsh-first", "", ""} {
			request := sqliteCaptureRequest(fixture)
			request.Header.Set("X-DeepSeek-Harness-User-Id", "shared-harness-home")
			if sessionID != "" {
				request.Header["x-deepseek-harness-session-id"] = []string{sessionID}
			}
			recorder := httptest.NewRecorder()
			serveSQLiteCapture(handler, recorder, request)
			if recorder.Code != 200 || recorder.Body.String() != fixture.raw {
				t.Fatalf("response changed: status=%d body=%q", recorder.Code, recorder.Body.String())
			}
		}

		recorder := httptest.NewRecorder()
		handler.GetSessions(recorder, httptest.NewRequest("GET", "/api/sessions", nil))
		var result struct {
			Sessions []sessionSummary `json:"sessions"`
			Total    int              `json:"total"`
		}
		if recorder.Code != 200 {
			t.Fatalf("session listing failed: %d %s", recorder.Code, recorder.Body.String())
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		ungrouped := 0
		for _, session := range result.Sessions {
			counts[session.SessionID] = session.RequestCount
			if strings.HasPrefix(session.SessionID, "request:") && session.RequestCount == 1 {
				ungrouped++
			}
		}
		if result.Total != 4 || counts["dsh-first"] != 2 || counts["dsh-second"] != 1 || ungrouped != 2 {
			t.Fatalf("expected two distinct DSH sessions and two ungrouped requests, got %v", counts)
		}
	}
}

func TestDeepSeekHarnessSubagentCapture(t *testing.T) {
	for _, fixture := range sqliteCaptureFixtures() {
		if fixture.endpoint != "/v1/messages" || !fixture.stream {
			continue
		}
		storage, err := service.NewSQLiteStorageService(&config.StorageConfig{DBPath: filepath.Join(t.TempDir(), "requests.db")})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = storage.Close() })
		handler := newSQLiteCaptureHandler(storage, &sqliteCaptureForwarder{fixtures: []sqliteCaptureFixture{fixture}})
		calls := []struct{ sessionID, logSessionID, parentID, origin string }{
			{"root", "", "", ""},
			{"child", "", "", ""},
			{"grandchild", "grandchild", "child", "subagent"},
			{"child", "child", "root", "subagent"},
			{"fork", "fork", "root", ""},
			{"mismatched", "other", "root", "subagent"},
			{"orphan", "orphan", "missing", "subagent"},
			{"cycle-first", "cycle-first", "cycle-second", "subagent"},
			{"cycle-second", "cycle-second", "cycle-first", "subagent"},
		}
		for _, call := range calls {
			body := fmt.Sprintf(`{"model":%q,"stream":true,"max_tokens":10,"system":"Harness instructions","messages":[]`, fixture.model)
			if call.logSessionID != "" {
				body += fmt.Sprintf(`,"dsh_session_log":{
					"version":1,"sessionFormatVersion":2,
					"session":{"version":2,"id":%q,"parentSession":%q,"origin":%q,"createdAt":1780000000000,"cwd":"/private/example"},
					"afterSeq":-1,"throughSeq":0,
					"events":[{"type":"turn/start","seq":0,"time":1780000000001,"data":{"turn":1}}]
				}`, call.logSessionID, call.parentID, call.origin)
			}
			body += "}"
			request := httptest.NewRequest("POST", fixture.endpoint, strings.NewReader(body))
			request = request.WithContext(context.WithValue(request.Context(), model.BodyBytesKey, []byte(body)))
			request.Header.Set("X-DeepSeek-Harness-Session-Id", call.sessionID)
			recorder := httptest.NewRecorder()
			serveSQLiteCapture(handler, recorder, request)
			if recorder.Code != 200 || recorder.Body.String() != fixture.raw {
				t.Fatalf("response changed: status=%d body=%q", recorder.Code, recorder.Body.String())
			}
		}

		recorder := httptest.NewRecorder()
		handler.GetSessions(recorder, httptest.NewRequest("GET", "/api/sessions", nil))
		var listing struct {
			Sessions []sessionSummary `json:"sessions"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
			t.Fatal(err)
		}
		roots := make(map[string]sessionSummary)
		for _, summary := range listing.Sessions {
			roots[summary.SessionID] = summary
		}
		if len(roots) != 6 {
			t.Fatalf("unexpected top-level sessions: %v", roots)
		}
		for _, sessionID := range []string{"root", "fork", "mismatched", "orphan", "cycle-first", "cycle-second"} {
			if _, exists := roots[sessionID]; !exists {
				t.Fatalf("expected %q to remain a top-level session", sessionID)
			}
		}
		for _, sessionID := range []string{"cycle-first", "cycle-second"} {
			if summary := roots[sessionID]; summary.ParentSessionID != "" || len(summary.Children) != 0 {
				t.Fatalf("expected cyclic parent link to be discarded: %+v", summary)
			}
		}

		recorder = httptest.NewRecorder()
		handler.GetSessions(recorder, httptest.NewRequest("GET", "/api/sessions?session=root", nil))
		var detail struct {
			Session sessionDetail `json:"session"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		if len(detail.Session.Children) != 1 {
			t.Fatalf("expected one direct subagent, got %d", len(detail.Session.Children))
		}
		child := detail.Session.Children[0]
		if child.Summary.SessionID != "child" || child.Summary.ParentSessionID != "root" || child.Summary.Kind != "subagent" || len(child.Requests) != 2 || len(child.Children) != 1 {
			t.Fatalf("unexpected child: %+v", child.Summary)
		}
		grandchild := child.Children[0]
		if grandchild.Summary.SessionID != "grandchild" || grandchild.Summary.ParentSessionID != "child" || grandchild.Summary.Kind != "subagent" || len(grandchild.Requests) != 1 {
			t.Fatalf("unexpected grandchild: %+v", grandchild.Summary)
		}
		system := arrayValue(objectValue(child.Requests[0].Body)["system"])
		if len(system) != 1 || objectValue(system[0])["text"] != "Harness instructions" {
			t.Fatalf("string system prompt was lost in capture: %v", system)
		}
		if body := recorder.Body.String(); strings.Contains(body, "/private/example") || strings.Contains(body, "turn/start") {
			t.Fatal("capture retained unrelated DeepSeek Harness session log data")
		}
	}
}

func TestFrameworkIdentityAliasPrecedence(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string][]string
		want    string
	}{
		{"thread-over-conversation", map[string][]string{"X-Thread-Id": {"child"}, "X-Conversation-Id": {"root"}}, "child"},
		{"native-thread-over-custom-thread", map[string][]string{"Thread-Id": {"native"}, "X-Thread-Id": {"custom"}}, "native"},
		{"dsh-session-over-affinity", map[string][]string{"X-DeepSeek-Harness-Session-Id": {"dsh"}, "X-Session-Affinity": {"shared"}}, "dsh"},
		{"thread-over-dsh-session", map[string][]string{"Thread-Id": {"thread"}, "X-DeepSeek-Harness-Session-Id": {"dsh"}}, "thread"},
		{"blank-thread-falls-back", map[string][]string{"X-Thread-Id": {" "}, "X-Session-Id": {"session"}}, "session"},
		{"mixed-case-alias", map[string][]string{"x-conversation-id": {" conversation "}}, "conversation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roots, _ := buildSessionGroups([]model.RequestLog{{RequestID: "req", Headers: tc.headers}})
			if len(roots) != 1 || roots[0].SessionID != tc.want {
				t.Fatalf("expected %q, got %+v", tc.want, roots)
			}
		})
	}
}
