package handler

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestRestoreResponseModelJSON(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		want     string
	}{
		{"anthropic", anthropicProtocolMessages, `{"model":"target","content":[]}`, `{"content":[],"model":"source"}`},
		{"chat", openAIProtocolChat, `{"model":"target","choices":[]}`, `{"choices":[],"model":"source"}`},
		{"responses", openAIProtocolResponses, `{"model":"target","output":[]}`, `{"model":"source","output":[]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}, "Content-Length": {strconv.Itoa(len(tc.body))}},
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}
			if err := restoreResponseModel(resp, "source", "target", tc.protocol); err != nil {
				t.Fatalf("restore response model: %v", err)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			if string(body) != tc.want {
				t.Fatalf("body = %s, want %s", body, tc.want)
			}
			if resp.ContentLength != int64(len(body)) || resp.Header.Get("Content-Length") != strconv.Itoa(len(body)) {
				t.Fatalf("content length = %d, header = %q, body length = %d", resp.ContentLength, resp.Header.Get("Content-Length"), len(body))
			}
		})
	}
}

func TestRestoreResponseModelSSE(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		want     string
	}{
		{
			"anthropic", anthropicProtocolMessages,
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"target\",\"id\":\"msg_1\"}}\n\n",
			"event: message_start\ndata: {\"message\":{\"id\":\"msg_1\",\"model\":\"source\"},\"type\":\"message_start\"}\n\n",
		},
		{
			"chat", openAIProtocolChat,
			"data: {\"model\":\"target\",\"choices\":[]}\r\n\r\ndata: [DONE]\r\n\r\n",
			"data: {\"choices\":[],\"model\":\"source\"}\r\n\r\ndata: [DONE]\r\n\r\n",
		},
		{
			"responses", openAIProtocolResponses,
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"target\",\"id\":\"resp_1\"}}\n\n",
			"event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"source\"},\"type\":\"response.completed\"}\n\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/event-stream"}, "Content-Length": {strconv.Itoa(len(tc.body))}},
				Body:       io.NopCloser(bytes.NewReader([]byte(tc.body))),
			}
			if err := restoreResponseModel(resp, "source", "target", tc.protocol); err != nil {
				t.Fatalf("restore stream model: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read stream: %v", err)
			}
			if string(body) != tc.want {
				t.Fatalf("stream = %q, want %q", body, tc.want)
			}
			if resp.ContentLength != -1 || resp.Header.Get("Content-Length") != "" {
				t.Fatalf("stream retained stale content length: %d %q", resp.ContentLength, resp.Header.Get("Content-Length"))
			}
		})
	}
}

func TestRestoreResponseModelLeavesErrorsUntouched(t *testing.T) {
	const upstream = `{"error":{"model":"target"}}`
	resp := &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(upstream))}
	if err := restoreResponseModel(resp, "source", "target", openAIProtocolChat); err != nil {
		t.Fatalf("restore error response: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != upstream {
		t.Fatalf("error response = %q, error = %v", body, err)
	}
}
