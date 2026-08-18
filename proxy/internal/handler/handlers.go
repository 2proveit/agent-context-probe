package handler

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/seifghazi/claude-code-monitor/internal/model"
	"github.com/seifghazi/claude-code-monitor/internal/service"
)

type openAIForwarder interface {
	ForwardChatCompletions(context.Context, *http.Request) (*http.Response, error)
	ForwardResponses(context.Context, *http.Request) (*http.Response, error)
}

type Options struct {
	MaxCaptureBytes            int64
	ShowRawStreamEvents        bool
	RawRequestMaxDisplayChars  int
	RawResponseMaxDisplayChars int
}

type Handler struct {
	anthropicService service.AnthropicService
	storageService   service.StorageService
	modelRouter      *service.ModelRouter
	openAIProvider   openAIForwarder
	logger           *log.Logger
	options          Options
}

func New(
	anthropicService service.AnthropicService,
	storageService service.StorageService,
	logger *log.Logger,
	modelRouter *service.ModelRouter,
	openAIProvider openAIForwarder,
	options Options,
) *Handler {
	return &Handler{
		anthropicService: anthropicService,
		storageService:   storageService,
		modelRouter:      modelRouter,
		openAIProvider:   openAIProvider,
		logger:           logger,
		options:          options,
	}
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	h.handleOpenAIRequest(w, r, openAIProtocolChat, h.openAIProvider.ForwardChatCompletions)
}

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	h.handleOpenAIRequest(w, r, openAIProtocolResponses, h.openAIProvider.ForwardResponses)
}

