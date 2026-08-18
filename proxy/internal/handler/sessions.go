package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/seifghazi/claude-code-monitor/internal/model"
)

type sessionSummary struct {
	SessionID       string            `json:"sessionId"`
	ParentSessionID string            `json:"parentSessionId,omitempty"`
	Purpose         string            `json:"purpose,omitempty"`
	Kind            string            `json:"kind"`
	Title           string            `json:"title"`
	Model           string            `json:"model,omitempty"`
	AgentName       string            `json:"agentName,omitempty"`
	TaskCallID      string            `json:"taskCallId,omitempty"`
	TaskDescription string            `json:"taskDescription,omitempty"`
	Status          string            `json:"status"`
	ResultMessage   string            `json:"resultMessage,omitempty"`
	RequestCount    int               `json:"requestCount"`
	ToolCallCount   int               `json:"toolCallCount"`
	InputTokens     int64             `json:"inputTokens"`
	OutputTokens    int64             `json:"outputTokens"`
	ResponseTimeMs  int64             `json:"responseTimeMs"`
	ElapsedTimeMs   int64             `json:"elapsedTimeMs"`
	FirstTimestamp  string            `json:"firstTimestamp"`
	LastTimestamp   string            `json:"lastTimestamp"`
	Children        []*sessionSummary `json:"children,omitempty"`
}

type sessionDetail struct {
	Summary  sessionSummary     `json:"summary"`
	Requests []model.RequestLog `json:"requests"`
	Children []*sessionDetail   `json:"children,omitempty"`
}

type sessionGroup struct {
	summary         *sessionSummary
	requests        []model.RequestLog
	firstUserPrompt string
	firstObservedAt time.Time
	lastObservedAt  time.Time
	treeObservedAt  time.Time
}

type taskInvocation struct {
	parentSessionID string
	callID          string
	timestamp       string
	description     string
	agentName       string
	prompt          string
	taskID          string
}

