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

	"github.com/gorilla/mux"

	"github.com/seifghazi/claude-code-monitor/internal/model"
	"github.com/seifghazi/claude-code-monitor/internal/service"
)

type openAIForwarder interface {
	ForwardChatCompletions(context.Context, *http.Request) (*http.Response, error)
	ForwardResponses(context.Context, *http.Request) (*http.Response, error)
}

type Options struct {
	MaxCaptureBytes     int64
	ShowRawStreamEvents bool
}

type Handler struct {
	anthropicService    service.AnthropicService
	storageService      service.StorageService
	conversationService service.ConversationService
	modelRouter         *service.ModelRouter
	openAIProvider      openAIForwarder
	logger              *log.Logger
	options             Options
}

func New(
	anthropicService service.AnthropicService,
	storageService service.StorageService,
	logger *log.Logger,
	modelRouter *service.ModelRouter,
	openAIProvider openAIForwarder,
	options Options,
) *Handler {
	conversationService := service.NewConversationService()

	return &Handler{
		anthropicService:    anthropicService,
		storageService:      storageService,
		conversationService: conversationService,
		modelRouter:         modelRouter,
		openAIProvider:      openAIProvider,
		logger:              logger,
		options:             options,
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
		Timestamp:     time.Now().Format(time.RFC3339),
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
			CompletedAt:  time.Now().Format(time.RFC3339),
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
		CompletedAt:   time.Now().Format(time.RFC3339),
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
	writeJSONResponse(w, map[string]bool{
		"showRawStreamEvents": h.options.ShowRawStreamEvents,
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
		Timestamp:     time.Now().Format(time.RFC3339),
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
	modelFilter := r.URL.Query().Get("model")
	if modelFilter == "" {
		modelFilter = "all"
	}

	// Get all requests with model filter applied at storage level
	allRequests, err := h.storageService.GetAllRequests(modelFilter)
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
			CompletedAt:  time.Now().Format(time.RFC3339),
		}

		requestLog.Response = responseLog
		if err := h.storageService.UpdateRequestWithResponse(requestLog); err != nil {
			log.Printf("❌ Error updating request with error response: %v", err)
		}

		w.WriteHeader(resp.StatusCode)
		w.Write(errorBytes)
		return
	}

	var fullResponseText strings.Builder
	var toolCalls []model.ContentBlock
	var streamingChunks []string
	var finalUsage *model.AnthropicUsage
	var messageID string
	var modelName string
	var stopReason string

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
			}
		}

		// Capture usage data from message_delta event
		if eventType, ok := genericEvent["type"].(string); ok && eventType == "message_delta" {
			// Usage is at top level for message_delta events
			if usage, ok := genericEvent["usage"].(map[string]interface{}); ok {
				// Create finalUsage if it doesn't exist yet
				if finalUsage == nil {
					finalUsage = &model.AnthropicUsage{}
				}

				// Capture all usage fields
				if inputTokens, ok := usage["input_tokens"].(float64); ok {
					finalUsage.InputTokens = int(inputTokens)
				}
				if outputTokens, ok := usage["output_tokens"].(float64); ok {
					finalUsage.OutputTokens = int(outputTokens)
				}
				if cacheCreation, ok := usage["cache_creation_input_tokens"].(float64); ok {
					finalUsage.CacheCreationInputTokens = int(cacheCreation)
				}
				if cacheRead, ok := usage["cache_read_input_tokens"].(float64); ok {
					finalUsage.CacheReadInputTokens = int(cacheRead)
				}

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
				if event.Delta.Type == "text_delta" {
					fullResponseText.WriteString(event.Delta.Text)
				} else if event.Delta.Type == "input_json_delta" {
					if event.Index != nil && *event.Index < len(toolCalls) {
						toolCalls[*event.Index].Input = append(toolCalls[*event.Index].Input, event.Delta.Input...)
					}
				}
			}
		case "content_block_start":
			if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				toolCalls = append(toolCalls, *event.ContentBlock)
			}
		case "message_stop":
			// End of stream - scanner will exit on its own
		}
	}

	responseLog := &model.ResponseLog{
		StatusCode:      resp.StatusCode,
		Headers:         CaptureHeaders(resp.Header),
		StreamingChunks: streamingChunks,
		ResponseTime:    time.Since(startTime).Milliseconds(),
		IsStreaming:     true,
		CompletedAt:     time.Now().Format(time.RFC3339),
	}

	// Create a structured response body that matches Anthropic's format
	var contentBlocks []model.AnthropicContentBlock
	if fullResponseText.Len() > 0 {
		contentBlocks = append(contentBlocks, model.AnthropicContentBlock{
			Type: "text",
			Text: fullResponseText.String(),
		})
	}

	// Create an AnthropicResponse-like structure for consistency
	responseBody := map[string]interface{}{
		"content":     contentBlocks,
		"id":          messageID,
		"model":       modelName,
		"role":        "assistant",
		"stop_reason": stopReason,
		"type":        "message",
	}

	// Add usage data if we captured it
	if finalUsage != nil {
		responseBody["usage"] = finalUsage
	}

	// Marshal to JSON for storage
	responseBodyBytes, err := json.Marshal(responseBody)
	if err != nil {
		log.Printf("❌ Error marshaling streaming response body: %v", err)
		responseBodyBytes = []byte("{}")
	}

	responseLog.Body = json.RawMessage(responseBodyBytes)

	requestLog.Response = responseLog
	if err := h.storageService.UpdateRequestWithResponse(requestLog); err != nil {
		log.Printf("❌ Error updating request with streaming response: %v", err)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("❌ Streaming error: %v", err)
	} else {
		log.Println("✅ Streaming response completed")
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
		CompletedAt:  time.Now().Format(time.RFC3339),
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

// extractTextFromMessage tries multiple strategies to extract text from a message
func extractTextFromMessage(message json.RawMessage) string {
	// Strategy 1: Direct string (simple text message)
	var directString string
	if err := json.Unmarshal(message, &directString); err == nil && directString != "" {
		return directString
	}

	// Strategy 2: Array format [{"type": "text", "text": "..."}]
	var msgArray []interface{}
	if err := json.Unmarshal(message, &msgArray); err == nil {
		for _, item := range msgArray {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if itemMap["type"] == "text" {
					if text, ok := itemMap["text"].(string); ok && text != "" {
						return text
					}
				}
			}
		}
	}

	// Strategy 3: Content object format {"content": [{"type": "text", "text": "..."}]}
	var msgContent map[string]interface{}
	if err := json.Unmarshal(message, &msgContent); err == nil {
		if content, ok := msgContent["content"]; ok {
			if contentArray, ok := content.([]interface{}); ok {
				for _, block := range contentArray {
					if blockMap, ok := block.(map[string]interface{}); ok {
						if blockMap["type"] == "text" {
							if text, ok := blockMap["text"].(string); ok && text != "" {
								return text
							}
						}
					}
				}
			}
		}

		// Also check if content is a string directly
		if contentStr, ok := msgContent["content"].(string); ok && contentStr != "" {
			return contentStr
		}
	}

	// Strategy 4: Single object with text field {"type": "text", "text": "..."}
	var singleObj map[string]interface{}
	if err := json.Unmarshal(message, &singleObj); err == nil {
		if singleObj["type"] == "text" {
			if text, ok := singleObj["text"].(string); ok && text != "" {
				return text
			}
		}

		// Also check for content field at top level
		if text, ok := singleObj["content"].(string); ok && text != "" {
			return text
		}
	}

	return ""
}

