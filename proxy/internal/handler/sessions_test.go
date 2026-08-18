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
	if memory == nil || memory.summary.Kind != "root" || memory.summary.Title != "maintain memory" {
		t.Fatalf("unexpected memory session: %+v", memory)
	}
	if memory.summary.Purpose != "memory-maintenance" {
		t.Fatalf("expected memory purpose metadata to be preserved: %+v", memory.summary)
	}

	detail := buildSessionDetail(parent, groups)
	if len(detail.Requests) != 2 || len(detail.Children) != 1 || len(detail.Children[0].Requests) != 1 {
		t.Fatalf("unexpected session detail shape: %+v", detail)
	}
}

func TestBuildSessionGroupsCalculatesTreeElapsedTime(t *testing.T) {
	requests := []model.RequestLog{
		{
			RequestID: "root-1",
			Timestamp: "2026-08-13T10:00:00Z",
			Headers: map[string][]string{
				"X-Session-Affinity": {"session-root"},
			},
			Body: map[string]interface{}{"model": "iFinD-Atlas"},
			Response: responseLogAt(t, map[string]interface{}{
				"usage": map[string]interface{}{"input_tokens": 10.0, "output_tokens": 5.0},
			}, 1000, "2026-08-13T10:00:01Z"),
		},
		{
			RequestID: "child-1",
			Timestamp: "2026-08-13T10:00:02Z",
			Headers: map[string][]string{
				"X-Session-Affinity":  {"session-child"},
				"X-Parent-Session-Id": {"session-root"},
			},
			Body: map[string]interface{}{"model": "deepseek-v4-flash"},
			Response: responseLogAt(t, map[string]interface{}{
				"usage": map[string]interface{}{"input_tokens": 20.0, "output_tokens": 8.0},
			}, 4000, "2026-08-13T10:00:06Z"),
		},
	}

	_, groups := buildSessionGroups(requests)
	root := groups["session-root"]
	child := groups["session-child"]
	if root == nil || child == nil {
		t.Fatalf("expected root and child sessions, got %+v", groups)
	}
	if root.summary.ResponseTimeMs != 1000 {
		t.Fatalf("expected cumulative root model latency to remain 1000ms, got %d", root.summary.ResponseTimeMs)
	}
	if root.summary.ElapsedTimeMs != 6000 {
		t.Fatalf("expected root E2E to include child completion, got %dms", root.summary.ElapsedTimeMs)
	}
	if child.summary.ElapsedTimeMs != 4000 {
		t.Fatalf("expected child E2E to use its own observed span, got %dms", child.summary.ElapsedTimeMs)
	}
}