// GetSessions returns session summaries, or one full session tree when the
// session query parameter is present. Grouping is performed before pagination
// so a long session is never split across request pages.
func (h *Handler) GetSessions(w http.ResponseWriter, r *http.Request) {
	modelFilter := strings.TrimSpace(r.URL.Query().Get("model"))
	if modelFilter == "" {
		modelFilter = "all"
	}
	headerFilter := strings.TrimSpace(r.URL.Query().Get("header"))
	sinceFilter := strings.TrimSpace(r.URL.Query().Get("since"))
	if sinceFilter != "" {
		if _, err := time.Parse(time.RFC3339, sinceFilter); err != nil {
			http.Error(w, "Invalid since filter", http.StatusBadRequest)
			return
		}
	}

	allRequests, err := h.storageService.GetAllRequests(modelFilter, headerFilter, sinceFilter)
	if err != nil {
		http.Error(w, "Failed to get sessions", http.StatusInternalServerError)
		return
	}

	requests := make([]model.RequestLog, 0, len(allRequests))
	for _, request := range allRequests {
		if request == nil {
			continue
		}
		copy := *request
		enrichOpenAIRequestForDisplay(&copy)
		requests = append(requests, copy)
	}

	roots, groups := buildSessionGroups(requests)
	if requestedID := strings.TrimSpace(r.URL.Query().Get("session")); requestedID != "" {
		group := groups[requestedID]
		if group == nil {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		detail := buildSessionDetail(group, groups)
		writeJSONResponse(w, map[string]interface{}{"session": detail})
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 25
	}
	total := len(roots)
	start := (page - 1) * limit
	end := start + limit
	if start >= total {
		roots = []*sessionSummary{}
	} else {
		if end > total {
			end = total
		}
		roots = roots[start:end]
	}

	writeJSONResponse(w, map[string]interface{}{
		"sessions": roots,
		"total":    total,
	})
}

func buildSessionGroups(requests []model.RequestLog) ([]*sessionSummary, map[string]*sessionGroup) {
	sort.SliceStable(requests, func(i, j int) bool {
		if requests[i].Timestamp == requests[j].Timestamp {
			return requests[i].RequestID < requests[j].RequestID
		}
		return requests[i].Timestamp < requests[j].Timestamp
	})

	groups := make(map[string]*sessionGroup)
	for _, request := range requests {
		sessionID := firstHeader(request.Headers, "X-Session-Affinity", "X-OpenCode-Session", "X-Claude-Code-Session-Id")
		if sessionID == "" {
			sessionID = "request:" + request.RequestID
		}
		group := groups[sessionID]
		if group == nil {
			parentID := firstHeader(request.Headers, "X-Parent-Session-Id")
			purpose := firstHeader(request.Headers, "X-Lumi-Session-Purpose")
			group = &sessionGroup{summary: &sessionSummary{
				SessionID:       sessionID,
				ParentSessionID: parentID,
				Purpose:         purpose,
				Kind:            sessionKind(parentID),
				Status:          "captured",
				FirstTimestamp:  request.Timestamp,
				LastTimestamp:   request.Timestamp,
			}}
			groups[sessionID] = group
		}

		group.requests = append(group.requests, request)
		observeRequestWindow(group, request)
		group.summary.RequestCount++
		group.summary.LastTimestamp = request.Timestamp
		if group.summary.Model == "" {
			group.summary.Model = requestModel(request)
		}
		if group.firstUserPrompt == "" {
			group.firstUserPrompt = firstUserPrompt(request.Body)
		}
		toolCalls, inputTokens, outputTokens, responseTime, status := requestMetrics(request)
		group.summary.ToolCallCount += toolCalls
		group.summary.InputTokens += inputTokens
		group.summary.OutputTokens += outputTokens
		group.summary.ResponseTimeMs += responseTime
		if status != "" {
			group.summary.Status = status
		}
	}

	invocations := collectTaskInvocations(groups)
	linkTaskInvocations(groups, invocations)
	for _, group := range groups {
		if group.summary.Title == "" {
			group.summary.Title = sessionTitle(group)
		}
	}

	var roots []*sessionSummary
	for _, group := range groups {
		parent := groups[group.summary.ParentSessionID]
		if parent == nil || group.summary.ParentSessionID == group.summary.SessionID {
			roots = append(roots, group.summary)
			continue
		}
		parent.summary.Children = append(parent.summary.Children, group.summary)
	}
	updateSessionElapsedTimes(groups)
	for _, group := range groups {
		sort.SliceStable(group.summary.Children, func(i, j int) bool {
			return group.summary.Children[i].FirstTimestamp < group.summary.Children[j].FirstTimestamp
		})
	}
	sort.SliceStable(roots, func(i, j int) bool {
		return roots[i].LastTimestamp > roots[j].LastTimestamp
	})
	return roots, groups
}

func observeRequestWindow(group *sessionGroup, request model.RequestLog) {
	startedAt, err := time.Parse(time.RFC3339Nano, request.Timestamp)
	if err != nil {
		return
	}
	if group.firstObservedAt.IsZero() || startedAt.Before(group.firstObservedAt) {
		group.firstObservedAt = startedAt
	}

	completedAt := startedAt
	if request.Response != nil {
		if parsed, parseErr := time.Parse(time.RFC3339Nano, request.Response.CompletedAt); parseErr == nil && parsed.After(completedAt) {
			completedAt = parsed
		}
		derivedCompletion := startedAt.Add(time.Duration(request.Response.ResponseTime) * time.Millisecond)
		if derivedCompletion.After(completedAt) {
			completedAt = derivedCompletion
		}
	}
	if group.lastObservedAt.IsZero() || completedAt.After(group.lastObservedAt) {
		group.lastObservedAt = completedAt
	}
}

func updateSessionElapsedTimes(groups map[string]*sessionGroup) {
	calculated := make(map[string]bool)
	visiting := make(map[string]bool)
	var update func(*sessionGroup) time.Time
	update = func(group *sessionGroup) time.Time {
		sessionID := group.summary.SessionID
		if calculated[sessionID] {
			return group.treeObservedAt
		}
		if visiting[sessionID] {
			return group.lastObservedAt
		}

		visiting[sessionID] = true
		latest := group.lastObservedAt
		for _, childSummary := range group.summary.Children {
			child := groups[childSummary.SessionID]
			if child == nil {
				continue
			}
			childLatest := update(child)
			if childLatest.After(latest) {
				latest = childLatest
			}
		}
		delete(visiting, sessionID)

		group.treeObservedAt = latest
		if !group.firstObservedAt.IsZero() && !latest.Before(group.firstObservedAt) {
			group.summary.ElapsedTimeMs = latest.Sub(group.firstObservedAt).Milliseconds()
		}
		calculated[sessionID] = true
		return latest
	}

	for _, group := range groups {
		update(group)
	}
}

func buildSessionDetail(group *sessionGroup, groups map[string]*sessionGroup) *sessionDetail {
	summary := *group.summary
	children := summary.Children
	summary.Children = nil
	detail := &sessionDetail{Summary: summary, Requests: group.requests}
	for _, child := range children {
		if childGroup := groups[child.SessionID]; childGroup != nil {
			detail.Children = append(detail.Children, buildSessionDetail(childGroup, groups))
		}
	}
	return detail
}

func collectTaskInvocations(groups map[string]*sessionGroup) []taskInvocation {
	seen := make(map[string]bool)
	var result []taskInvocation
	for sessionID, group := range groups {
		for _, request := range group.requests {
			for _, call := range responseToolCalls(request) {
				if !strings.EqualFold(call.name, "task") || call.id == "" || seen[sessionID+"\x00"+call.id] {
					continue
				}
				seen[sessionID+"\x00"+call.id] = true
				result = append(result, taskInvocation{
					parentSessionID: sessionID,
					callID:          call.id,
					timestamp:       request.Timestamp,
					description:     stringValue(call.arguments["description"]),
					agentName:       stringValue(call.arguments["subagent_type"]),
					prompt:          stringValue(call.arguments["prompt"]),
					taskID:          stringValue(call.arguments["task_id"]),
				})
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].timestamp < result[j].timestamp })
	return result
}

func linkTaskInvocations(groups map[string]*sessionGroup, invocations []taskInvocation) {
	assigned := make(map[string]bool)
	for _, invocation := range invocations {
		var child *sessionGroup
		if invocation.taskID != "" {
			candidate := groups[invocation.taskID]
			if candidate != nil && candidate.summary.ParentSessionID == invocation.parentSessionID {
				child = candidate
			}
		}
		if child == nil {
			var candidates []*sessionGroup
			for sessionID, candidate := range groups {
				if assigned[sessionID] || candidate.summary.ParentSessionID != invocation.parentSessionID {
					continue
				}
				if invocation.prompt != "" && candidate.firstUserPrompt == invocation.prompt && candidate.summary.FirstTimestamp >= invocation.timestamp {
					candidates = append(candidates, candidate)
				}
			}
			sort.SliceStable(candidates, func(i, j int) bool {
				return candidates[i].summary.FirstTimestamp < candidates[j].summary.FirstTimestamp
			})
			if len(candidates) == 1 {
				child = candidates[0]
			}
		}
		if child == nil {
			continue
		}
		assigned[child.summary.SessionID] = true
		child.summary.TaskCallID = invocation.callID
		child.summary.TaskDescription = invocation.description
		child.summary.AgentName = invocation.agentName
		if invocation.description != "" {
			child.summary.Title = invocation.description
		}
		if parent := groups[invocation.parentSessionID]; parent != nil {
			if taskResult := findToolResult(parent.requests, invocation.callID); taskResult != "" {
				child.summary.ResultMessage = compactText(taskResult, 240)
				if strings.Contains(taskResult, "<task_result>") || strings.Contains(taskResult, "task_id:") {
					child.summary.Status = "completed"
				} else {
					child.summary.Status = "error"
				}
			} else if child.summary.Status != "interrupted" && child.summary.Status != "error" {
				child.summary.Status = "awaiting-result"
			}
		}
	}
}

func firstHeader(headers map[string][]string, names ...string) string {
	for _, name := range names {
		for key, values := range headers {
			if strings.EqualFold(key, name) && len(values) > 0 {
				return strings.TrimSpace(values[0])
			}
		}
	}
	return ""
}

func sessionKind(parentID string) string {
	if parentID != "" {
		return "subagent"
	}
	return "root"
}

func sessionTitle(group *sessionGroup) string {
	title := stripContextBlocks(group.firstUserPrompt)
	if title == "" {
		title = group.summary.SessionID
	}
	return compactText(title, 90)
}

func stripContextBlocks(value string) string {
	for _, tag := range []string{"user_memory_context", "memory_maintenance_job", "lumi_workspace"} {
		start := strings.Index(value, "<"+tag+">")
		endTag := "</" + tag + ">"
		end := strings.Index(value, endTag)
		if start >= 0 && end >= start {
			value = value[:start] + value[end+len(endTag):]
		}
	}
	return strings.TrimSpace(value)
}

func compactText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes]) + "…"
}