// Conversation handlers

func (h *Handler) GetConversations(w http.ResponseWriter, r *http.Request) {

	conversations, err := h.conversationService.GetConversations()
	if err != nil {
		log.Printf("❌ Error getting conversations: %v", err)
		writeErrorResponse(w, "Failed to get conversations", http.StatusInternalServerError)
		return
	}

	// Flatten all conversations into a single array for the UI
	var allConversations []map[string]interface{}
	for _, convs := range conversations {
		for _, conv := range convs {
			// Extract first user message from the conversation
			var firstMessage string
			for _, msg := range conv.Messages {
				if msg.Type == "user" {
					// Try multiple parsing strategies
					text := extractTextFromMessage(msg.Message)
					if text != "" {
						firstMessage = text
						if len(firstMessage) > 200 {
							firstMessage = firstMessage[:200] + "..."
						}
						break
					}
				}
			}

			allConversations = append(allConversations, map[string]interface{}{
				"id":           conv.SessionID,
				"requestCount": conv.MessageCount,
				"startTime":    conv.StartTime.Format(time.RFC3339),
				"lastActivity": conv.EndTime.Format(time.RFC3339),
				"duration":     conv.EndTime.Sub(conv.StartTime).Milliseconds(),
				"firstMessage": firstMessage,
				"projectName":  conv.ProjectName,
			})
		}
	}

	// Sort by last activity (newest first)
	sort.Slice(allConversations, func(i, j int) bool {
		t1, _ := time.Parse(time.RFC3339, allConversations[i]["lastActivity"].(string))
		t2, _ := time.Parse(time.RFC3339, allConversations[j]["lastActivity"].(string))
		return t1.After(t2)
	})

	// Apply pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}

	start := (page - 1) * limit
	end := start + limit
	if start > len(allConversations) {
		allConversations = []map[string]interface{}{}
	} else {
		if end > len(allConversations) {
			end = len(allConversations)
		}
		allConversations = allConversations[start:end]
	}

	response := map[string]interface{}{
		"conversations": allConversations,
	}

	writeJSONResponse(w, response)
}

func (h *Handler) GetConversationByID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID, ok := vars["id"]
	if !ok {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	projectPath := r.URL.Query().Get("project")
	if projectPath == "" {
		http.Error(w, "Project path is required", http.StatusBadRequest)
		return
	}

	conversation, err := h.conversationService.GetConversation(projectPath, sessionID)
	if err != nil {
		log.Printf("❌ Error getting conversation: %v", err)
		http.Error(w, "Conversation not found", http.StatusNotFound)
		return
	}

	writeJSONResponse(w, conversation)
}

func (h *Handler) GetConversationsByProject(w http.ResponseWriter, r *http.Request) {
	projectPath := r.URL.Query().Get("project")
	if projectPath == "" {
		http.Error(w, "Project path is required", http.StatusBadRequest)
		return
	}

	conversations, err := h.conversationService.GetConversationsByProject(projectPath)
	if err != nil {
		log.Printf("❌ Error getting project conversations: %v", err)
		writeErrorResponse(w, "Failed to get project conversations", http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, conversations)
}