func (h *Handler) handleOpenAIRequest(
	w http.ResponseWriter,
	r *http.Request,
	protocol string,
	forward func(context.Context, *http.Request) (*http.Response, error),
) {
	bodyBytes := getBodyBytes(r)
	if bodyBytes == nil {
		writeOpenAIError(w, "Error reading request body", "invalid_request_error", "request_body_unavailable", http.StatusBadRequest)
		return
	}

	var requestBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &requestBody); err != nil {
		writeOpenAIError(w, "Invalid JSON", "invalid_request_error", "invalid_json", http.StatusBadRequest)
		return
	}

	modelName, _ := requestBody["model"].(string)
	isStreaming, _ := requestBody["stream"].(bool)
	storedBody := interface{}(requestBody)
	if h.options.MaxCaptureBytes > 0 && int64(len(bodyBytes)) > h.options.MaxCaptureBytes {
		storedBody = map[string]interface{}{
			"model":  modelName,
			"stream": isStreaming,
			"_capture": map[string]interface{}{
				"truncated":     true,
				"capturedBytes": h.options.MaxCaptureBytes,
				"requestBytes":  len(bodyBytes),
				"preview":       string(bodyBytes[:h.options.MaxCaptureBytes]),
			},
		}
	}
	requestLog := &model.RequestLog{
		RequestID:     generateRequestID(),
		Timestamp:     time.Now().Format(time.RFC3339Nano),
		Method:        r.Method,
		Endpoint:      r.URL.Path,
		Protocol:      protocol,
		Headers:       CaptureHeaders(r.Header),
		Body:          storedBody,
		Model:         modelName,
		OriginalModel: modelName,
		RoutedModel:   modelName,
		UserAgent:     r.Header.Get("User-Agent"),
		ContentType:   r.Header.Get("Content-Type"),
	}
	if _, err := h.storageService.SaveRequest(requestLog); err != nil {
		h.logger.Printf("❌ Error saving OpenAI request: %v", err)
	}

	startTime := time.Now()
	resp, err := forward(r.Context(), r)
	if err != nil {
		h.logger.Printf("❌ Error forwarding %s request: %v", protocol, err)
		errorBody, _ := json.Marshal(openAIErrorPayload(
			"Failed to connect to upstream",
			"proxy_error",
			"upstream_connection_failed",
		))
		requestLog.Response = &model.ResponseLog{
			StatusCode:   http.StatusBadGateway,
			Body:         errorBody,
			ResponseTime: time.Since(startTime).Milliseconds(),
			IsStreaming:  isStreaming,
			CompletedAt:  time.Now().Format(time.RFC3339Nano),
		}
		if updateErr := h.storageService.UpdateRequestWithResponse(requestLog); updateErr != nil {
			h.logger.Printf("❌ Error updating failed OpenAI request: %v", updateErr)
		}
		writeOpenAIError(w, "Failed to connect to upstream", "proxy_error", "upstream_connection_failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	h.copyOpenAIResponse(w, resp, requestLog, startTime, isStreaming, protocol)
}

func (h *Handler) copyOpenAIResponse(
	w http.ResponseWriter,
	resp *http.Response,
	requestLog *model.RequestLog,
	startTime time.Time,
	isStreaming bool,
	protocol string,
) {
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	var captured bytes.Buffer
	var responseBytes int64
	var streamErr string
	buffer := make([]byte, 32*1024)
	for {
		count, readErr := resp.Body.Read(buffer)
		if count > 0 {
			chunk := buffer[:count]
			responseBytes += int64(count)
			if h.options.MaxCaptureBytes == 0 {
				_, _ = captured.Write(chunk)
			} else if remaining := h.options.MaxCaptureBytes - int64(captured.Len()); remaining > 0 {
				captureCount := int64(count)
				if captureCount > remaining {
					captureCount = remaining
				}
				_, _ = captured.Write(chunk[:captureCount])
			}
			if _, writeErr := w.Write(chunk); writeErr != nil {
				streamErr = "client connection closed while streaming the response"
				break
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			h.logger.Printf("❌ Error reading OpenAI upstream response: %v", readErr)
			streamErr = "upstream response stream was interrupted"
			break
		}
	}

	truncated := responseBytes > int64(captured.Len())
	responseLog := &model.ResponseLog{
		StatusCode:    resp.StatusCode,
		Headers:       CaptureHeaders(resp.Header),
		ResponseTime:  time.Since(startTime).Milliseconds(),
		IsStreaming:   isStreaming,
		CompletedAt:   time.Now().Format(time.RFC3339Nano),
		Truncated:     truncated,
		CapturedBytes: int64(captured.Len()),
		ResponseBytes: responseBytes,
		StreamError:   streamErr,
	}

	capturedBytes := captured.Bytes()
	isEventStream := isStreaming && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
	switch {
	case isEventStream:
		responseLog.BodyText = captured.String()
		if truncated {
			responseLog.StreamError = "response capture was truncated before it could be parsed"
		} else if responseLog.StreamError == "" {
			body, aggregateErr := aggregateOpenAIStream(protocol, capturedBytes)
			if body != nil {
				responseLog.Body = body
			}
			if aggregateErr != nil {
				responseLog.StreamError = aggregateErr.Error()
			}
		}
	case json.Valid(capturedBytes):
		responseLog.Body = append(json.RawMessage(nil), capturedBytes...)
	default:
		responseLog.BodyText = captured.String()
	}

	requestLog.Response = responseLog
	if err := h.storageService.UpdateRequestWithResponse(requestLog); err != nil {
		h.logger.Printf("❌ Error updating OpenAI request response: %v", err)
	}
}

func (h *Handler) UIConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSONResponse(w, map[string]interface{}{
		"showRawStreamEvents":        h.options.ShowRawStreamEvents,
		"rawRequestMaxDisplayChars":  h.options.RawRequestMaxDisplayChars,
		"rawResponseMaxDisplayChars": h.options.RawResponseMaxDisplayChars,
	})
}

func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	// Get body bytes from context (set by middleware)
	bodyBytes := getBodyBytes(r)
	if bodyBytes == nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	// Parse the request
	var req model.AnthropicRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		log.Printf("❌ Error parsing JSON: %v", err)
		writeErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	requestID := generateRequestID()
	startTime := time.Now()

	// Use model router to determine provider and route the request
	decision, err := h.modelRouter.DetermineRoute(&req)
	if err != nil {
		log.Printf("❌ Error routing request: %v", err)
		writeErrorResponse(w, "Failed to route request", http.StatusInternalServerError)
		return
	}

	// Create request log with routing information
	requestLog := &model.RequestLog{
		RequestID:     requestID,
		Timestamp:     time.Now().Format(time.RFC3339Nano),
		Method:        r.Method,
		Endpoint:      r.URL.Path,
		Headers:       CaptureHeaders(r.Header),
		Body:          req,
		Model:         decision.OriginalModel,
		OriginalModel: decision.OriginalModel,
		RoutedModel:   decision.TargetModel,
		UserAgent:     r.Header.Get("User-Agent"),
		ContentType:   r.Header.Get("Content-Type"),
	}

	if _, err := h.storageService.SaveRequest(requestLog); err != nil {
		log.Printf("❌ Error saving request: %v", err)
	}

	// If the model was changed by routing, update the request body
	if decision.TargetModel != decision.OriginalModel {
		req.Model = decision.TargetModel

		// Re-marshal the request with the updated model
		updatedBodyBytes, err := json.Marshal(req)
		if err != nil {
			log.Printf("❌ Error marshaling updated request: %v", err)
			writeErrorResponse(w, "Failed to process request", http.StatusInternalServerError)
			return
		}

		// Update the request body
		r.Body = io.NopCloser(bytes.NewReader(updatedBodyBytes))
		r.ContentLength = int64(len(updatedBodyBytes))
		r.Header.Set("Content-Length", fmt.Sprintf("%d", len(updatedBodyBytes)))
	}

	// Forward the request to the selected provider
	resp, err := decision.Provider.ForwardRequest(r.Context(), r)
	if err != nil {
		log.Printf("❌ Error forwarding to %s API: %v", decision.Provider.Name(), err)
		writeErrorResponse(w, "Failed to forward request", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if req.Stream {
		h.handleStreamingResponse(w, resp, requestLog, startTime)
		return
	}

	h.handleNonStreamingResponse(w, resp, requestLog, startTime)
}

func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	// This proxy uses pattern-based routing and supports any model dynamically.
	// Returning an empty list since the actual supported models depend on the
	// upstream providers (Anthropic, OpenAI) and their current offerings.
	response := &model.ModelsResponse{
		Object: "list",
		Data:   []model.ModelInfo{},
	}

	writeJSONResponse(w, response)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	response := &model.HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
	}

	writeJSONResponse(w, response)
}