func requestModel(request model.RequestLog) string {
	if request.RoutedModel != "" {
		return request.RoutedModel
	}
	if request.Model != "" {
		return request.Model
	}
	body := objectValue(request.Body)
	return stringValue(body["model"])
}

func firstUserPrompt(body interface{}) string {
	root := objectValue(body)
	for _, item := range arrayValue(root["messages"]) {
		message := objectValue(item)
		if stringValue(message["role"]) == "user" {
			return contentText(message["content"])
		}
	}
	if input := root["input"]; input != nil {
		if text := contentText(input); text != "" {
			return text
		}
	}
	return ""
}

func requestMetrics(request model.RequestLog) (int, int64, int64, int64, string) {
	if request.Response == nil {
		return 0, 0, 0, 0, "pending"
	}
	response := objectFromRaw(request.Response.Body)
	usage := objectValue(response["usage"])
	inputTokens := intValue(firstValue(usage, "prompt_tokens", "input_tokens"))
	outputTokens := intValue(firstValue(usage, "completion_tokens", "output_tokens"))
	status := "captured"
	if request.Response.StreamError != "" {
		status = "interrupted"
	} else if request.Response.StatusCode >= 400 {
		status = "error"
	} else {
		finish := finishReason(response)
		switch finish {
		case "stop", "end_turn", "completed":
			status = "completed"
		case "tool_calls", "tool_use":
			status = "awaiting-tool"
		}
	}
	return len(responseToolCalls(request)), inputTokens, outputTokens, request.Response.ResponseTime, status
}

