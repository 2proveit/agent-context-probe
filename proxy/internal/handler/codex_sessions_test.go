package handler

import (
	"testing"

	"github.com/seifghazi/claude-code-monitor/internal/model"
)

func TestBuildSessionGroupsCodexAgentTree(t *testing.T) {
	request := func(id, thread, parent string) model.RequestLog {
		return model.RequestLog{
			RequestID: id, Timestamp: "2026-11-09T10:00:00Z",
			Headers: map[string][]string{
				"Originator": {"codex_sdk_ts"}, "Session-Id": {"root"},
				"Thread-Id": {thread}, "X-Codex-Parent-Thread-Id": {parent},
			},
		}
	}
	roots, groups := buildSessionGroups([]model.RequestLog{
		request("1", "root", ""), request("2", "child", "root"),
		request("3", "root", ""), request("4", "child", "root"),
		request("5", "grandchild", "child"), request("6", "sibling", "root"),
	})
	if len(roots) != 1 || roots[0].SessionID != "root" || len(groups) != 4 {
		t.Fatalf("expected one tree with four distinct threads, got roots=%+v groups=%+v", roots, groups)
	}
	root, child := groups["root"], groups["child"]
	if root.summary.RequestCount != 2 || child.summary.RequestCount != 2 {
		t.Fatalf("thread requests were split or merged: root=%+v child=%+v", root.summary, child.summary)
	}
	if len(root.summary.Children) != 2 || len(child.summary.Children) != 1 || child.summary.Children[0].SessionID != "grandchild" {
		t.Fatalf("expected direct parent relationships: root=%+v child=%+v", root.summary, child.summary)
	}
	if child.summary.Kind != "subagent" || child.summary.ParentSessionID != "root" {
		t.Fatalf("unexpected child metadata: %+v", child.summary)
	}
	detail := buildSessionDetail(root, groups)
	if len(detail.Requests) != 2 || len(detail.Children) != 2 {
		t.Fatalf("unexpected detail tree: %+v", detail)
	}
}

func TestBuildSessionGroupsIdentityHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string][]string
		want    string
	}{
		{"sdk session fallback", map[string][]string{"Originator": {"codex_sdk_ts"}, "Session-Id": {"session"}}, "session"},
		{"thread wins over shared session and affinity", map[string][]string{"Thread-Id": {"child"}, "Session-Id": {"root"}, "X-Session-Affinity": {"root"}}, "child"},
		{"case insensitive", map[string][]string{"thread-id": {" child "}, "session-id": {"root"}}, "child"},
		{"blank thread falls back", map[string][]string{"Thread-Id": {" "}, "Session-Id": {"session"}}, "session"},
		{"empty header values fall back", map[string][]string{"Thread-Id": {}, "Session-Id": {"", "session"}}, "session"},
		{"legacy affinity", map[string][]string{"X-Session-Affinity": {"legacy"}, "Session-Id": {"session"}}, "legacy"},
		{"opencode", map[string][]string{"X-OpenCode-Session": {"legacy"}}, "legacy"},
		{"claude", map[string][]string{"X-Claude-Code-Session-Id": {"legacy"}}, "legacy"},
		{"originator alone is not identity", map[string][]string{"Originator": {"codex_sdk_ts"}}, "request:req"},
		{"no identity", nil, "request:req"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roots, _ := buildSessionGroups([]model.RequestLog{{RequestID: "req", Headers: tt.headers}})
			if len(roots) != 1 || roots[0].SessionID != tt.want {
				t.Fatalf("expected identity %q, got %+v", tt.want, roots)
			}
		})
	}
}

func TestBuildSessionGroupsCodexParentArrivesLater(t *testing.T) {
	roots, groups := buildSessionGroups([]model.RequestLog{
		{RequestID: "1", Headers: map[string][]string{"Thread-Id": {"root"}}},
		{RequestID: "2", Headers: map[string][]string{"Thread-Id": {"child"}, "Session-Id": {"root"}}},
		{RequestID: "3", Headers: map[string][]string{"Thread-Id": {"child"}, "X-Codex-Parent-Thread-Id": {"root"}}},
	})
	if len(roots) != 1 || len(roots[0].Children) != 1 || groups["child"].summary.Kind != "subagent" {
		t.Fatalf("expected later parent header to link child, got %+v", roots)
	}
}

func TestBuildSessionGroupsDoesNotInferCodexParentFromSharedSession(t *testing.T) {
	roots, _ := buildSessionGroups([]model.RequestLog{
		{RequestID: "1", Headers: map[string][]string{"Thread-Id": {"root"}, "Session-Id": {"root"}}},
		{RequestID: "2", Headers: map[string][]string{"Thread-Id": {"child"}, "Session-Id": {"root"}}},
	})
	if len(roots) != 2 {
		t.Fatalf("shared session does not establish a direct parent: got %d roots", len(roots))
	}
}