func (h *Handler) UI(w http.ResponseWriter, r *http.Request) {
	htmlContent, err := os.ReadFile("index.html")
	if err != nil {
		// Error reading index.html
		http.Error(w, "UI not available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write(htmlContent)
}

func (h *Handler) GetRequests(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10 // Default limit
	}

	// Get model filter from query parameters
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

	// Apply filters at storage level before pagination.
	allRequests, err := h.storageService.GetAllRequests(modelFilter, headerFilter, sinceFilter)
	if err != nil {
		log.Printf("Error getting requests: %v", err)
		http.Error(w, "Failed to get requests", http.StatusInternalServerError)
		return
	}

	// Convert pointers to values for consistency
	requests := make([]model.RequestLog, len(allRequests))
	for i, req := range allRequests {
		if req != nil {
			requests[i] = *req
			enrichOpenAIRequestForDisplay(&requests[i])
		}
	}

	// Calculate total before pagination
	total := len(requests)

	// Apply pagination
	start := (page - 1) * limit
	end := start + limit
	if start >= len(requests) {
		requests = []model.RequestLog{}
	} else {
		if end > len(requests) {
			end = len(requests)
		}
		requests = requests[start:end]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Requests []model.RequestLog `json:"requests"`
		Total    int                `json:"total"`
	}{
		Requests: requests,
		Total:    total,
	})
}

func enrichOpenAIRequestForDisplay(request *model.RequestLog) {
	switch {
	case strings.Contains(request.Endpoint, "/chat/completions"):
		request.Protocol = openAIProtocolChat
	case strings.Contains(request.Endpoint, "/responses"):
		request.Protocol = openAIProtocolResponses
	default:
		return
	}

	response := request.Response
	if response == nil || len(response.Body) > 0 || response.BodyText == "" || !response.IsStreaming {
		return
	}
	body, err := aggregateOpenAIStream(request.Protocol, []byte(response.BodyText))
	if err != nil {
		if response.StreamError == "" {
			response.StreamError = err.Error()
		}
		return
	}
	response.Body = body
}