func TestBuildToolExecutionWindowsAcrossProtocols(t *testing.T) {
	requests := []model.RequestLog{
		{
			RequestID: "chat-source",
			Timestamp: "2026-08-13T10:00:00.100Z",
			Headers:   map[string][]string{"X-Session-Affinity": {"chat-session"}},
			Body:      map[string]interface{}{"model": "chat-model"},
			Response: responseLogAt(t, map[string]interface{}{
				"choices": []interface{}{map[string]interface{}{
					"message": map[string]interface{}{"tool_calls": []interface{}{
						map[string]interface{}{"id": "call-read", "function": map[string]interface{}{"name": "read", "arguments": `{}`}},
						map[string]interface{}{"id": "call-glob", "function": map[string]interface{}{"name": "glob", "arguments": `{}`}},
					}},
				}},
			}, 1400, "2026-08-13T10:00:01.500Z"),
		},
		{
			RequestID: "chat-result",
			Timestamp: "2026-08-13T10:00:04.900Z",
			Headers:   map[string][]string{"X-Session-Affinity": {"chat-session"}},
			Body: map[string]interface{}{"messages": []interface{}{
				map[string]interface{}{"role": "tool", "tool_call_id": "call-read", "content": "read result"},
				map[string]interface{}{"role": "tool", "tool_call_id": "call-glob", "content": "glob result"},
			}},
			Response: responseLogAt(t, map[string]interface{}{}, 100, "2026-08-13T10:00:05.000Z"),
		},
		{
			RequestID: "anthropic-source",
			Timestamp: "2026-08-13T10:01:10.050Z",
			Headers:   map[string][]string{"X-Session-Affinity": {"anthropic-session"}},
			Body:      map[string]interface{}{"model": "anthropic-model"},
			Response: responseLogAt(t, map[string]interface{}{
				"content": []interface{}{map[string]interface{}{
					"type": "tool_use", "id": "toolu-edit", "name": "edit", "input": map[string]interface{}{},
				}},
			}, 2200, "2026-08-13T10:01:12.250Z"),
		},
		{
			RequestID: "anthropic-result",
			Timestamp: "2026-08-13T10:01:15.750Z",
			Headers:   map[string][]string{"X-Session-Affinity": {"anthropic-session"}},
			Body: map[string]interface{}{"messages": []interface{}{map[string]interface{}{
				"role": "user", "content": []interface{}{map[string]interface{}{
					"type": "tool_result", "tool_use_id": "toolu-edit", "content": "edited",
				}},
			}}},
			Response: responseLogAt(t, map[string]interface{}{}, 100, "2026-08-13T10:01:15.850Z"),
		},
		{
			RequestID: "responses-source",
			Timestamp: "2026-08-13T10:02:20.000Z",
			Headers:   map[string][]string{"X-Session-Affinity": {"responses-session"}},
			Body:      map[string]interface{}{"model": "responses-model"},
			Response: responseLogAt(t, map[string]interface{}{
				"output": []interface{}{map[string]interface{}{
					"type": "function_call", "call_id": "call-shell", "name": "shell", "arguments": `{}`,
				}},
			}, 1125, "2026-08-13T10:02:21.125Z"),
		},
		{
			RequestID: "responses-result",
			Timestamp: "2026-08-13T10:02:22.625Z",
			Headers:   map[string][]string{"X-Session-Affinity": {"responses-session"}},
			Body: map[string]interface{}{"input": []interface{}{map[string]interface{}{
				"type": "function_call_output", "call_id": "call-shell", "output": "done",
			}}},
			Response: responseLogAt(t, map[string]interface{}{}, 100, "2026-08-13T10:02:22.725Z"),
		},
	}

	_, groups := buildSessionGroups(requests)
	tests := []struct {
		sessionID string
		requestID string
		duration  int64
		tools     int
	}{
		{sessionID: "chat-session", requestID: "chat-source", duration: 3400, tools: 2},
		{sessionID: "anthropic-session", requestID: "anthropic-source", duration: 3500, tools: 1},
		{sessionID: "responses-session", requestID: "responses-source", duration: 1500, tools: 1},
	}
	for _, test := range tests {
		t.Run(test.sessionID, func(t *testing.T) {
			detail := buildSessionDetail(groups[test.sessionID], groups)
			if len(detail.ToolWindows) != 1 {
				t.Fatalf("expected one tool window, got %+v", detail.ToolWindows)
			}
			window := detail.ToolWindows[0]
			if window.RequestID != test.requestID || !window.Complete || window.Approximate {
				t.Fatalf("unexpected window metadata: %+v", window)
			}
			if window.DurationMs != test.duration || len(window.ToolNames) != test.tools {
				t.Fatalf("unexpected tool window values: %+v", window)
			}
		})
	}
}

func TestBuildToolExecutionWindowsMarksLegacyAndMissingResults(t *testing.T) {
	legacy := model.RequestLog{
		RequestID: "legacy-source",
		Timestamp: "2026-08-13T10:00:00Z",
		Body:      map[string]interface{}{},
		Response: responseLogAt(t, map[string]interface{}{
			"content": []interface{}{map[string]interface{}{
				"type": "tool_use", "id": "toolu-read", "name": "read", "input": map[string]interface{}{},
			}},
		}, 1000, "2026-08-13T10:00:01Z"),
	}
	result := model.RequestLog{
		RequestID: "legacy-result",
		Timestamp: "2026-08-13T10:00:01Z",
		Body: map[string]interface{}{"messages": []interface{}{map[string]interface{}{
			"role": "user", "content": []interface{}{map[string]interface{}{
				"type": "tool_result", "tool_use_id": "toolu-read", "content": "done",
			}},
		}}},
	}
	missing := model.RequestLog{
		RequestID: "missing-source",
		Timestamp: "2026-08-13T10:00:02Z",
		Body:      map[string]interface{}{},
		Response: responseLogAt(t, map[string]interface{}{
			"output": []interface{}{map[string]interface{}{
				"type": "function_call", "call_id": "call-missing", "name": "shell", "arguments": `{}`,
			}},
		}, 1000, "2026-08-13T10:00:03Z"),
	}

	windows := buildToolExecutionWindows([]model.RequestLog{legacy, result, missing})
	if len(windows) != 2 {
		t.Fatalf("expected two tool windows, got %+v", windows)
	}
	if !windows[0].Complete || !windows[0].Approximate || windows[0].DurationMs != 0 {
		t.Fatalf("expected a collapsed legacy window, got %+v", windows[0])
	}
	if windows[1].Complete {
		t.Fatalf("expected missing result timing to be unavailable, got %+v", windows[1])
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

func responseLogAt(t *testing.T, body map[string]interface{}, responseTime int64, completedAt string) *model.ResponseLog {
	t.Helper()
	response := responseLog(t, body, responseTime)
	response.CompletedAt = completedAt
	return response
}
