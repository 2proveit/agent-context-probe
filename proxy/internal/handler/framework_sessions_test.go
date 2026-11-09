package handler

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
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

func TestFrameworkIdentityAliasPrecedence(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string][]string
		want    string
	}{
		{"thread-over-conversation", map[string][]string{"X-Thread-Id": {"child"}, "X-Conversation-Id": {"root"}}, "child"},
		{"native-thread-over-custom-thread", map[string][]string{"Thread-Id": {"native"}, "X-Thread-Id": {"custom"}}, "native"},
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