func (h *Handler) DeleteRequests(w http.ResponseWriter, r *http.Request) {

	clearedCount, err := h.storageService.ClearRequests()
	if err != nil {
		log.Printf("Error clearing requests: %v", err)
		writeErrorResponse(w, "Error clearing request history", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"message": "Request history cleared",
		"deleted": clearedCount,
	}

	writeJSONResponse(w, response)
}

func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	writeErrorResponse(w, "Not found", http.StatusNotFound)
}

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, resp *http.Response, requestLog *model.RequestLog, startTime time.Time) {

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	if resp.StatusCode != http.StatusOK {
		log.Printf("❌ Anthropic API error: %d", resp.StatusCode)
		errorBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Error details: %s", string(errorBytes))

		responseLog := &model.ResponseLog{
			StatusCode:   resp.StatusCode,
			Headers:      CaptureHeaders(resp.Header),
			BodyText:     string(errorBytes),
			ResponseTime: time.Since(startTime).Milliseconds(),
			IsStreaming:  true,
			CompletedAt:  time.Now().Format(time.RFC3339Nano),
		}

		requestLog.Response = responseLog
		if err := h.storageService.UpdateRequestWithResponse(requestLog); err != nil {
			log.Printf("❌ Error updating request with error response: %v", err)
		}

		w.WriteHeader(resp.StatusCode)
		w.Write(errorBytes)
		return
	}

	textBlocks := make(map[int]*strings.Builder)
	thinkingBlocks := make(map[int]*strings.Builder)
	thinkingSignatures := make(map[int]*strings.Builder)
	redactedThinkingBlocks := make(map[int]*model.ContentBlock)
	toolCalls := make(map[int]*model.ContentBlock)
	var streamingChunks []string
	var finalUsage *model.AnthropicUsage
	var messageID string
	var modelName string
	var stopReason string
	var messageStopped bool

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		streamingChunks = append(streamingChunks, line)
		fmt.Fprintf(w, "%s\n\n", line)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		jsonData := strings.TrimPrefix(line, "data: ")

		// Parse as generic JSON first to capture usage data
		var genericEvent map[string]interface{}
		if err := json.Unmarshal([]byte(jsonData), &genericEvent); err != nil {
			log.Printf("⚠️ Error unmarshalling streaming event: %v", err)
			continue
		}

		// Capture metadata from message_start event
		if eventType, ok := genericEvent["type"].(string); ok && eventType == "message_start" {
			if message, ok := genericEvent["message"].(map[string]interface{}); ok {
				// Capture message metadata
				if id, ok := message["id"].(string); ok {
					messageID = id
				}
				if model, ok := message["model"].(string); ok {
					modelName = model
				}
				if reason, ok := message["stop_reason"].(string); ok {
					stopReason = reason
				}
				if usage, ok := message["usage"].(map[string]interface{}); ok {
					mergeAnthropicUsage(&finalUsage, usage)
				}
			}
		}

		// Capture usage data from message_delta event
		if eventType, ok := genericEvent["type"].(string); ok && eventType == "message_delta" {
			if delta, ok := genericEvent["delta"].(map[string]interface{}); ok {
				if reason, ok := delta["stop_reason"].(string); ok {
					stopReason = reason
				}
			}
			// Usage is at top level for message_delta events
			if usage, ok := genericEvent["usage"].(map[string]interface{}); ok {
				mergeAnthropicUsage(&finalUsage, usage)
			}
		}

		// Parse as structured event for content processing
		var event model.StreamingEvent
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			// Skip if structured parsing fails, but we already got the usage data above
			continue
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta != nil {
				index := 0
				if event.Index != nil {
					index = *event.Index
				}
				switch event.Delta.Type {
				case "text_delta":
					if textBlocks[index] == nil {
						textBlocks[index] = &strings.Builder{}
					}
					textBlocks[index].WriteString(event.Delta.Text)
				case "thinking_delta":
					if thinkingBlocks[index] == nil {
						thinkingBlocks[index] = &strings.Builder{}
					}
					thinkingBlocks[index].WriteString(event.Delta.Thinking)
				case "signature_delta":
					if thinkingSignatures[index] == nil {
						thinkingSignatures[index] = &strings.Builder{}
					}
					thinkingSignatures[index].WriteString(event.Delta.Signature)
				case "input_json_delta":
					if toolCalls[index] != nil {
						toolCalls[index].Input = append(
							toolCalls[index].Input,
							[]byte(event.Delta.PartialJSON)...,
						)
					}
				}
			}
		case "content_block_start":
			if event.ContentBlock != nil {
				index := 0
				if event.Index != nil {
					index = *event.Index
				}
				switch event.ContentBlock.Type {
				case "text":
					if textBlocks[index] == nil {
						textBlocks[index] = &strings.Builder{}
					}
					textBlocks[index].WriteString(event.ContentBlock.Text)
				case "thinking":
					if thinkingBlocks[index] == nil {
						thinkingBlocks[index] = &strings.Builder{}
					}
					thinkingBlocks[index].WriteString(event.ContentBlock.Thinking)
					if event.ContentBlock.Signature != "" {
						thinkingSignatures[index] = &strings.Builder{}
						thinkingSignatures[index].WriteString(event.ContentBlock.Signature)
					}
				case "redacted_thinking":
					block := *event.ContentBlock
					redactedThinkingBlocks[index] = &block
				case "tool_use":
					block := *event.ContentBlock
					block.Input = nil
					toolCalls[index] = &block
				}
			}
		case "message_stop":
			messageStopped = true
		}
	}

	scanErr := scanner.Err()
	responseLog := &model.ResponseLog{
		StatusCode:      resp.StatusCode,
		Headers:         CaptureHeaders(resp.Header),
		StreamingChunks: streamingChunks,
		ResponseTime:    time.Since(startTime).Milliseconds(),
		IsStreaming:     true,
		CompletedAt:     time.Now().Format(time.RFC3339Nano),
	}

	if scanErr != nil {
		responseLog.StreamError = "upstream response stream was interrupted"
	} else if !messageStopped {
		responseLog.StreamError = "upstream response stream ended before message_stop"
	} else {
		blockIndexSet := make(map[int]struct{}, len(textBlocks)+len(thinkingBlocks)+len(redactedThinkingBlocks)+len(toolCalls))
		for index := range textBlocks {
			blockIndexSet[index] = struct{}{}
		}
		for index := range thinkingBlocks {
			blockIndexSet[index] = struct{}{}
		}
		for index := range redactedThinkingBlocks {
			blockIndexSet[index] = struct{}{}
		}
		for index := range toolCalls {
			blockIndexSet[index] = struct{}{}
		}
		blockIndexes := make([]int, 0, len(blockIndexSet))
		for index := range blockIndexSet {
			blockIndexes = append(blockIndexes, index)
		}
		sort.Ints(blockIndexes)

		contentBlocks := make([]interface{}, 0, len(blockIndexes))
		for _, index := range blockIndexes {
			if textBlock := textBlocks[index]; textBlock != nil && textBlock.Len() > 0 {
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "text",
					"text": textBlock.String(),
				})
			}
			if thinkingBlock := thinkingBlocks[index]; thinkingBlock != nil && thinkingBlock.Len() > 0 {
				thinkingContent := map[string]interface{}{
					"type":     "thinking",
					"thinking": thinkingBlock.String(),
				}
				if signature := thinkingSignatures[index]; signature != nil && signature.Len() > 0 {
					thinkingContent["signature"] = signature.String()
				}
				contentBlocks = append(contentBlocks, thinkingContent)
			}
			if redactedThinking := redactedThinkingBlocks[index]; redactedThinking != nil {
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "redacted_thinking",
					"data": redactedThinking.Data,
				})
			}
			if toolCall := toolCalls[index]; toolCall != nil {
				input := interface{}(map[string]interface{}{})
				if len(toolCall.Input) > 0 {
					if err := json.Unmarshal(toolCall.Input, &input); err != nil {
						input = map[string]interface{}{"raw": string(toolCall.Input)}
					}
				}
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type":  "tool_use",
					"id":    toolCall.ID,
					"name":  toolCall.Name,
					"input": input,
				})
			}
		}

		responseBody := map[string]interface{}{
			"content":     contentBlocks,
			"id":          messageID,
			"model":       modelName,
			"role":        "assistant",
			"stop_reason": stopReason,
			"type":        "message",
		}
		if finalUsage != nil {
			responseBody["usage"] = finalUsage
		}
		responseBodyBytes, err := json.Marshal(responseBody)
		if err != nil {
			log.Printf("❌ Error marshaling streaming response body: %v", err)
			responseBodyBytes = []byte("{}")
		}
		responseLog.Body = json.RawMessage(responseBodyBytes)
	}

	requestLog.Response = responseLog
	if err := h.storageService.UpdateRequestWithResponse(requestLog); err != nil {
		log.Printf("❌ Error updating request with streaming response: %v", err)
	}

	if scanErr != nil {
		log.Printf("❌ Streaming error: %v", scanErr)
	} else if responseLog.StreamError != "" {
		log.Printf("❌ Streaming error: %s", responseLog.StreamError)
	} else {
		log.Println("✅ Streaming response completed")
	}
}