type normalizedToolCall struct {
	id        string
	name      string
	arguments map[string]interface{}
}

func responseToolCalls(request model.RequestLog) []normalizedToolCall {
	if request.Response == nil {
		return nil
	}
	root := objectFromRaw(request.Response.Body)
	var calls []normalizedToolCall
	choices := arrayValue(root["choices"])
	if len(choices) > 0 {
		message := objectValue(objectValue(choices[0])["message"])
		for _, item := range arrayValue(message["tool_calls"]) {
			call := objectValue(item)
			function := objectValue(call["function"])
			calls = append(calls, normalizedToolCall{
				id:        stringValue(firstValue(call, "id", "call_id")),
				name:      stringValue(function["name"]),
				arguments: parseArguments(function["arguments"]),
			})
		}
	}
	for _, item := range arrayValue(root["content"]) {
		block := objectValue(item)
		if stringValue(block["type"]) != "tool_use" {
			continue
		}
		calls = append(calls, normalizedToolCall{
			id:        stringValue(block["id"]),
			name:      stringValue(block["name"]),
			arguments: objectValue(block["input"]),
		})
	}
	for _, item := range arrayValue(root["output"]) {
		block := objectValue(item)
		if stringValue(block["type"]) != "function_call" {
			continue
		}
		calls = append(calls, normalizedToolCall{
			id:        stringValue(firstValue(block, "call_id", "id")),
			name:      stringValue(block["name"]),
			arguments: parseArguments(block["arguments"]),
		})
	}
	return calls
}

func findToolResult(requests []model.RequestLog, callID string) string {
	for _, request := range requests {
		body := objectValue(request.Body)
		for _, item := range arrayValue(body["messages"]) {
			message := objectValue(item)
			if stringValue(message["role"]) == "tool" && stringValue(message["tool_call_id"]) == callID {
				return contentText(message["content"])
			}
			for _, blockValue := range arrayValue(message["content"]) {
				block := objectValue(blockValue)
				if stringValue(block["type"]) == "tool_result" && stringValue(block["tool_use_id"]) == callID {
					return contentText(block["content"])
				}
			}
		}
	}
	return ""
}

func finishReason(root map[string]interface{}) string {
	if value := stringValue(firstValue(root, "finish_reason", "stop_reason", "status")); value != "" {
		return value
	}
	choices := arrayValue(root["choices"])
	if len(choices) > 0 {
		return stringValue(objectValue(choices[0])["finish_reason"])
	}
	return ""
}

func parseArguments(value interface{}) map[string]interface{} {
	if object, ok := value.(map[string]interface{}); ok {
		return object
	}
	text := stringValue(value)
	if text == "" {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if json.Unmarshal([]byte(text), &result) != nil {
		return map[string]interface{}{}
	}
	return result
}

func objectFromRaw(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if json.Unmarshal(raw, &result) != nil {
		return map[string]interface{}{}
	}
	return result
}

func objectValue(value interface{}) map[string]interface{} {
	if result, ok := value.(map[string]interface{}); ok {
		return result
	}
	return map[string]interface{}{}
}

func arrayValue(value interface{}) []interface{} {
	if result, ok := value.([]interface{}); ok {
		return result
	}
	return nil
}

func stringValue(value interface{}) string {
	if result, ok := value.(string); ok {
		return result
	}
	return ""
}

func contentText(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	var parts []string
	for _, item := range arrayValue(value) {
		block := objectValue(item)
		if text := stringValue(block["text"]); text != "" {
			parts = append(parts, text)
		} else if text := stringValue(block["content"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func firstValue(object map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := object[key]; ok {
			return value
		}
	}
	return nil
}

func intValue(value interface{}) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}
