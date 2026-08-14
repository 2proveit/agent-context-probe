package handler

import (
	"encoding/json"
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/model"
)

func TestBuildSessionGroupsLinksTaskChildAndResult(t *testing.T) {
	requests := []model.RequestLog{
		{
			RequestID: "parent-1",
			Timestamp: "2026-08-13T11:22:35+08:00",
			Headers: map[string][]string{
				"X-Session-Affinity": {"session-parent"},
			},
			Body: map[string]interface{}{
				"model": "iFinD-Atlas",
				"messages": []interface{}{
					map[string]interface{}{"role": "user", "content": "Create a company report"},
				},
			},
			Response: responseLog(t, map[string]interface{}{
				"choices": []interface{}{
					map[string]interface{}{
						"finish_reason": "tool_calls",
						"message": map[string]interface{}{
							"tool_calls": []interface{}{
								map[string]interface{}{
									"id": "call-task-1",
									"function": map[string]interface{}{
										"name":      "task",
										"arguments": `{"description":"Collect evidence","prompt":"child prompt","subagent_type":"data-collector"}`,
									},
								},
							},
						},
					},
				},
				"usage": map[string]interface{}{"prompt_tokens": 100.0, "completion_tokens": 20.0},
			}, 1200),
		},
		{
			RequestID: "child-1",
			Timestamp: "2026-08-13T11:23:00+08:00",
			Headers: map[string][]string{
				"X-Session-Affinity":  {"session-child"},
				"X-Parent-Session-Id": {"session-parent"},
			},
			Body: map[string]interface{}{
				"model": "deepseek-v4-flash",
				"messages": []interface{}{
					map[string]interface{}{"role": "system", "content": "You collect data"},
					map[string]interface{}{"role": "user", "content": "child prompt"},
				},
			},
			Response: responseLog(t, map[string]interface{}{
				"choices": []interface{}{
					map[string]interface{}{"finish_reason": "stop", "message": map[string]interface{}{"content": "done"}},
				},
				"usage": map[string]interface{}{"prompt_tokens": 50.0, "completion_tokens": 10.0},
			}, 800),
		},
		{
			RequestID: "parent-2",
			Timestamp: "2026-08-13T11:25:11+08:00",
			Headers: map[string][]string{
				"X-Session-Affinity": {"session-parent"},
			},
			Body: map[string]interface{}{
				"model": "iFinD-Atlas",
				"messages": []interface{}{
					map[string]interface{}{"role": "user", "content": "Create a company report"},
					map[string]interface{}{"role": "tool", "tool_call_id": "call-task-1", "content": "Expected object at state.input"},
				},
			},
			Response: responseLog(t, map[string]interface{}{
				"choices": []interface{}{
					map[string]interface{}{"finish_reason": "stop", "message": map[string]interface{}{"content": "retrying"}},
				},
				"usage": map[string]interface{}{"prompt_tokens": 150.0, "completion_tokens": 15.0},
			}, 900),
		},
		{
			RequestID: "memory-1",
			Timestamp: "2026-08-13T14:35:13+08:00",
			Headers: map[string][]string{
				"X-Session-Affinity":     {"session-memory"},
				"X-Lumi-Session-Purpose": {"memory-maintenance"},
			},
			Body: map[string]interface{}{
				"model":    "iFinD-Atlas",
				"messages": []interface{}{map[string]interface{}{"role": "user", "content": "maintain memory"}},
			},
			Response: responseLog(t, map[string]interface{}{
				"choices": []interface{}{
					map[string]interface{}{"finish_reason": "stop", "message": map[string]interface{}{"content": "updated"}},
				},
			}, 400),
		},
	}

	roots, groups := buildSessionGroups(requests)
	if len(roots) != 2 {
		t.Fatalf("expected two root sessions, got %d", len(roots))
	}

	parent := groups["session-parent"]
	if parent == nil {
		t.Fatal("parent session was not grouped")
	}
	if parent.summary.RequestCount != 2 || parent.summary.InputTokens != 250 || parent.summary.OutputTokens != 35 {
		t.Fatalf("unexpected parent metrics: %+v", parent.summary)
	}
	if len(parent.summary.Children) != 1 {
		t.Fatalf("expected one child, got %d", len(parent.summary.Children))
	}
	child := parent.summary.Children[0]
	if child.SessionID != "session-child" || child.TaskCallID != "call-task-1" {
		t.Fatalf("unexpected child linkage: %+v", child)
	}
	if child.AgentName != "data-collector" || child.Title != "Collect evidence" {
		t.Fatalf("unexpected child label: %+v", child)
	}
	if child.Status != "error" || child.ResultMessage != "Expected object at state.input" {
		t.Fatalf("unexpected child result: %+v", child)
	}

	memory := groups["session-memory"]
	if memory == nil || memory.summary.Kind != "memory-maintenance" || memory.summary.Title != "Memory maintenance" {
		t.Fatalf("unexpected memory session: %+v", memory)
	}

	detail := buildSessionDetail(parent, groups)
	if len(detail.Requests) != 2 || len(detail.Children) != 1 || len(detail.Children[0].Requests) != 1 {
		t.Fatalf("unexpected session detail shape: %+v", detail)
	}
}

func responseLog(t *testing.T, body map[string]interface{}, responseTime int64) *model.ResponseLog {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return &model.ResponseLog{
		StatusCode:   200,
		Body:         encoded,
		ResponseTime: responseTime,
		IsStreaming:  true,
	}
}