func mergeAnthropicUsage(target **model.AnthropicUsage, usage map[string]interface{}) {
	if *target == nil {
		*target = &model.AnthropicUsage{}
	}
	if inputTokens, ok := usage["input_tokens"].(float64); ok {
		(*target).InputTokens = int(inputTokens)
	}
	if outputTokens, ok := usage["output_tokens"].(float64); ok {
		(*target).OutputTokens = int(outputTokens)
	}
	if cacheCreation, ok := usage["cache_creation_input_tokens"].(float64); ok {
		(*target).CacheCreationInputTokens = int(cacheCreation)
	}
	if cacheRead, ok := usage["cache_read_input_tokens"].(float64); ok {
		(*target).CacheReadInputTokens = int(cacheRead)
	}
}

func (h *Handler) handleNonStreamingResponse(w http.ResponseWriter, resp *http.Response, requestLog *model.RequestLog, startTime time.Time) {
	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ Error reading Anthropic response: %v", err)
		writeErrorResponse(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	responseLog := &model.ResponseLog{
		StatusCode:   resp.StatusCode,
		Headers:      CaptureHeaders(resp.Header),
		ResponseTime: time.Since(startTime).Milliseconds(),
		IsStreaming:  false,
		CompletedAt:  time.Now().Format(time.RFC3339Nano),
	}

	// Parse the response as AnthropicResponse for consistent structure
	if resp.StatusCode == http.StatusOK {
		var anthropicResp model.AnthropicResponse
		if err := json.Unmarshal(responseBytes, &anthropicResp); err == nil {
			// Successfully parsed - store the structured response
			responseLog.Body = json.RawMessage(responseBytes)
		} else {
			// If parsing fails, store as text but log the error
			log.Printf("⚠️ Failed to parse Anthropic response: %v", err)
			log.Printf("📄 Response body (first 500 chars): %s", string(responseBytes[:min(500, len(responseBytes))]))
			responseLog.BodyText = string(responseBytes)
		}
	} else {
		// For error responses, store as text
		responseLog.BodyText = string(responseBytes)
	}

	requestLog.Response = responseLog
	if err := h.storageService.UpdateRequestWithResponse(requestLog); err != nil {
		log.Printf("❌ Error updating request with response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("❌ Anthropic API error: %d %s", resp.StatusCode, string(responseBytes))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(responseBytes)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseBytes)
}

// Helper function to get minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func generateRequestID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func getBodyBytes(r *http.Request) []byte {
	if bodyBytes, ok := r.Context().Value(model.BodyBytesKey).([]byte); ok {
		return bodyBytes
	}
	return nil
}

func writeJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("❌ Error encoding JSON response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(&model.ErrorResponse{Error: message})
}

func openAIErrorPayload(message, errorType, code string) map[string]interface{} {
	return map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errorType,
			"param":   nil,
			"code":    code,
		},
	}
}

func writeOpenAIError(w http.ResponseWriter, message, errorType, code string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(openAIErrorPayload(message, errorType, code))
}
